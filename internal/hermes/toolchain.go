package hermes

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/operationlock"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

var (
	ErrToolchainPin       = errors.New("toolchain pin does not match the checked-out source")
	ErrToolchainLayout    = errors.New("toolchain skill layout is invalid")
	ErrToolchainCollision = errors.New("HERMES_TOOLCHAIN_NAME_COLLISION")
)

const (
	maxToolchainFiles       = 10_000
	maxToolchainBytes       = int64(512 << 20)
	maxToolchainLock        = int64(4 << 20)
	maxToolchainHEAD        = int64(4 << 10)
	maxToolchainSkills      = 256
	maxToolchainRootEntries = 512
)

const toolchainPendingSchemaVersion = 1

// MaterializeOptions provides deterministic fault injection to package tests.
// Production callers use MaterializeToolchain, which supplies only a secure
// nonce source.
type MaterializeOptions struct {
	NonceSource                           func() (string, error)
	AfterPending                          func() error
	AfterPublish                          func(skill string) error
	BeforePublish                         func(skill string) error
	BeforePendingReplace                  func() error
	BeforeFinal                           func() error
	BeforePendingDelete                   func() error
	AfterPendingVerifyBeforeProgress      func() error
	AfterPendingVerifyBeforeDelete        func() error
	AfterLegacyFinalVerifyBeforeNormalize func() error
	AfterStagingVerifyBeforeArchive       func(path string) error
	AfterHEADVerifyBeforeRead             func() error
	AfterHEADValidateBeforeOpen           func() error
	AfterPendingPreviousRename            func() error
	AfterPendingNextRename                func() error
	AfterPendingDeleteRename              func() error
	AfterLegacyPreviousRename             func() error
	AfterLegacyNextRename                 func() error
	AfterStagingRename                    func() error
	AfterStagingTokenRemoved              func() error
	AfterStagingArchiveRemoved            func() error
}

// ToolchainPending is the owner-only progress record for one resumable
// external-toolchain materialization.
type ToolchainPending struct {
	SchemaVersion int           `json:"schema_version"`
	Nonce         string        `json:"nonce"`
	Lock          ToolchainLock `json:"lock"`
	Completed     []string      `json:"completed"`
}

type toolchainFileTransaction struct {
	SchemaVersion     int      `json:"schema_version"`
	Kind              string   `json:"kind"`
	Nonce             string   `json:"nonce"`
	TreeSHA256        string   `json:"tree_sha256"`
	PreviousSHA256    string   `json:"previous_sha256"`
	NextSHA256        string   `json:"next_sha256,omitempty"`
	PreviousCompleted []string `json:"previous_completed,omitempty"`
	NextCompleted     []string `json:"next_completed,omitempty"`
}

type toolchainStagingTransaction struct {
	SchemaVersion int    `json:"schema_version"`
	Nonce         string `json:"nonce"`
	TreeSHA256    string `json:"tree_sha256"`
}

// ToolchainLock is the non-secret receipt proving which single toolchain was
// materialized into a Hermes profile.
type ToolchainLock struct {
	Toolchain       domain.Toolchain `json:"toolchain"`
	Origin          string           `json:"origin"`
	Commit          string           `json:"commit"`
	InstalledSkills []string         `json:"installed_skills"`
	Files           []ToolchainFile  `json:"files"`
	TreeSHA256      string           `json:"tree_sha256"`
}

// ToolchainFile binds one installed relative path to its content. Directory
// entries have an empty SHA256 value so additions of otherwise-empty paths are
// observable too.
type ToolchainFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
}

// ToolchainInstalled observes a profile without changing it.
func ToolchainInstalled(profileRoot string, pin catalog.Toolchain) (bool, error) {
	canonical, err := catalog.LookupToolchain(pin.ID)
	if err != nil || canonical.Origin != pin.Origin || canonical.Commit != pin.Commit {
		return false, ErrToolchainPin
	}
	if err := regularDirectory(profileRoot); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, ErrToolchainLayout
	}
	profileBinding, err := openBoundDirectory(profileRoot)
	if err != nil {
		return false, ErrToolchainLayout
	}
	defer profileBinding.Close()
	external := filepath.Join(profileRoot, "external")
	if err := regularDirectory(external); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, ErrToolchainLayout
	}
	externalBinding, err := openBoundDirectory(external)
	if err != nil {
		return false, ErrToolchainLayout
	}
	defer externalBinding.Close()
	entries, err := readDirectoryBounded(external, maxToolchainRootEntries)
	if err != nil {
		return false, err
	}
	selectedLock := string(pin.ID) + ".json"
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") && entry.Name() != selectedLock {
			return false, ErrToolchainCollision
		}
	}
	lockPath := filepath.Join(external, selectedLock)
	info, err := os.Lstat(lockPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, ErrToolchainLayout
	}
	lock, err := readToolchainLock(lockPath)
	if err != nil {
		return false, ErrToolchainLayout
	}
	if err := validateToolchainLock(lock); err != nil {
		return false, ErrToolchainLayout
	}
	if err := verifyBoundDirectories(profileBinding, externalBinding); err != nil {
		return false, ErrToolchainLayout
	}
	expected := ToolchainLock{
		Toolchain: pin.ID, Origin: pin.Origin, Commit: pin.Commit,
		InstalledSkills: append([]string(nil), lock.InstalledSkills...),
		Files:           append([]ToolchainFile(nil), lock.Files...),
	}
	if len(expected.InstalledSkills) == 0 || len(expected.Files) == 0 {
		return false, nil
	}
	sort.Strings(expected.InstalledSkills)
	sort.Slice(expected.Files, func(i, j int) bool { return expected.Files[i].Path < expected.Files[j].Path })
	expected.TreeSHA256 = toolchainTreeSHA256(expected.Toolchain, expected.Origin, expected.Commit, expected.InstalledSkills, expected.Files)
	if !sameToolchainLock(lock, expected) {
		return false, nil
	}
	skillsRoot := filepath.Join(profileRoot, "skills")
	if err := regularDirectory(skillsRoot); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, ErrToolchainLayout
	}
	skillsBinding, err := openBoundDirectory(skillsRoot)
	if err != nil {
		return false, ErrToolchainLayout
	}
	defer skillsBinding.Close()
	ready := installedSkillsMatch(skillsRoot, expected.InstalledSkills, expected.Files)
	if err := verifyBoundDirectories(profileBinding, externalBinding, skillsBinding); err != nil {
		return false, ErrToolchainLayout
	}
	return ready, nil
}

// CheckBundledSkillCollisions rejects an external skill name already owned by
// the verified Hermes bundled inventory.
func CheckBundledSkillCollisions(bundled, selected []string) error {
	bundledNames, err := checkedSkillNames(bundled, true)
	if err != nil {
		return err
	}
	selectedNames, err := checkedSkillNames(selected, false)
	if err != nil {
		return err
	}
	for name := range selectedNames {
		if _, exists := bundledNames[name]; exists {
			return ErrToolchainCollision
		}
	}
	return nil
}

// VerifiedToolchainLock reads the single selected final lock and proves that it
// is canonical for pin without inspecting or changing unrelated profile data.
func VerifiedToolchainLock(profileRoot string, pin catalog.Toolchain) (ToolchainLock, error) {
	canonical, err := catalog.LookupToolchain(pin.ID)
	if err != nil || canonical.Origin != pin.Origin || canonical.Commit != pin.Commit {
		return ToolchainLock{}, ErrToolchainPin
	}
	if err := regularDirectory(profileRoot); err != nil {
		return ToolchainLock{}, ErrToolchainLayout
	}
	profileBinding, err := openBoundDirectory(profileRoot)
	if err != nil {
		return ToolchainLock{}, ErrToolchainLayout
	}
	defer profileBinding.Close()
	external := filepath.Join(profileRoot, "external")
	if err := regularDirectory(external); err != nil {
		return ToolchainLock{}, ErrToolchainLayout
	}
	externalBinding, err := openBoundDirectory(external)
	if err != nil {
		return ToolchainLock{}, ErrToolchainLayout
	}
	defer externalBinding.Close()
	entries, err := readDirectoryBounded(external, maxToolchainRootEntries)
	if err != nil {
		return ToolchainLock{}, ErrToolchainLayout
	}
	selectedLock := string(pin.ID) + ".json"
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") && entry.Name() != selectedLock {
			return ToolchainLock{}, ErrToolchainCollision
		}
	}
	lock, err := readToolchainLock(filepath.Join(external, selectedLock))
	if err != nil || validateToolchainLock(lock) != nil {
		return ToolchainLock{}, ErrToolchainLayout
	}
	expected := ToolchainLock{
		Toolchain: pin.ID, Origin: pin.Origin, Commit: pin.Commit,
		InstalledSkills: append([]string(nil), lock.InstalledSkills...),
		Files:           append([]ToolchainFile(nil), lock.Files...),
	}
	sort.Strings(expected.InstalledSkills)
	sort.Slice(expected.Files, func(i, j int) bool { return expected.Files[i].Path < expected.Files[j].Path })
	expected.TreeSHA256 = toolchainTreeSHA256(expected.Toolchain, expected.Origin, expected.Commit, expected.InstalledSkills, expected.Files)
	if !sameToolchainLock(lock, expected) {
		return ToolchainLock{}, ErrToolchainLayout
	}
	if err := verifyBoundDirectories(profileBinding, externalBinding); err != nil {
		return ToolchainLock{}, ErrToolchainLayout
	}
	return lock, nil
}

// MaterializeToolchain installs only the closed, pinned skill layout alongside
// unrelated Hermes bundled and user skills. A profile can contain exactly one
// such external toolchain.
func MaterializeToolchain(sourceRoot, profileRoot string, pin catalog.Toolchain) error {
	return materializeToolchain(sourceRoot, profileRoot, pin, MaterializeOptions{NonceSource: secureToolchainNonce})
}

func materializeToolchain(sourceRoot, profileRoot string, pin catalog.Toolchain, options MaterializeOptions) error {
	canonical, err := catalog.LookupToolchain(pin.ID)
	if err != nil || canonical.Origin != pin.Origin || canonical.Commit != pin.Commit {
		return ErrToolchainPin
	}
	if err := regularDirectory(sourceRoot); err != nil {
		return fmt.Errorf("%w: source: %v", ErrToolchainLayout, err)
	}
	if err := regularDirectory(profileRoot); err != nil {
		return fmt.Errorf("%w: profile: %v", ErrToolchainLayout, err)
	}
	sourceBinding, err := openBoundDirectory(sourceRoot)
	if err != nil {
		return fmt.Errorf("%w: source identity", ErrToolchainLayout)
	}
	defer sourceBinding.Close()
	profileBinding, err := openBoundDirectory(profileRoot)
	if err != nil {
		return fmt.Errorf("%w: profile identity", ErrToolchainLayout)
	}
	defer profileBinding.Close()
	headPath := filepath.Join(sourceRoot, ".git", "HEAD")
	if err := pathsafe.ValidateRegular(headPath); err != nil {
		return fmt.Errorf("%w: source HEAD: %v", ErrToolchainLayout, err)
	}
	head, err := readToolchainHEAD(headPath, options.AfterHEADValidateBeforeOpen, options.AfterHEADVerifyBeforeRead)
	if err != nil || strings.TrimSpace(string(head)) != pin.Commit {
		if err != nil {
			return fmt.Errorf("%w: source HEAD: %v", ErrToolchainLayout, err)
		}
		return ErrToolchainPin
	}

	sourceSkills := filepath.Join(sourceRoot, toolchainSkillsSubpath(pin.ID))
	if err := regularDirectoryComponents(sourceRoot, toolchainSkillsSubpath(pin.ID)); err != nil {
		return fmt.Errorf("%w: skill root: %v", ErrToolchainLayout, err)
	}
	sourceSkillsBinding, err := openBoundDirectory(sourceSkills)
	if err != nil {
		return fmt.Errorf("%w: source skills identity", ErrToolchainLayout)
	}
	defer sourceSkillsBinding.Close()
	entries, err := readDirectoryBounded(sourceSkills, maxToolchainRootEntries)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrToolchainLayout, err)
	}
	skills := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: redirected source entry", ErrToolchainLayout)
		}
		if !entry.IsDir() {
			if err := pathsafe.ValidateRegular(filepath.Join(sourceSkills, entry.Name())); err != nil {
				return fmt.Errorf("%w: source entry: %v", ErrToolchainLayout, err)
			}
			continue
		}
		skillDirectory := filepath.Join(sourceSkills, entry.Name())
		if err := pathsafe.ValidateDirectory(skillDirectory); err != nil {
			return fmt.Errorf("%w: skill directory: %v", ErrToolchainLayout, err)
		}
		skillFile := filepath.Join(skillDirectory, "SKILL.md")
		if err := pathsafe.ValidateRegular(skillFile); err != nil {
			return fmt.Errorf("%w: skill file: %v", ErrToolchainLayout, err)
		}
		if info, statErr := os.Lstat(skillFile); statErr == nil && info.Mode().IsRegular() {
			skills = append(skills, entry.Name())
		}
	}
	sort.Strings(skills)
	if len(skills) == 0 || len(skills) > maxToolchainSkills {
		return ErrToolchainLayout
	}
	sourceSkillBindings := make(map[string]*boundDirectory, len(skills))
	for _, skill := range skills {
		binding, err := openBoundDirectory(filepath.Join(sourceSkills, skill))
		if err != nil {
			return ErrToolchainLayout
		}
		sourceSkillBindings[skill] = binding
		defer binding.Close()
	}
	manifest, err := selectedToolchainManifest(sourceSkills, skills)
	if err != nil {
		return err
	}
	lock := ToolchainLock{Toolchain: pin.ID, Origin: pin.Origin, Commit: pin.Commit, InstalledSkills: skills, Files: manifest}
	lock.TreeSHA256 = toolchainTreeSHA256(lock.Toolchain, lock.Origin, lock.Commit, lock.InstalledSkills, lock.Files)
	if err := validateToolchainLock(lock); err != nil {
		return err
	}
	if err := verifyBoundDirectories(sourceBinding, sourceSkillsBinding, profileBinding); err != nil {
		return err
	}
	targetSkills := filepath.Join(profileRoot, "skills")
	for _, skill := range lock.InstalledSkills {
		selectedPath := filepath.Join(targetSkills, skill)
		if err := pathsafe.ValidateDirectory(selectedPath); err != nil {
			return ErrToolchainCollision
		}
		if info, err := os.Lstat(selectedPath); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return ErrToolchainCollision
		} else if err != nil && !os.IsNotExist(err) {
			return ErrToolchainLayout
		}
	}
	teamkitRoot := filepath.Join(profileRoot, ".teamkit")
	if err := pathsafe.EnsureDirectory(teamkitRoot, 0o700); err != nil {
		return fmt.Errorf("%w: pending directory: %v", ErrToolchainLayout, err)
	}
	teamkitBinding, err := openBoundDirectory(teamkitRoot)
	if err != nil {
		return ErrToolchainLayout
	}
	defer teamkitBinding.Close()
	profileLock, err := operationlock.Acquire(profileRoot)
	if err != nil {
		return err
	}
	defer profileLock.Close()
	if err := verifyBoundDirectories(sourceBinding, sourceSkillsBinding, profileBinding, teamkitBinding); err != nil {
		return err
	}

	external := filepath.Join(profileRoot, "external")
	if err := inspectExclusiveLock(external, pin.ID); err != nil {
		return err
	}
	var initialExternalBinding *boundDirectory
	if _, err := os.Lstat(external); err == nil {
		initialExternalBinding, err = openBoundDirectory(external)
		if err != nil {
			return ErrToolchainLayout
		}
		defer initialExternalBinding.Close()
	} else if !os.IsNotExist(err) {
		return ErrToolchainLayout
	}
	lockPath := filepath.Join(external, string(pin.ID)+".json")
	if err := pathsafe.ValidateDirectory(targetSkills); err != nil {
		return fmt.Errorf("%w: skills directory: %v", ErrToolchainLayout, err)
	}
	var initialSkillsBinding *boundDirectory
	if _, err := os.Lstat(targetSkills); err == nil {
		initialSkillsBinding, err = openBoundDirectory(targetSkills)
		if err != nil {
			return ErrToolchainLayout
		}
		defer initialSkillsBinding.Close()
	} else if !os.IsNotExist(err) {
		return ErrToolchainLayout
	}

	pendingPath := filepath.Join(teamkitRoot, "toolchain.pending.json")
	if err := reconcileToolchainFileTransaction(teamkitRoot, pendingPath, lockPath, lock); err != nil {
		return err
	}
	if err := reconcileStagingTransaction(teamkitRoot, lock); err != nil {
		return err
	}
	pending, pendingExists, pendingIdentity, err := loadToolchainPending(pendingPath)
	if err != nil {
		return err
	}
	defer func() {
		if pendingIdentity != nil {
			pendingIdentity.Close()
		}
	}()

	existingLock, finalExists, err := loadFinalToolchainLock(lockPath, &lock, teamkitRoot, initialExternalBinding, options.AfterLegacyFinalVerifyBeforeNormalize, options.AfterLegacyPreviousRename, options.AfterLegacyNextRename)
	if err != nil {
		return err
	}
	if finalExists && !sameToolchainLock(existingLock, lock) {
		return ErrToolchainCollision
	}

	createdPending := false
	if !pendingExists {
		if finalExists {
			if installedSkillsMatch(targetSkills, lock.InstalledSkills, lock.Files) {
				return nil
			}
			return ErrToolchainCollision
		}
		for _, skill := range lock.InstalledSkills {
			if _, statErr := os.Lstat(filepath.Join(targetSkills, skill)); statErr == nil {
				return ErrToolchainCollision
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("%w: selected skill preflight: %v", ErrToolchainLayout, statErr)
			}
		}
		if options.NonceSource == nil {
			return fmt.Errorf("%w: nonce source is unavailable", ErrToolchainLayout)
		}
		nonce, nonceErr := options.NonceSource()
		if nonceErr != nil {
			return fmt.Errorf("%w: nonce source: %v", ErrToolchainLayout, nonceErr)
		}
		if !validToolchainNonce(nonce) {
			return fmt.Errorf("%w: invalid operation nonce", ErrToolchainLayout)
		}
		if err := verifyBoundDirectories(sourceBinding, sourceSkillsBinding, profileBinding, teamkitBinding, initialSkillsBinding, initialExternalBinding); err != nil {
			return err
		}
		stagingRoot := toolchainStagingPath(teamkitRoot, nonce)
		if err := os.Mkdir(stagingRoot, 0o700); err != nil {
			return fmt.Errorf("%w: staging directory: %v", ErrToolchainLayout, err)
		}
		pending = ToolchainPending{SchemaVersion: toolchainPendingSchemaVersion, Nonce: nonce, Lock: lock, Completed: []string{}}
		if err := verifyBoundDirectories(sourceBinding, sourceSkillsBinding, profileBinding, teamkitBinding); err != nil {
			return err
		}
		pendingIdentity, err = writeToolchainPendingInitial(pendingPath, pending)
		if err != nil {
			return err
		}
		createdPending = true
	} else if err := validateToolchainPending(pending, lock, teamkitRoot); err != nil {
		return err
	}

	if createdPending && options.AfterPending != nil {
		if err := options.AfterPending(); err != nil {
			return err
		}
	}
	if err := verifyBoundDirectories(sourceBinding, sourceSkillsBinding, profileBinding, teamkitBinding, initialSkillsBinding, initialExternalBinding); err != nil {
		return err
	}
	if err := verifyToolchainPendingState(pendingPath, pending, pendingIdentity); err != nil {
		return err
	}

	for _, skill := range pending.Completed {
		if err := verifyCompletedSkill(targetSkills, skill, pending.Lock.Files); err != nil {
			return err
		}
	}
	if initialSkillsBinding == nil {
		if _, err := os.Lstat(targetSkills); err == nil {
			return ErrToolchainLayout
		} else if !os.IsNotExist(err) {
			return ErrToolchainLayout
		}
	}
	if initialExternalBinding == nil {
		if _, err := os.Lstat(external); err == nil {
			return ErrToolchainLayout
		} else if !os.IsNotExist(err) {
			return ErrToolchainLayout
		}
	}
	if err := pathsafe.EnsureDirectory(targetSkills, 0o700); err != nil {
		return fmt.Errorf("%w: skills directory: %v", ErrToolchainLayout, err)
	}
	if err := regularDirectory(targetSkills); err != nil {
		return fmt.Errorf("%w: skills directory: %v", ErrToolchainLayout, err)
	}
	if err := ensureExclusiveLock(external, pin.ID); err != nil {
		return err
	}
	targetSkillsBinding, err := openBoundDirectory(targetSkills)
	if err != nil {
		return ErrToolchainLayout
	}
	defer targetSkillsBinding.Close()
	externalBinding, err := openBoundDirectory(external)
	if err != nil {
		return ErrToolchainLayout
	}
	defer externalBinding.Close()

	stagingRoot := toolchainStagingPath(teamkitRoot, pending.Nonce)
	for _, skill := range pending.Lock.InstalledSkills {
		if containsSkill(pending.Completed, skill) {
			continue
		}
		if err := verifyBoundDirectories(sourceBinding, sourceSkillsBinding, sourceSkillBindings[skill], profileBinding, teamkitBinding, targetSkillsBinding, externalBinding); err != nil {
			return err
		}
		if err := verifyToolchainPendingState(pendingPath, pending, pendingIdentity); err != nil {
			return err
		}
		target := filepath.Join(targetSkills, skill)
		if info, statErr := os.Lstat(target); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return ErrToolchainCollision
			}
			if !skillMatchesManifest(targetSkills, skill, pending.Lock.Files) {
				return ErrToolchainCollision
			}
		} else if os.IsNotExist(statErr) {
			if err := verifyBoundDirectories(sourceBinding, sourceSkillsBinding, sourceSkillBindings[skill], profileBinding); err != nil {
				return err
			}
			stagedSkill := filepath.Join(stagingRoot, skill)
			if err := stageToolchainSkill(filepath.Join(sourceSkills, skill), stagedSkill, skill, pending.Lock.Files); err != nil {
				return err
			}
			if err := verifyBoundDirectories(sourceBinding, sourceSkillsBinding, sourceSkillBindings[skill], profileBinding, teamkitBinding, targetSkillsBinding, externalBinding); err != nil {
				return err
			}
			if err := verifyToolchainPendingState(pendingPath, pending, pendingIdentity); err != nil {
				return err
			}
			if options.BeforePublish != nil {
				if err := options.BeforePublish(skill); err != nil {
					return err
				}
			}
			if err := renameToolchainNoReplace(stagedSkill, target); err != nil {
				return ErrToolchainCollision
			}
			if !skillMatchesManifest(targetSkills, skill, pending.Lock.Files) {
				return ErrToolchainLayout
			}
		} else {
			return fmt.Errorf("%w: selected skill: %v", ErrToolchainLayout, statErr)
		}
		previous := pending
		pending.Completed = append(append([]string(nil), pending.Completed...), skill)
		if options.BeforePendingReplace != nil {
			if err := options.BeforePendingReplace(); err != nil {
				return err
			}
		}
		pendingIdentity, err = replaceToolchainPendingTransactional(teamkitRoot, pendingPath, previous, pendingIdentity, pending, options.AfterPendingVerifyBeforeProgress, options.AfterPendingPreviousRename, options.AfterPendingNextRename)
		if err != nil {
			return err
		}
		if options.AfterPublish != nil {
			if err := options.AfterPublish(skill); err != nil {
				return err
			}
		}
	}

	if err := verifyBoundDirectories(sourceBinding, sourceSkillsBinding, profileBinding, teamkitBinding, targetSkillsBinding, externalBinding); err != nil {
		return err
	}
	if err := verifyToolchainPendingState(pendingPath, pending, pendingIdentity); err != nil {
		return err
	}
	if !installedSkillsMatch(targetSkills, pending.Lock.InstalledSkills, pending.Lock.Files) {
		return ErrToolchainLayout
	}
	if err := archiveEmptyStaging(teamkitRoot, stagingRoot, pending.Nonce, pending.Lock.TreeSHA256, options.AfterStagingVerifyBeforeArchive, options.AfterStagingRename, options.AfterStagingTokenRemoved, options.AfterStagingArchiveRemoved); err != nil {
		return err
	}
	if finalExists {
		if !sameToolchainLock(existingLock, pending.Lock) {
			return ErrToolchainCollision
		}
	} else {
		if options.BeforeFinal != nil {
			if err := options.BeforeFinal(); err != nil {
				return err
			}
		}
		if _, appeared, err := loadFinalToolchainLock(lockPath, nil, teamkitRoot, externalBinding, nil, nil, nil); err != nil || appeared {
			return ErrToolchainCollision
		}
		if err := verifyBoundDirectories(profileBinding, teamkitBinding, targetSkillsBinding, externalBinding); err != nil {
			return err
		}
		if err := verifyToolchainPendingState(pendingPath, pending, pendingIdentity); err != nil {
			return err
		}
		if err := writeToolchainLockNoReplace(lockPath, pending.Lock); err != nil {
			return err
		}
	}
	if options.BeforePendingDelete != nil {
		if err := options.BeforePendingDelete(); err != nil {
			return err
		}
	}
	if err := removePendingCAS(teamkitRoot, pendingPath, pending, pendingIdentity, options.AfterPendingVerifyBeforeDelete, options.AfterPendingDeleteRename); err != nil {
		return err
	}
	return nil
}

func toolchainSkillsSubpath(id domain.Toolchain) string {
	switch id {
	case domain.ToolchainAIRules1C:
		return filepath.Join("content", "skills")
	case domain.ToolchainCC1CSkills:
		return filepath.Join(".claude", "skills")
	default:
		return ""
	}
}

func regularDirectory(path string) error {
	if err := pathsafe.ValidateDirectory(path); err != nil {
		return fmt.Errorf("%w: %v", ErrToolchainLayout, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrToolchainLayout
	}
	return nil
}

type boundDirectory struct {
	path string
	file *os.File
	info os.FileInfo
}

func openBoundDirectory(path string) (*boundDirectory, error) {
	if err := regularDirectory(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		_ = file.Close()
		return nil, ErrToolchainLayout
	}
	named, err := os.Lstat(path)
	if err != nil || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, named) {
		_ = file.Close()
		return nil, ErrToolchainLayout
	}
	return &boundDirectory{path: path, file: file, info: info}, nil
}

func (directory *boundDirectory) verify() error {
	if directory == nil || directory.file == nil {
		return ErrToolchainLayout
	}
	opened, err := directory.file.Stat()
	if err != nil || !opened.IsDir() || !os.SameFile(opened, directory.info) {
		return ErrToolchainLayout
	}
	named, err := os.Lstat(directory.path)
	if err != nil || named.Mode()&os.ModeSymlink != 0 || !named.IsDir() || !os.SameFile(opened, named) {
		return ErrToolchainLayout
	}
	return nil
}

func (directory *boundDirectory) Close() {
	if directory != nil && directory.file != nil {
		_ = directory.file.Close()
	}
}

func verifyBoundDirectories(directories ...*boundDirectory) error {
	for _, directory := range directories {
		if directory != nil {
			if err := directory.verify(); err != nil {
				return ErrToolchainLayout
			}
		}
	}
	return nil
}

func readDirectoryBounded(path string, limit int) ([]fs.DirEntry, error) {
	directory, err := openBoundDirectory(path)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.file.ReadDir(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > limit {
		return nil, ErrToolchainLayout
	}
	if err := directory.verify(); err != nil {
		return nil, err
	}
	return entries, nil
}

func readToolchainHEAD(path string, afterValidate, afterVerify func() error) ([]byte, error) {
	if err := pathsafe.ValidateRegular(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxToolchainHEAD || !os.SameFile(info, info) {
		return nil, ErrToolchainLayout
	}
	if afterValidate != nil {
		if err := afterValidate(); err != nil {
			return nil, err
		}
	}
	if afterVerify != nil {
		if err := afterVerify(); err != nil {
			return nil, err
		}
	}
	data, err := pathsafe.ReadRegular(path, maxToolchainHEAD)
	if err != nil {
		return nil, ErrToolchainLayout
	}
	namedAfter, namedErr := os.Lstat(path)
	if namedErr != nil || namedAfter.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, namedAfter) || info.Size() != int64(len(data)) {
		return nil, ErrToolchainLayout
	}
	return data, nil
}

func regularDirectoryComponents(root, relative string) error {
	current := root
	for _, component := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." {
			return ErrToolchainLayout
		}
		current = filepath.Join(current, component)
		if err := regularDirectory(current); err != nil {
			return err
		}
	}
	return nil
}

func secureToolchainNonce() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}

func validToolchainNonce(nonce string) bool {
	if len(nonce) != 32 || nonce != strings.ToLower(nonce) {
		return false
	}
	decoded, err := hex.DecodeString(nonce)
	return err == nil && len(decoded) == 16
}

func checkedSkillNames(names []string, allowEmpty bool) (map[string]struct{}, error) {
	if !allowEmpty && len(names) == 0 {
		return nil, ErrToolchainLayout
	}
	checked := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || filepath.Base(name) != name || filepath.Clean(name) != name {
			return nil, ErrToolchainLayout
		}
		if _, exists := checked[name]; exists {
			return nil, ErrToolchainLayout
		}
		checked[name] = struct{}{}
	}
	return checked, nil
}

func inspectExclusiveLock(directory string, selected domain.Toolchain) error {
	if err := pathsafe.ValidateDirectory(directory); err != nil {
		return fmt.Errorf("%w: lock directory: %v", ErrToolchainLayout, err)
	}
	if _, err := os.Lstat(directory); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	entries, err := readDirectoryBounded(directory, maxToolchainRootEntries)
	if err != nil {
		return err
	}
	selectedName := string(selected) + ".json"
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") && entry.Name() != selectedName {
			return ErrToolchainCollision
		}
	}
	return nil
}

func ensureExclusiveLock(directory string, selected domain.Toolchain) error {
	if err := pathsafe.EnsureDirectory(directory, 0o700); err != nil {
		return fmt.Errorf("%w: lock directory: %v", ErrToolchainLayout, err)
	}
	if err := regularDirectory(directory); err != nil {
		return ErrToolchainLayout
	}
	return inspectExclusiveLock(directory, selected)
}

func copySkillTree(source, target string, files *int, total *int64) error {
	visited, err := walkToolchainTree(source, maxToolchainFiles-*files, func(path string, entry fs.DirEntry) error {
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrToolchainLayout
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return ErrToolchainLayout
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			if err := pathsafe.ValidateDirectory(path); err != nil {
				return ErrToolchainLayout
			}
			return pathsafe.EnsureDirectory(destination, 0o700)
		}
		if err := pathsafe.ValidateRegular(path); err != nil {
			return ErrToolchainLayout
		}
		copied, err := copyRegularFileBounded(path, destination, maxToolchainBytes-*total)
		if err != nil {
			return ErrToolchainLayout
		}
		*total += copied
		return nil
	})
	*files += visited
	return err
}

func copyRegularFileBounded(source, target string, remaining int64) (int64, error) {
	if remaining < 0 || pathsafe.ValidateRegular(source) != nil {
		return 0, ErrToolchainLayout
	}
	input, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > remaining {
		_ = input.Close()
		return 0, ErrToolchainLayout
	}
	named, err := os.Lstat(source)
	if err != nil || !os.SameFile(info, named) {
		_ = input.Close()
		return 0, ErrToolchainLayout
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = input.Close()
		return 0, err
	}
	copied, copyErr := io.CopyN(output, input, remaining+1)
	if errors.Is(copyErr, io.EOF) {
		copyErr = nil
	}
	after, statErr := input.Stat()
	namedAfter, namedErr := os.Lstat(source)
	closeOutputErr := output.Close()
	closeInputErr := input.Close()
	if copyErr != nil || copied > remaining || statErr != nil || namedErr != nil || !os.SameFile(info, after) || !os.SameFile(after, namedAfter) || after.Size() != copied || copied != info.Size() {
		return 0, ErrToolchainLayout
	}
	if closeOutputErr != nil {
		return 0, closeOutputErr
	}
	if closeInputErr != nil {
		return 0, closeInputErr
	}
	return copied, nil
}

func loadToolchainPending(path string) (ToolchainPending, bool, *openedPrivateRecord, error) {
	data, exists, identity, err := readBoundedPrivate(path, maxToolchainLock)
	if err != nil {
		return ToolchainPending{}, false, nil, fmt.Errorf("%w: pending: %v", ErrToolchainLayout, err)
	}
	if !exists {
		return ToolchainPending{}, false, nil, nil
	}
	var pending ToolchainPending
	if err := decodeStrictJSON(data, &pending); err != nil {
		identity.Close()
		return ToolchainPending{}, false, nil, fmt.Errorf("%w: pending JSON", ErrToolchainLayout)
	}
	return pending, true, identity, nil
}

func validateToolchainPending(pending ToolchainPending, expected ToolchainLock, teamkitRoot string) error {
	if pending.SchemaVersion != toolchainPendingSchemaVersion || !validToolchainNonce(pending.Nonce) || !sameToolchainLock(pending.Lock, expected) {
		return ErrToolchainLayout
	}
	if len(pending.Completed) > len(expected.InstalledSkills) {
		return ErrToolchainLayout
	}
	for index, completed := range pending.Completed {
		if completed != expected.InstalledSkills[index] {
			return ErrToolchainLayout
		}
	}
	if err := regularDirectory(toolchainStagingPath(teamkitRoot, pending.Nonce)); err != nil &&
		!(os.IsNotExist(err) && len(pending.Completed) == len(expected.InstalledSkills)) {
		return fmt.Errorf("%w: operation staging: %v", ErrToolchainLayout, err)
	}
	return nil
}

func toolchainStagingPath(teamkitRoot, nonce string) string {
	return filepath.Join(teamkitRoot, "toolchain-staging-"+nonce)
}

func marshalToolchainPending(pending ToolchainPending) ([]byte, error) {
	data, err := json.Marshal(pending)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxToolchainLock {
		return nil, ErrToolchainLayout
	}
	return data, nil
}

func writeToolchainPendingInitial(path string, pending ToolchainPending) (*openedPrivateRecord, error) {
	data, err := marshalToolchainPending(pending)
	if err != nil {
		return nil, err
	}
	if err := writePrivateNoReplace(path, data); err != nil {
		return nil, fmt.Errorf("%w: initial pending already exists", ErrToolchainCollision)
	}
	loaded, exists, identity, err := loadToolchainPending(path)
	if err != nil || !exists || !sameToolchainPending(loaded, pending) {
		return nil, ErrToolchainLayout
	}
	return identity, nil
}

func toolchainTransactionPaths(teamkitRoot string) (descriptor, previous, next string) {
	return filepath.Join(teamkitRoot, "toolchain-txn.json"), filepath.Join(teamkitRoot, "toolchain-txn.prev"), filepath.Join(teamkitRoot, "toolchain-txn.next")
}

func bytesSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func marshalToolchainTransaction(transaction toolchainFileTransaction) ([]byte, error) {
	data, err := json.Marshal(transaction)
	if err != nil || int64(len(data)) > maxToolchainLock {
		return nil, ErrToolchainLayout
	}
	return data, nil
}

func readTransaction(path string) (toolchainFileTransaction, bool, error) {
	data, exists, record, err := readBoundedPrivate(path, maxToolchainLock)
	if err != nil || !exists {
		return toolchainFileTransaction{}, exists, err
	}
	defer record.Close()
	var transaction toolchainFileTransaction
	if decodeStrictJSON(data, &transaction) != nil {
		return toolchainFileTransaction{}, false, ErrToolchainLayout
	}
	return transaction, true, nil
}

func readTransactionFile(path string, private bool) ([]byte, bool, *openedPrivateRecord, error) {
	if private {
		return readBoundedPrivate(path, maxToolchainLock)
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, false, nil, nil
	} else if err != nil {
		return nil, false, nil, err
	}
	data, record, err := readBoundedLegacyRegular(path, maxToolchainLock)
	return data, err == nil, record, err
}

func transactionFileMatches(path, expectedHash string, private bool, expectedInfo os.FileInfo) bool {
	data, exists, record, err := readTransactionFile(path, private)
	if err != nil || !exists {
		return false
	}
	defer record.Close()
	return bytesSHA256(data) == expectedHash && (expectedInfo == nil || os.SameFile(expectedInfo, record.info))
}

func removeTransactionFile(path, expectedHash string, private bool) error {
	data, exists, record, err := readTransactionFile(path, private)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if bytesSHA256(data) != expectedHash || record.verify(path) != nil {
		record.Close()
		return ErrToolchainLayout
	}
	record.Close()
	if err := os.Remove(path); err != nil {
		return err
	}
	return nil
}

func removeTransactionDescriptor(path string, transaction toolchainFileTransaction) error {
	data, err := marshalToolchainTransaction(transaction)
	if err != nil {
		return err
	}
	return removeTransactionFile(path, bytesSHA256(data), true)
}

func cleanupStagedTransaction(descriptorPath, nextPath string, transaction toolchainFileTransaction) error {
	if transaction.NextSHA256 != "" {
		if err := removeTransactionFile(nextPath, transaction.NextSHA256, true); err != nil {
			return err
		}
	}
	return removeTransactionDescriptor(descriptorPath, transaction)
}

func beginToolchainFileTransaction(teamkitRoot, target string, transaction toolchainFileTransaction, previousData, nextData []byte, previousPrivate bool, expectedInfo os.FileInfo, afterVerify, afterPreviousRename, afterNextRename func() error) error {
	descriptorPath, previousPath, nextPath := toolchainTransactionPaths(teamkitRoot)
	transaction.PreviousSHA256 = bytesSHA256(previousData)
	if nextData != nil {
		transaction.NextSHA256 = bytesSHA256(nextData)
	}
	descriptorData, err := marshalToolchainTransaction(transaction)
	if err != nil {
		return err
	}
	if nextData != nil {
		if err := writePrivateNoReplace(nextPath, nextData); err != nil {
			return ErrToolchainLayout
		}
	}
	if err := writePrivateNoReplace(descriptorPath, descriptorData); err != nil {
		if nextData != nil {
			if cleanupErr := removeTransactionFile(nextPath, transaction.NextSHA256, true); cleanupErr != nil {
				return fmt.Errorf("%w: transaction setup cleanup: %v", ErrToolchainLayout, cleanupErr)
			}
		}
		return ErrToolchainLayout
	}
	if !transactionFileMatches(target, transaction.PreviousSHA256, previousPrivate, expectedInfo) {
		return ErrToolchainLayout
	}
	if afterVerify != nil {
		if err := afterVerify(); err != nil {
			return err
		}
	}
	if err := renameToolchainNoReplace(target, previousPath); err != nil {
		return ErrToolchainLayout
	}
	if afterPreviousRename != nil {
		if err := afterPreviousRename(); err != nil {
			return err
		}
	}
	if !transactionFileMatches(previousPath, transaction.PreviousSHA256, previousPrivate, expectedInfo) {
		if rollbackErr := renameToolchainNoReplace(previousPath, target); rollbackErr != nil {
			return fmt.Errorf("%w: transaction rollback: %v", ErrToolchainLayout, rollbackErr)
		}
		if cleanupErr := cleanupStagedTransaction(descriptorPath, nextPath, transaction); cleanupErr != nil {
			return fmt.Errorf("%w: transaction rollback cleanup: %v", ErrToolchainLayout, cleanupErr)
		}
		return ErrToolchainLayout
	}
	if nextData != nil {
		if err := renameToolchainNoReplace(nextPath, target); err != nil {
			if _, statErr := os.Lstat(target); os.IsNotExist(statErr) {
				if rollbackErr := renameToolchainNoReplace(previousPath, target); rollbackErr != nil {
					return fmt.Errorf("%w: transaction publish rollback: %v", ErrToolchainLayout, rollbackErr)
				}
				if cleanupErr := cleanupStagedTransaction(descriptorPath, nextPath, transaction); cleanupErr != nil {
					return fmt.Errorf("%w: transaction publish rollback cleanup: %v", ErrToolchainLayout, cleanupErr)
				}
			}
			return ErrToolchainLayout
		}
		if afterNextRename != nil {
			if err := afterNextRename(); err != nil {
				return err
			}
		}
		if !transactionFileMatches(target, transaction.NextSHA256, true, nil) {
			return ErrToolchainLayout
		}
	}
	if err := removeTransactionFile(previousPath, transaction.PreviousSHA256, previousPrivate); err != nil {
		return err
	}
	if err := removeTransactionDescriptor(descriptorPath, transaction); err != nil {
		return err
	}
	return nil
}

func transactionFileState(path string, private bool) (exists bool, hash string, err error) {
	data, exists, record, err := readTransactionFile(path, private)
	if err != nil || !exists {
		return exists, "", err
	}
	defer record.Close()
	return true, bytesSHA256(data), nil
}

func validateTransactionDescriptor(transaction toolchainFileTransaction, expected ToolchainLock) error {
	if transaction.SchemaVersion != toolchainPendingSchemaVersion || !validToolchainNonce(transaction.Nonce) || transaction.TreeSHA256 != expected.TreeSHA256 || len(transaction.PreviousSHA256) != sha256.Size*2 {
		return ErrToolchainLayout
	}
	if transaction.Kind != "pending-progress" && transaction.Kind != "pending-delete" && transaction.Kind != "legacy-final" {
		return ErrToolchainLayout
	}
	if transaction.Kind != "pending-delete" && len(transaction.NextSHA256) != sha256.Size*2 {
		return ErrToolchainLayout
	}
	for _, completed := range [][]string{transaction.PreviousCompleted, transaction.NextCompleted} {
		if len(completed) > len(expected.InstalledSkills) {
			return ErrToolchainLayout
		}
		for index, skill := range completed {
			if skill != expected.InstalledSkills[index] {
				return ErrToolchainLayout
			}
		}
	}
	return nil
}

func reconcileToolchainFileTransaction(teamkitRoot, pendingPath, finalPath string, expected ToolchainLock) error {
	descriptorPath, previousPath, nextPath := toolchainTransactionPaths(teamkitRoot)
	transaction, exists, err := readTransaction(descriptorPath)
	if err != nil {
		return ErrToolchainLayout
	}
	if !exists {
		if _, statErr := os.Lstat(previousPath); statErr == nil {
			return ErrToolchainLayout
		} else if !os.IsNotExist(statErr) {
			return ErrToolchainLayout
		}
		data, nextExists, record, nextErr := readBoundedPrivate(nextPath, maxToolchainLock)
		if nextErr != nil {
			return ErrToolchainLayout
		}
		if nextExists {
			record.Close()
			validOrphan := false
			var pending ToolchainPending
			if decodeStrictJSON(data, &pending) == nil && pending.SchemaVersion == toolchainPendingSchemaVersion && validToolchainNonce(pending.Nonce) && sameToolchainLock(pending.Lock, expected) && len(pending.Completed) <= len(expected.InstalledSkills) {
				validOrphan = true
				for index, skill := range pending.Completed {
					if skill != expected.InstalledSkills[index] {
						validOrphan = false
					}
				}
			}
			var final ToolchainLock
			if decodeStrictJSON(data, &final) == nil && sameToolchainLock(final, expected) {
				validOrphan = true
			}
			if !validOrphan {
				return ErrToolchainLayout
			}
			if err := removeTransactionFile(nextPath, bytesSHA256(data), true); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validateTransactionDescriptor(transaction, expected); err != nil {
		return err
	}
	target := pendingPath
	previousPrivate := true
	if transaction.Kind == "legacy-final" {
		target = finalPath
		previousPrivate = false
	}
	targetExists, targetHash, targetErr := transactionFileState(target, false)
	previousExists, previousHash, previousErr := transactionFileState(previousPath, previousPrivate)
	nextExists, nextHash, nextErr := transactionFileState(nextPath, true)
	if targetErr != nil || previousErr != nil || nextErr != nil {
		return ErrToolchainLayout
	}
	targetIsPrevious := targetExists && targetHash == transaction.PreviousSHA256
	targetIsNext := targetExists && transaction.NextSHA256 != "" && targetHash == transaction.NextSHA256
	previousIsExpected := previousExists && previousHash == transaction.PreviousSHA256
	nextIsExpected := nextExists && nextHash == transaction.NextSHA256

	if transaction.Kind == "pending-delete" {
		if targetExists && !targetIsPrevious {
			return ErrToolchainLayout
		}
		if !targetExists && previousExists && !previousIsExpected {
			if rollbackErr := renameToolchainNoReplace(previousPath, target); rollbackErr != nil {
				return fmt.Errorf("%w: delete transaction rollback: %v", ErrToolchainLayout, rollbackErr)
			}
			if cleanupErr := cleanupStagedTransaction(descriptorPath, nextPath, transaction); cleanupErr != nil {
				return fmt.Errorf("%w: delete recovery cleanup: %v", ErrToolchainLayout, cleanupErr)
			}
			return ErrToolchainLayout
		}
		if targetIsPrevious && !previousExists {
			if err := renameToolchainNoReplace(target, previousPath); err != nil {
				return ErrToolchainLayout
			}
			previousIsExpected = true
		}
		if previousExists && !previousIsExpected {
			return ErrToolchainLayout
		}
		if previousIsExpected {
			if err := removeTransactionFile(previousPath, transaction.PreviousSHA256, true); err != nil {
				return err
			}
		}
		return removeTransactionDescriptor(descriptorPath, transaction)
	}

	if targetExists && !targetIsPrevious && !targetIsNext {
		return ErrToolchainLayout
	}
	if previousExists && !previousIsExpected {
		if !targetExists {
			if rollbackErr := renameToolchainNoReplace(previousPath, target); rollbackErr != nil {
				return fmt.Errorf("%w: replace transaction rollback: %v", ErrToolchainLayout, rollbackErr)
			}
			if cleanupErr := cleanupStagedTransaction(descriptorPath, nextPath, transaction); cleanupErr != nil {
				return fmt.Errorf("%w: replace recovery cleanup: %v", ErrToolchainLayout, cleanupErr)
			}
		}
		return ErrToolchainLayout
	}
	if nextExists && !nextIsExpected {
		return ErrToolchainLayout
	}
	if targetIsPrevious && !previousExists && nextIsExpected {
		if err := renameToolchainNoReplace(target, previousPath); err != nil {
			return ErrToolchainLayout
		}
		previousIsExpected = true
		targetExists = false
	}
	if !targetExists && previousIsExpected && nextIsExpected {
		if err := renameToolchainNoReplace(nextPath, target); err != nil {
			return ErrToolchainLayout
		}
		targetIsNext = true
		nextExists = false
	}
	if !targetIsNext {
		return ErrToolchainLayout
	}
	if !transactionFileMatches(target, transaction.NextSHA256, true, nil) {
		return ErrToolchainLayout
	}
	if previousIsExpected {
		if err := removeTransactionFile(previousPath, transaction.PreviousSHA256, previousPrivate); err != nil {
			return err
		}
	}
	if nextExists {
		if err := removeTransactionFile(nextPath, transaction.NextSHA256, true); err != nil {
			return err
		}
	}
	return removeTransactionDescriptor(descriptorPath, transaction)
}

func replaceToolchainPendingTransactional(teamkitRoot, path string, previous ToolchainPending, identity *openedPrivateRecord, next ToolchainPending, afterVerify, afterPreviousRename, afterNextRename func() error) (*openedPrivateRecord, error) {
	if err := verifyToolchainPendingState(path, previous, identity); err != nil {
		return nil, err
	}
	previousData, err := marshalToolchainPending(previous)
	if err != nil {
		return nil, err
	}
	nextData, err := marshalToolchainPending(next)
	if err != nil {
		return nil, err
	}
	transaction := toolchainFileTransaction{
		SchemaVersion:     toolchainPendingSchemaVersion,
		Kind:              "pending-progress",
		Nonce:             previous.Nonce,
		TreeSHA256:        previous.Lock.TreeSHA256,
		PreviousCompleted: append([]string(nil), previous.Completed...),
		NextCompleted:     append([]string(nil), next.Completed...),
	}
	if err := beginToolchainFileTransaction(teamkitRoot, path, transaction, previousData, nextData, true, identity.info, afterVerify, afterPreviousRename, afterNextRename); err != nil {
		return nil, err
	}
	identity.Close()
	loaded, exists, nextIdentity, err := loadToolchainPending(path)
	if err != nil || !exists || !sameToolchainPending(loaded, next) {
		return nil, ErrToolchainLayout
	}
	return nextIdentity, nil
}

func verifyToolchainPendingState(path string, expected ToolchainPending, identity *openedPrivateRecord) error {
	if identity == nil || identity.verify(path) != nil {
		return ErrToolchainLayout
	}
	data, err := identity.readAgain(maxToolchainLock)
	if err != nil {
		return ErrToolchainLayout
	}
	var loaded ToolchainPending
	if decodeStrictJSON(data, &loaded) != nil || !sameToolchainPending(loaded, expected) {
		return ErrToolchainLayout
	}
	return nil
}

func sameToolchainPending(left, right ToolchainPending) bool {
	if left.SchemaVersion != right.SchemaVersion || left.Nonce != right.Nonce || !sameToolchainLock(left.Lock, right.Lock) || len(left.Completed) != len(right.Completed) {
		return false
	}
	for index := range left.Completed {
		if left.Completed[index] != right.Completed[index] {
			return false
		}
	}
	return true
}

func writePrivateNoReplace(path string, data []byte) error {
	if err := pathsafe.EnsureDirectory(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := privatefile.CreateTemp(filepath.Dir(path), ".teamkit-publish-", "", 0o600)
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := renameToolchainNoReplace(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func loadFinalToolchainLock(path string, expected *ToolchainLock, teamkitRoot string, externalBinding *boundDirectory, afterVerify, afterPreviousRename, afterNextRename func() error) (ToolchainLock, bool, error) {
	lock, err := readToolchainLock(path)
	if os.IsNotExist(err) {
		return ToolchainLock{}, false, nil
	}
	if errors.Is(err, privatefile.ErrUnsafePermissions) && expected != nil {
		if normalizeErr := normalizeExactLegacyFinalLock(path, *expected, teamkitRoot, externalBinding, afterVerify, afterPreviousRename, afterNextRename); normalizeErr != nil {
			return ToolchainLock{}, false, fmt.Errorf("%w: unsafe legacy final lock: %w", ErrToolchainLayout, normalizeErr)
		}
		lock, err = readToolchainLock(path)
	}
	if err != nil {
		return ToolchainLock{}, false, fmt.Errorf("%w: final lock: %v", ErrToolchainLayout, err)
	}
	if err := validateToolchainLock(lock); err != nil {
		return ToolchainLock{}, false, ErrToolchainLayout
	}
	return lock, true, nil
}

type openedPrivateRecord struct {
	file *os.File
	info os.FileInfo
}

func (record *openedPrivateRecord) Close() {
	if record != nil && record.file != nil {
		_ = record.file.Close()
		record.file = nil
	}
}

func (record *openedPrivateRecord) verify(path string) error {
	if record == nil || record.file == nil || record.info == nil {
		return ErrToolchainLayout
	}
	opened, err := record.file.Stat()
	named, namedErr := os.Lstat(path)
	if err != nil || namedErr != nil || !opened.Mode().IsRegular() || !os.SameFile(record.info, opened) || !os.SameFile(opened, named) {
		return ErrToolchainLayout
	}
	return nil
}

func (record *openedPrivateRecord) readAgain(limit int64) ([]byte, error) {
	if record == nil || record.file == nil {
		return nil, ErrToolchainLayout
	}
	if _, err := record.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(record.file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrToolchainLayout
	}
	after, err := record.file.Stat()
	if err != nil || !os.SameFile(record.info, after) || after.Size() != int64(len(data)) {
		return nil, ErrToolchainLayout
	}
	return data, nil
}

func readBoundedPrivate(path string, limit int64) ([]byte, bool, *openedPrivateRecord, error) {
	if err := pathsafe.ValidateRegular(path); err != nil {
		return nil, false, nil, err
	}
	input, err := privatefile.OpenValidated(path)
	if os.IsNotExist(err) {
		return nil, false, nil, nil
	}
	if err != nil {
		return nil, false, nil, err
	}
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		_ = input.Close()
		return nil, false, nil, ErrToolchainLayout
	}
	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil || int64(len(data)) > limit {
		_ = input.Close()
		return nil, false, nil, ErrToolchainLayout
	}
	openedAfter, err := input.Stat()
	named, namedErr := os.Lstat(path)
	if err != nil || namedErr != nil || !os.SameFile(info, openedAfter) || !os.SameFile(openedAfter, named) || openedAfter.Size() != int64(len(data)) {
		_ = input.Close()
		return nil, false, nil, ErrToolchainLayout
	}
	return data, true, &openedPrivateRecord{file: input, info: info}, nil
}

func readBoundedLegacyRegular(path string, limit int64) ([]byte, *openedPrivateRecord, error) {
	if err := pathsafe.ValidateRegular(path); err != nil {
		return nil, nil, err
	}
	input, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		_ = input.Close()
		return nil, nil, ErrToolchainLayout
	}
	named, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, named) {
		_ = input.Close()
		return nil, nil, ErrToolchainLayout
	}
	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil || int64(len(data)) > limit {
		_ = input.Close()
		return nil, nil, ErrToolchainLayout
	}
	after, err := input.Stat()
	namedAfter, namedErr := os.Lstat(path)
	if err != nil || namedErr != nil || !os.SameFile(info, after) || !os.SameFile(after, namedAfter) || after.Size() != int64(len(data)) {
		_ = input.Close()
		return nil, nil, ErrToolchainLayout
	}
	return data, &openedPrivateRecord{file: input, info: info}, nil
}

func normalizeExactLegacyFinalLock(path string, expected ToolchainLock, teamkitRoot string, externalBinding *boundDirectory, afterVerify, afterPreviousRename, afterNextRename func() error) error {
	data, record, err := readBoundedLegacyRegular(path, maxToolchainLock)
	if err != nil {
		return err
	}
	defer record.Close()
	var lock ToolchainLock
	if err := decodeStrictJSON(data, &lock); err != nil || validateToolchainLock(lock) != nil || !sameToolchainLock(lock, expected) {
		return ErrToolchainLayout
	}
	if err := verifyBoundDirectories(externalBinding); err != nil {
		record.Close()
		return err
	}
	canonical, err := json.Marshal(expected)
	if err != nil {
		record.Close()
		return err
	}
	info := record.info
	record.Close()
	transaction := toolchainFileTransaction{
		SchemaVersion: toolchainPendingSchemaVersion,
		Kind:          "legacy-final",
		Nonce:         expected.TreeSHA256[:32],
		TreeSHA256:    expected.TreeSHA256,
	}
	beforeMutation := func() error {
		if err := verifyBoundDirectories(externalBinding); err != nil {
			return err
		}
		if afterVerify != nil {
			if err := afterVerify(); err != nil {
				return err
			}
		}
		return verifyBoundDirectories(externalBinding)
	}
	if err := beginToolchainFileTransaction(teamkitRoot, path, transaction, data, canonical, false, info, beforeMutation, afterPreviousRename, afterNextRename); err != nil {
		return err
	}
	if err := verifyBoundDirectories(externalBinding); err != nil {
		return err
	}
	normalized, readErr := readToolchainLock(path)
	if readErr != nil || !sameToolchainLock(normalized, expected) {
		return ErrToolchainLayout
	}
	return nil
}

func containsSkill(completed []string, skill string) bool {
	index := sort.SearchStrings(completed, skill)
	return index < len(completed) && completed[index] == skill
}

func filesForSkill(files []ToolchainFile, skill string) []ToolchainFile {
	prefix := skill + "/"
	selected := make([]ToolchainFile, 0)
	for _, file := range files {
		if file.Path == skill || strings.HasPrefix(file.Path, prefix) {
			selected = append(selected, file)
		}
	}
	return selected
}

func verifyCompletedSkill(skillsRoot, skill string, files []ToolchainFile) error {
	path := filepath.Join(skillsRoot, skill)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return ErrToolchainLayout
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrToolchainLayout
	}
	actual, err := selectedToolchainManifest(skillsRoot, []string{skill})
	if err != nil {
		return ErrToolchainLayout
	}
	expected := filesForSkill(files, skill)
	if !sameToolchainFiles(actual, expected) {
		return ErrToolchainCollision
	}
	return nil
}

func skillMatchesManifest(skillsRoot, skill string, files []ToolchainFile) bool {
	actual, err := selectedToolchainManifest(skillsRoot, []string{skill})
	return err == nil && sameToolchainFiles(actual, filesForSkill(files, skill))
}

func sameToolchainFiles(left, right []ToolchainFile) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func stageToolchainSkill(source, staged, skill string, files []ToolchainFile) error {
	if info, err := os.Lstat(staged); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrToolchainLayout
		}
		stagingRoot := filepath.Dir(staged)
		actual, manifestErr := selectedToolchainManifest(stagingRoot, []string{skill})
		if manifestErr != nil || !sameToolchainFiles(actual, filesForSkill(files, skill)) {
			return ErrToolchainLayout
		}
		return nil
	} else if !os.IsNotExist(err) {
		return ErrToolchainLayout
	}
	count, total := 0, int64(0)
	if err := copySkillTree(source, staged, &count, &total); err != nil {
		return err
	}
	actual, err := selectedToolchainManifest(filepath.Dir(staged), []string{skill})
	if err != nil || !sameToolchainFiles(actual, filesForSkill(files, skill)) {
		return ErrToolchainLayout
	}
	return nil
}

func stagingTransactionPaths(teamkitRoot, nonce string) (descriptor, archive string) {
	return filepath.Join(teamkitRoot, "toolchain-staging-txn.json"), filepath.Join(teamkitRoot, "toolchain-staging-trash-"+nonce)
}

func marshalStagingTransaction(transaction toolchainStagingTransaction) ([]byte, error) {
	data, err := json.Marshal(transaction)
	if err != nil || int64(len(data)) > maxToolchainLock {
		return nil, ErrToolchainLayout
	}
	return data, nil
}

func stagingTokenPath(directory string) string {
	return filepath.Join(directory, ".teamkit-staging-token.json")
}

func stagingMarkerPath(teamkitRoot, nonce string) string {
	return filepath.Join(teamkitRoot, "toolchain-staging-marker-"+nonce+".json")
}

func stagingDirectoryMatches(path string, expectedInfo os.FileInfo, tokenHash string) bool {
	binding, err := openBoundDirectory(path)
	if err != nil {
		return false
	}
	defer binding.Close()
	entries, readErr := binding.file.ReadDir(2)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return false
	}
	return len(entries) == 1 && entries[0].Name() == ".teamkit-staging-token.json" && (expectedInfo == nil || os.SameFile(expectedInfo, binding.info)) && transactionFileMatches(stagingTokenPath(path), tokenHash, true, nil)
}

func finishStagingTransaction(descriptorPath, archive, marker string, transaction toolchainStagingTransaction, tokenHash string, afterTokenRemoved, afterArchiveRemoved func() error) error {
	markerExists, markerHash, markerErr := transactionFileState(marker, true)
	if markerErr != nil || (markerExists && markerHash != tokenHash) {
		return ErrToolchainLayout
	}
	if _, err := os.Lstat(archive); err == nil {
		if transactionFileMatches(stagingTokenPath(archive), tokenHash, true, nil) {
			if !markerExists {
				markerData, marshalErr := marshalStagingTransaction(transaction)
				if marshalErr != nil {
					return marshalErr
				}
				if err := writePrivateNoReplace(marker, markerData); err != nil {
					return ErrToolchainLayout
				}
				markerExists = true
			}
			if err := removeTransactionFile(stagingTokenPath(archive), tokenHash, true); err != nil {
				return err
			}
			if afterTokenRemoved != nil {
				if err := afterTokenRemoved(); err != nil {
					return err
				}
			}
		} else if !markerExists {
			return ErrToolchainLayout
		}
		entries, err := readDirectoryBounded(archive, 0)
		if err != nil || len(entries) != 0 {
			return ErrToolchainLayout
		}
		if err := os.Remove(archive); err != nil {
			return err
		}
		if afterArchiveRemoved != nil {
			if err := afterArchiveRemoved(); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if !markerExists {
		return ErrToolchainLayout
	}
	if err := removeTransactionFile(marker, tokenHash, true); err != nil {
		return err
	}
	descriptorData, err := marshalStagingTransaction(transaction)
	if err != nil {
		return err
	}
	return removeTransactionFile(descriptorPath, bytesSHA256(descriptorData), true)
}

func archiveEmptyStaging(teamkitRoot, path, nonce, treeSHA256 string, afterVerify func(string) error, afterRename, afterTokenRemoved, afterArchiveRemoved func() error) error {
	if !validToolchainNonce(nonce) || len(treeSHA256) != sha256.Size*2 {
		return ErrToolchainLayout
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return ErrToolchainLayout
	}
	binding, err := openBoundDirectory(path)
	if err != nil {
		return ErrToolchainLayout
	}
	entries, readErr := binding.file.ReadDir(1)
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || len(entries) != 0 || binding.verify() != nil {
		binding.Close()
		return ErrToolchainLayout
	}
	expectedInfo := binding.info
	binding.Close()
	transaction := toolchainStagingTransaction{SchemaVersion: toolchainPendingSchemaVersion, Nonce: nonce, TreeSHA256: treeSHA256}
	tokenData, err := marshalStagingTransaction(transaction)
	if err != nil {
		return err
	}
	tokenHash := bytesSHA256(tokenData)
	descriptorPath, archive := stagingTransactionPaths(teamkitRoot, nonce)
	marker := stagingMarkerPath(teamkitRoot, nonce)
	if err := writePrivateNoReplace(stagingTokenPath(path), tokenData); err != nil {
		return ErrToolchainLayout
	}
	if err := writePrivateNoReplace(descriptorPath, tokenData); err != nil {
		return ErrToolchainLayout
	}
	if !stagingDirectoryMatches(path, expectedInfo, tokenHash) {
		return ErrToolchainLayout
	}
	if afterVerify != nil {
		if err := afterVerify(path); err != nil {
			return err
		}
	}
	if err := renameToolchainNoReplace(path, archive); err != nil {
		return ErrToolchainLayout
	}
	if afterRename != nil {
		if err := afterRename(); err != nil {
			return err
		}
	}
	if !stagingDirectoryMatches(archive, expectedInfo, tokenHash) {
		if rollbackErr := renameToolchainNoReplace(archive, path); rollbackErr != nil {
			return fmt.Errorf("%w: staging transaction rollback: %v", ErrToolchainLayout, rollbackErr)
		}
		return ErrToolchainLayout
	}
	return finishStagingTransaction(descriptorPath, archive, marker, transaction, tokenHash, afterTokenRemoved, afterArchiveRemoved)
}

func reconcileStagingTransaction(teamkitRoot string, expected ToolchainLock) error {
	descriptorPath := filepath.Join(teamkitRoot, "toolchain-staging-txn.json")
	data, exists, record, err := readBoundedPrivate(descriptorPath, maxToolchainLock)
	if err != nil {
		return ErrToolchainLayout
	}
	if !exists {
		entries, readErr := readDirectoryBounded(teamkitRoot, maxToolchainRootEntries)
		if readErr != nil {
			return ErrToolchainLayout
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "toolchain-staging-trash-") {
				return ErrToolchainLayout
			}
			if strings.HasPrefix(entry.Name(), "toolchain-staging-marker-") {
				return ErrToolchainLayout
			}
			if strings.HasPrefix(entry.Name(), "toolchain-staging-") && entry.IsDir() {
				staging := filepath.Join(teamkitRoot, entry.Name())
				tokenPath := stagingTokenPath(staging)
				tokenData, tokenExists, tokenRecord, tokenErr := readBoundedPrivate(tokenPath, maxToolchainLock)
				if tokenErr != nil {
					return ErrToolchainLayout
				}
				if tokenExists {
					tokenRecord.Close()
					var orphan toolchainStagingTransaction
					if decodeStrictJSON(tokenData, &orphan) != nil || orphan.SchemaVersion != toolchainPendingSchemaVersion || orphan.TreeSHA256 != expected.TreeSHA256 || entry.Name() != "toolchain-staging-"+orphan.Nonce {
						return ErrToolchainLayout
					}
					if err := removeTransactionFile(tokenPath, bytesSHA256(tokenData), true); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	record.Close()
	var transaction toolchainStagingTransaction
	if decodeStrictJSON(data, &transaction) != nil || transaction.SchemaVersion != toolchainPendingSchemaVersion || !validToolchainNonce(transaction.Nonce) || transaction.TreeSHA256 != expected.TreeSHA256 {
		return ErrToolchainLayout
	}
	tokenHash := bytesSHA256(data)
	original := toolchainStagingPath(teamkitRoot, transaction.Nonce)
	_, archive := stagingTransactionPaths(teamkitRoot, transaction.Nonce)
	marker := stagingMarkerPath(teamkitRoot, transaction.Nonce)
	originalExists := false
	if _, statErr := os.Lstat(original); statErr == nil {
		originalExists = true
	} else if !os.IsNotExist(statErr) {
		return ErrToolchainLayout
	}
	archiveExists := false
	if _, statErr := os.Lstat(archive); statErr == nil {
		archiveExists = true
	} else if !os.IsNotExist(statErr) {
		return ErrToolchainLayout
	}
	if originalExists {
		if archiveExists || !stagingDirectoryMatches(original, nil, tokenHash) {
			return ErrToolchainLayout
		}
		if err := renameToolchainNoReplace(original, archive); err != nil {
			return ErrToolchainLayout
		}
		archiveExists = true
	}
	if archiveExists {
		if !stagingDirectoryMatches(archive, nil, tokenHash) {
			markerExists, markerHash, markerErr := transactionFileState(marker, true)
			if markerErr != nil || !markerExists || markerHash != tokenHash {
				if rollbackErr := renameToolchainNoReplace(archive, original); rollbackErr != nil {
					return fmt.Errorf("%w: staging recovery rollback: %v", ErrToolchainLayout, rollbackErr)
				}
				return ErrToolchainLayout
			}
		}
	}
	return finishStagingTransaction(descriptorPath, archive, marker, transaction, tokenHash, nil, nil)
}

func removePendingCAS(teamkitRoot, path string, pending ToolchainPending, identity *openedPrivateRecord, afterVerify, afterRename func() error) error {
	if err := verifyToolchainPendingState(path, pending, identity); err != nil {
		return err
	}
	previousData, err := marshalToolchainPending(pending)
	if err != nil {
		return err
	}
	transaction := toolchainFileTransaction{
		SchemaVersion:     toolchainPendingSchemaVersion,
		Kind:              "pending-delete",
		Nonce:             pending.Nonce,
		TreeSHA256:        pending.Lock.TreeSHA256,
		PreviousCompleted: append([]string(nil), pending.Completed...),
	}
	if err := beginToolchainFileTransaction(teamkitRoot, path, transaction, previousData, nil, true, identity.info, afterVerify, afterRename, nil); err != nil {
		return err
	}
	identity.Close()
	return nil
}

func readToolchainLock(path string) (ToolchainLock, error) {
	data, exists, record, err := readBoundedPrivate(path, maxToolchainLock)
	if err != nil {
		return ToolchainLock{}, err
	}
	if !exists {
		return ToolchainLock{}, fs.ErrNotExist
	}
	defer record.Close()
	var lock ToolchainLock
	if err := decodeStrictJSON(data, &lock); err != nil {
		return ToolchainLock{}, err
	}
	return lock, nil
}

func decodeStrictJSON(data []byte, target any) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return ErrToolchainLayout
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrToolchainLayout
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var parseValue func() error
	parseValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, isDelimiter := token.(json.Delim)
		if !isDelimiter {
			return nil
		}
		switch delimiter {
		case '{':
			keys := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return ErrToolchainLayout
				}
				if _, duplicate := keys[key]; duplicate {
					return ErrToolchainLayout
				}
				keys[key] = struct{}{}
				if err := parseValue(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return ErrToolchainLayout
			}
		case '[':
			for decoder.More() {
				if err := parseValue(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return ErrToolchainLayout
			}
		default:
			return ErrToolchainLayout
		}
		return nil
	}
	if err := parseValue(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrToolchainLayout
	}
	return nil
}

func writeToolchainLock(path string, lock ToolchainLock) error {
	data, err := json.Marshal(lock)
	if err != nil {
		return err
	}
	if int64(len(data)) > maxToolchainLock {
		return ErrToolchainLayout
	}
	if err := privatefile.WriteAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("%w: write final lock: %v", ErrToolchainLayout, err)
	}
	return nil
}

func writeToolchainLockNoReplace(path string, lock ToolchainLock) error {
	data, err := json.Marshal(lock)
	if err != nil {
		return err
	}
	if int64(len(data)) > maxToolchainLock {
		return ErrToolchainLayout
	}
	if err := writePrivateNoReplace(path, data); err != nil {
		return ErrToolchainCollision
	}
	return nil
}

func validateToolchainLock(lock ToolchainLock) error {
	if lock.Toolchain == "" || lock.Origin == "" || lock.Commit == "" || len(lock.Commit) != 40 || len(lock.InstalledSkills) == 0 || len(lock.InstalledSkills) > maxToolchainSkills || len(lock.Files) == 0 || len(lock.Files) > maxToolchainFiles {
		return ErrToolchainLayout
	}
	if _, err := checkedSkillNames(lock.InstalledSkills, false); err != nil {
		return ErrToolchainLayout
	}
	if !sort.StringsAreSorted(lock.InstalledSkills) {
		return ErrToolchainLayout
	}
	selected := make(map[string]bool, len(lock.InstalledSkills))
	for _, skill := range lock.InstalledSkills {
		selected[skill] = false
	}
	previous := ""
	for _, file := range lock.Files {
		if file.Path == "" || strings.Contains(file.Path, "\\") || filepath.IsAbs(file.Path) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Path))) != file.Path || file.Path == "." || strings.HasPrefix(file.Path, "../") {
			return ErrToolchainLayout
		}
		if previous != "" && file.Path <= previous {
			return ErrToolchainLayout
		}
		previous = file.Path
		top := strings.SplitN(file.Path, "/", 2)[0]
		if _, exists := selected[top]; !exists {
			return ErrToolchainLayout
		}
		if file.Path == top {
			if file.SHA256 != "" {
				return ErrToolchainLayout
			}
			selected[top] = true
		} else if file.SHA256 != "" {
			if len(file.SHA256) != sha256.Size*2 || file.SHA256 != strings.ToLower(file.SHA256) {
				return ErrToolchainLayout
			}
			if _, err := hex.DecodeString(file.SHA256); err != nil {
				return ErrToolchainLayout
			}
		}
	}
	for _, hasRoot := range selected {
		if !hasRoot {
			return ErrToolchainLayout
		}
	}
	expectedTree := toolchainTreeSHA256(lock.Toolchain, lock.Origin, lock.Commit, lock.InstalledSkills, lock.Files)
	if lock.TreeSHA256 != expectedTree {
		return ErrToolchainLayout
	}
	return nil
}

func sameToolchainLock(left, right ToolchainLock) bool {
	if left.Toolchain != right.Toolchain || left.Origin != right.Origin || left.Commit != right.Commit || left.TreeSHA256 != right.TreeSHA256 || len(left.InstalledSkills) != len(right.InstalledSkills) || len(left.Files) != len(right.Files) {
		return false
	}
	for index := range left.InstalledSkills {
		if left.InstalledSkills[index] != right.InstalledSkills[index] {
			return false
		}
	}
	for index := range left.Files {
		if left.Files[index] != right.Files[index] {
			return false
		}
	}
	return true
}

func toolchainTreeSHA256(toolchain domain.Toolchain, origin, commit string, skills []string, files []ToolchainFile) string {
	canonicalSkills := append([]string(nil), skills...)
	canonicalFiles := append([]ToolchainFile(nil), files...)
	sort.Strings(canonicalSkills)
	sort.Slice(canonicalFiles, func(i, j int) bool { return canonicalFiles[i].Path < canonicalFiles[j].Path })
	payload, err := json.Marshal(struct {
		SchemaVersion int              `json:"schema_version"`
		Toolchain     domain.Toolchain `json:"toolchain"`
		Origin        string           `json:"origin"`
		Commit        string           `json:"commit"`
		Skills        []string         `json:"skills"`
		Files         []ToolchainFile  `json:"files"`
	}{1, toolchain, origin, commit, canonicalSkills, canonicalFiles})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func installedSkillsMatch(root string, expected []string, files []ToolchainFile) bool {
	if err := pathsafe.ValidateDirectory(root); err != nil {
		return false
	}
	for _, skill := range expected {
		skillPath := filepath.Join(root, skill, "SKILL.md")
		if err := pathsafe.ValidateDirectory(filepath.Dir(skillPath)); err != nil {
			return false
		}
		if err := pathsafe.ValidateRegular(skillPath); err != nil {
			return false
		}
		info, err := os.Lstat(skillPath)
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	actual, err := selectedToolchainManifest(root, expected)
	if err != nil || len(actual) != len(files) {
		return false
	}
	for index := range files {
		if actual[index] != files[index] {
			return false
		}
	}
	return true
}

// selectedToolchainManifest binds only the selected skill directories. Regular
// service files at the skills root (for example .gitignore) are repository
// metadata: they are neither installed nor included in the toolchain lock.
func selectedToolchainManifest(root string, skills []string) ([]ToolchainFile, error) {
	selected, err := checkedSkillNames(skills, false)
	if err != nil {
		return nil, ErrToolchainLayout
	}
	return toolchainManifestSelected(root, selected)
}

func toolchainManifestSelected(root string, selected map[string]struct{}) ([]ToolchainFile, error) {
	if err := regularDirectory(root); err != nil {
		return nil, ErrToolchainLayout
	}
	manifest := make([]ToolchainFile, 0)
	entries, total := 0, int64(0)
	walkRoot := func(base string) error {
		visited, err := walkToolchainTree(base, maxToolchainFiles-entries, func(path string, entry fs.DirEntry) error {
			relative, err := filepath.Rel(root, path)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return ErrToolchainLayout
			}
			relative = filepath.ToSlash(relative)
			if entry.Type()&os.ModeSymlink != 0 {
				return ErrToolchainLayout
			}
			if entry.IsDir() {
				if err := pathsafe.ValidateDirectory(path); err != nil {
					return ErrToolchainLayout
				}
				manifest = append(manifest, ToolchainFile{Path: relative})
				return nil
			}
			if err := pathsafe.ValidateRegular(path); err != nil {
				return ErrToolchainLayout
			}
			hash, read, err := hashRegularFileBounded(path, maxToolchainBytes-total)
			if err != nil {
				return err
			}
			total += read
			manifest = append(manifest, ToolchainFile{Path: relative, SHA256: hash})
			return nil
		})
		entries += visited
		return err
	}
	if selected == nil {
		if err := walkRoot(root); err != nil {
			return nil, err
		}
	} else {
		names := make([]string, 0, len(selected))
		for name := range selected {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			path := filepath.Join(root, name)
			if err := regularDirectory(path); err != nil {
				return nil, ErrToolchainLayout
			}
			if err := walkRoot(path); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(manifest, func(i, j int) bool { return manifest[i].Path < manifest[j].Path })
	return manifest, nil
}

func walkToolchainTree(root string, limit int, visit func(string, fs.DirEntry) error) (int, error) {
	if limit <= 0 {
		return 0, ErrToolchainLayout
	}
	visited := 0
	var walk func(string, int) error
	walk = func(path string, depth int) error {
		if depth > 64 || visited >= limit {
			return ErrToolchainLayout
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrToolchainLayout
		}
		entry := fs.FileInfoToDirEntry(info)
		visited++
		if err := visit(path, entry); err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		children, err := readDirectoryBounded(path, limit-visited)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := walk(filepath.Join(path, child.Name()), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	err := walk(root, 0)
	return visited, err
}

func hashRegularFileBounded(path string, remaining int64) (string, int64, error) {
	if remaining < 0 || pathsafe.ValidateRegular(path) != nil {
		return "", 0, ErrToolchainLayout
	}
	input, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > remaining {
		return "", 0, ErrToolchainLayout
	}
	named, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, named) {
		return "", 0, ErrToolchainLayout
	}
	digest := sha256.New()
	read, copyErr := io.CopyN(digest, input, remaining+1)
	if errors.Is(copyErr, io.EOF) {
		copyErr = nil
	}
	after, statErr := input.Stat()
	namedAfter, namedErr := os.Lstat(path)
	if copyErr != nil || read > remaining || statErr != nil || namedErr != nil || !os.SameFile(info, after) || !os.SameFile(after, namedAfter) || after.Size() != read || read != info.Size() {
		return "", 0, ErrToolchainLayout
	}
	return hex.EncodeToString(digest.Sum(nil)), read, nil
}
