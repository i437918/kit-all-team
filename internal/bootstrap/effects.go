// Package bootstrap implements the imperative reconciliation effects.
package bootstrap

import (
	"bytes"
	"context"
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
	"runtime"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/apps"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/config"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

const DefaultCertificateSHA256 = "88d85e7e7d64c061c195f93c517500bdc91fccfb9b5a8115da9f6a5a17e689f8"

var (
	ErrForeignWorkspace    = errors.New("FOREIGN_WORKSPACE")
	ErrForeignProfile      = errors.New("FOREIGN_HERMES_PROFILE")
	ErrCertificateChecksum = errors.New("CERTIFICATE_ARCHIVE_CHECKSUM_MISMATCH")
	ErrCertificateRequired = errors.New("CERTIFICATE_ARCHIVE_REQUIRED")
	ErrProfileInProgress   = errors.New("HERMES_PROFILE_CREATE_IN_PROGRESS")
	ErrStateVerification   = errors.New("STATE_VERIFICATION_FAILED")
)

type GitPort interface {
	CloneContent(context.Context, string, string, string) error
	UpdateContent(context.Context, string, string, string) error
	CloneDatabase(context.Context, string, string) error
	UpdateDatabase(context.Context, string, string) error
	SyncPinned(context.Context, string, string, string) error
}

type GitPortFunc struct {
	CloneContentFunc   func(context.Context, string, string, string) error
	UpdateContentFunc  func(context.Context, string, string, string) error
	CloneDatabaseFunc  func(context.Context, string, string) error
	UpdateDatabaseFunc func(context.Context, string, string) error
	SyncPinnedFunc     func(context.Context, string, string, string) error
}

func (f GitPortFunc) CloneContent(c context.Context, a, b, d string) error {
	if f.CloneContentFunc == nil {
		return fmt.Errorf("GIT_CLONE_CONTENT_REQUIRED")
	}
	return f.CloneContentFunc(c, a, b, d)
}
func (f GitPortFunc) UpdateContent(c context.Context, a, b, d string) error {
	if f.UpdateContentFunc == nil {
		return fmt.Errorf("GIT_UPDATE_CONTENT_REQUIRED")
	}
	return f.UpdateContentFunc(c, a, b, d)
}
func (f GitPortFunc) CloneDatabase(c context.Context, a, b string) error {
	if f.CloneDatabaseFunc == nil {
		return fmt.Errorf("GIT_CLONE_DATABASE_REQUIRED")
	}
	return f.CloneDatabaseFunc(c, a, b)
}
func (f GitPortFunc) UpdateDatabase(c context.Context, a, b string) error {
	if f.UpdateDatabaseFunc == nil {
		return fmt.Errorf("GIT_UPDATE_DATABASE_REQUIRED")
	}
	return f.UpdateDatabaseFunc(c, a, b)
}
func (f GitPortFunc) SyncPinned(c context.Context, a, b, d string) error {
	if f.SyncPinnedFunc == nil {
		return fmt.Errorf("GIT_SYNC_PINNED_REQUIRED")
	}
	return f.SyncPinnedFunc(c, a, b, d)
}

type InstallerPort interface {
	Apply(context.Context, string) error
}
type InstallerPortFunc func(context.Context, string) error

func (f InstallerPortFunc) Apply(ctx context.Context, path string) error { return f(ctx, path) }

type SecretStore interface {
	Save(map[string]string) (string, error)
}

// ProfilePort is the narrow Hermes profile lifecycle boundary.
type ProfilePort interface {
	Create(context.Context, string) error
	OptInBundledSkills(context.Context, string) error
	Doctor(context.Context, string) error
}

// OfficeCLIPort is the narrow managed OfficeCLI lifecycle boundary.
type OfficeCLIPort interface {
	Path() string
	Ensure(context.Context) error
	Ready(context.Context) (bool, error)
}

// ProfilePortFuncs adapts test and platform functions to ProfilePort.
type ProfilePortFuncs struct {
	CreateFunc             func(context.Context, string) error
	OptInBundledSkillsFunc func(context.Context, string) error
	DoctorFunc             func(context.Context, string) error
}

func (f ProfilePortFuncs) OptInBundledSkills(ctx context.Context, identity string) error {
	if f.OptInBundledSkillsFunc == nil {
		return fmt.Errorf("HERMES_BUNDLED_SKILLS_MIGRATION_REQUIRED")
	}
	return f.OptInBundledSkillsFunc(ctx, identity)
}

func (f ProfilePortFuncs) Create(ctx context.Context, identity string) error {
	if f.CreateFunc == nil {
		return fmt.Errorf("HERMES_PROFILE_CREATE_REQUIRED")
	}
	return f.CreateFunc(ctx, identity)
}

func (f ProfilePortFuncs) Doctor(ctx context.Context, identity string) error {
	if f.DoctorFunc == nil {
		return fmt.Errorf("HERMES_PROFILE_DOCTOR_REQUIRED")
	}
	return f.DoctorFunc(ctx, identity)
}

type Effects struct {
	Git                  GitPort
	Installer            InstallerPort
	InstallerPath        string
	CertificateArchive   string
	CertificateSHA256    string
	Secrets              SecretStore
	ProfileSecrets       SecretStore
	ProfileEnvironment   map[string]string
	Profile              ProfilePort
	OfficeCLI            OfficeCLIPort
	HermesEnvironment    func(string) error
	HermesExecutable     string
	RuntimeContract      hermes.RuntimeContract
	RuntimeProbe         RuntimeProbe
	ToolchainMaterialize ToolchainMaterializer
	ManagedInstallReady  func(string) (bool, error)
	InstallHooks         func(string) error
}

// RuntimeProbe re-verifies the exact Hermes executable used by an operation.
type RuntimeProbe func(context.Context, string) (hermes.RuntimeContract, error)

// ToolchainMaterializer installs one selected external toolchain beside bundled skills.
type ToolchainMaterializer func(string, string, catalog.Toolchain) error

func (e *Effects) Observe(ctx context.Context, desired domain.DesiredState, update reconcile.UpdateChoice) (reconcile.ObservedState, error) {
	if err := validateHomes(desired); err != nil {
		return reconcile.ObservedState{}, err
	}
	if err := validateWorkspacePaths(desired); err != nil {
		return reconcile.ObservedState{}, err
	}
	root := desired.KitHome()
	entries, err := rootEntries(root)
	if err != nil {
		return reconcile.ObservedState{}, err
	}
	gitReady, err := safeGitDirectory(filepath.Join(root, ".git"))
	if err != nil {
		return reconcile.ObservedState{}, err
	}
	contentReady, err := contentMarkerReady(root, projectContentBranch(desired))
	if err != nil {
		return reconcile.ObservedState{}, err
	}
	contentReady = contentReady && gitReady
	ownerReady, err := ownerMatches(root, desired.Project())
	if err != nil {
		return reconcile.ObservedState{}, err
	}
	publicStateReady, err := publicDesiredStateMatches(root, desired)
	if err != nil {
		return reconcile.ObservedState{}, err
	}
	if len(entries) > 0 && !contentReady && !ownerReady {
		return reconcile.ObservedState{}, ErrForeignWorkspace
	}
	databaseGitReady, err := safeGitDirectory(filepath.Join(root, "db", ".git"))
	if err != nil {
		return reconcile.ObservedState{}, err
	}
	databaseMarker, err := databaseMarkerReady(root)
	if err != nil {
		return reconcile.ObservedState{}, err
	}
	databaseReady := databaseGitReady && databaseMarker
	if databaseReady {
		hooksReady, hooksErr := gitx.HooksReady(filepath.Join(root, "db", ".git", "hooks"))
		if hooksErr != nil {
			return reconcile.ObservedState{}, hooksErr
		}
		databaseReady = hooksReady
	}
	toolchainReady := true
	applicationReady := false
	if desired.Application() == domain.AppHermes {
		if err := validateHermesProfilePath(desired); err != nil {
			return reconcile.ObservedState{}, err
		}
		installReady := true
		if desired.AppInstalled() {
			installReady, err = hermes.ExecutableReady(e.HermesExecutable)
			if err != nil {
				return reconcile.ObservedState{}, err
			}
		} else {
			installReady, err = e.managedHermesInstallReady(desired)
			if err != nil {
				return reconcile.ObservedState{}, err
			}
		}
		owned, ownerErr := profileOwnerMatches(desired)
		if ownerErr != nil {
			return reconcile.ObservedState{}, ownerErr
		}
		profileInfo, profileErr := os.Lstat(profileDirectory(desired))
		if profileErr != nil && !errors.Is(profileErr, fs.ErrNotExist) {
			return reconcile.ObservedState{}, profileErr
		}
		creating, creatingErr := profileCreatingMatches(desired)
		if creatingErr != nil {
			return reconcile.ObservedState{}, creatingErr
		}
		if profileErr == nil {
			if !profileInfo.IsDir() || unsafeProfileComponent(profileInfo) {
				return reconcile.ObservedState{}, ErrForeignProfile
			}
			if !owned {
				claim, _, claimErr := readProfileClaim(desired)
				if claimErr != nil {
					return reconcile.ObservedState{}, claimErr
				}
				proof, proofErr := profileAdoptionProofMatches(profileDirectory(desired), claim)
				if proofErr != nil || !creating || !proof {
					return reconcile.ObservedState{}, ErrForeignProfile
				}
			}
		}
		pin, lookupErr := catalog.LookupToolchain(desired.Toolchain())
		if lookupErr != nil {
			return reconcile.ObservedState{}, lookupErr
		}
		if creating && !owned {
			toolchainReady = false
		} else {
			toolchainReady, err = hermes.ToolchainInstalled(profileDirectory(desired), pin)
			if err != nil {
				return reconcile.ObservedState{}, err
			}
		}
		schema := e.RuntimeContract.ConfigSchema
		if schema == 0 {
			schema = hermes.HermesConfigVersion
		}
		if e.OfficeCLI == nil {
			return reconcile.ObservedState{}, fmt.Errorf("OFFICECLI_REQUIRED")
		}
		officeCLIReady, officeCLIError := e.OfficeCLI.Ready(ctx)
		if officeCLIError != nil {
			return reconcile.ObservedState{}, officeCLIError
		}
		configReady, configErr := profileConfigReady(desired, schema, e.OfficeCLI.Path())
		if configErr != nil {
			return reconcile.ObservedState{}, configErr
		}
		certificateReady, certificateErr := e.certificateStateReady(desired)
		if certificateErr != nil {
			return reconcile.ObservedState{}, certificateErr
		}
		legacyMarker := false
		if owned {
			legacyMarker, err = hermes.ExactLegacyOptOutMarker(profileDirectory(desired))
			if err != nil {
				return reconcile.ObservedState{}, err
			}
		}
		applicationReady = installReady && owned && !legacyMarker && configReady && certificateReady && officeCLIReady
	} else {
		applicationReady, err = handoffReady(desired)
		if err != nil {
			return reconcile.ObservedState{}, err
		}
	}
	return reconcile.ObservedState{
		WorkspaceReady: ownerReady && publicStateReady, ContentReady: contentReady, DatabaseReady: databaseReady,
		ToolchainReady: toolchainReady, ApplicationReady: applicationReady,
		NonemptyWorkspace: len(entries) > 0, Update: update,
	}, nil
}

func publicDesiredStateMatches(root string, desired domain.DesiredState) (bool, error) {
	path := filepath.Join(root, ".env")
	if err := pathsafe.ValidateRegular(path); err != nil {
		return false, fmt.Errorf("%w: %v", ErrForeignWorkspace, err)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	parsed, err := config.ParseDotenv(string(data))
	if err != nil || parsed != desired {
		return false, ErrForeignWorkspace
	}
	return true, nil
}

func (e *Effects) Apply(ctx context.Context, desired domain.DesiredState, action reconcile.Action) error {
	if err := validateHomes(desired); err != nil {
		return err
	}
	if err := validateWorkspacePaths(desired); err != nil {
		return err
	}
	if desired.Application() == domain.AppHermes {
		if err := validateHermesProfilePath(desired); err != nil {
			return err
		}
	}
	if err := safeRoot(desired); err != nil {
		return err
	}
	project, err := catalog.LookupProject(desired.Project())
	if err != nil {
		return err
	}
	switch action.Kind {
	case reconcile.ActionPrepareWorkspace:
		if err := pathsafe.EnsureDirectory(desired.KitHome(), 0o700); err != nil {
			return err
		}
		return workspace.EnsureOwner(desired.KitHome(), string(desired.Project()))
	case reconcile.ActionSyncContent:
		if e.Git == nil {
			return fmt.Errorf("GIT_PORT_REQUIRED")
		}
		if ready, markerErr := contentMarkerReady(desired.KitHome(), project.ContentBranch); markerErr != nil {
			return markerErr
		} else if ready {
			if err := e.Git.UpdateContent(ctx, desired.KitHome(), project.ContentRepository, project.ContentBranch); err != nil {
				return err
			}
		} else if err := e.Git.CloneContent(ctx, project.ContentRepository, project.ContentBranch, desired.KitHome()); err != nil {
			return err
		}
		return workspace.WriteFileAtomic(contentMarkerPath(desired.KitHome()), []byte(project.ContentBranch+"\n"), 0o600)
	case reconcile.ActionSyncDatabase:
		if e.Git == nil {
			return fmt.Errorf("GIT_PORT_REQUIRED")
		}
		database := filepath.Join(desired.KitHome(), "db")
		ready, markerErr := databaseMarkerReady(desired.KitHome())
		if markerErr != nil {
			return markerErr
		}
		if ready {
			if err := e.Git.UpdateDatabase(ctx, database, project.DatabaseRepository); err != nil {
				return err
			}
		} else if err := e.Git.CloneDatabase(ctx, project.DatabaseRepository, desired.KitHome()); err != nil {
			return err
		}
		return workspace.WriteFileAtomic(databaseMarkerPath(desired.KitHome()), []byte("develop\n"), 0o600)
	case reconcile.ActionInstallToolchain:
		if desired.Application() != domain.AppHermes {
			return nil
		}
		if e.Git == nil {
			return fmt.Errorf("GIT_PORT_REQUIRED")
		}
		if err := e.ensureHermes(ctx, desired); err != nil {
			return err
		}
		pin, err := catalog.LookupToolchain(desired.Toolchain())
		if err != nil {
			return err
		}
		if err := e.Git.SyncPinned(ctx, pin.Origin, pin.Commit, toolchainPath(desired)); err != nil {
			return err
		}
		materialize := e.ToolchainMaterialize
		if materialize == nil {
			materialize = hermes.MaterializeToolchain
		}
		return materialize(toolchainPath(desired), profileDirectory(desired), pin)
	case reconcile.ActionConfigureApplication:
		if desired.Application() == domain.AppHermes {
			if err := e.ensureHermesCompatibility(ctx, desired); err != nil {
				return err
			}
		}
		return e.configure(ctx, desired)
	case reconcile.ActionVerifyState:
		if desired.Application() == domain.AppHermes {
			if err := e.ensureHermesCompatibility(ctx, desired); err != nil {
				return err
			}
		}
		if err := e.finalize(desired); err != nil {
			return err
		}
		observed, err := e.Observe(ctx, desired, reconcile.UpdateNone)
		if err != nil {
			return err
		}
		if !observed.WorkspaceReady || !observed.ContentReady || !observed.DatabaseReady ||
			!observed.ToolchainReady || !observed.ApplicationReady {
			return ErrStateVerification
		}
		if desired.Application() == domain.AppHermes {
			if e.Profile == nil {
				return fmt.Errorf("HERMES_PROFILE_REQUIRED")
			}
			if err := e.verifyHermesManagedState(ctx, desired); err != nil {
				return err
			}
			if err := writeRoleSoul(desired); err != nil {
				return err
			}
			if err := e.Profile.Doctor(ctx, profileIdentity(desired)); err != nil {
				return err
			}
			if err := e.verifyHermesManagedState(ctx, desired); err != nil {
				return err
			}
			officeCLIReady, err := e.OfficeCLI.Ready(ctx)
			if err != nil {
				return err
			}
			if !officeCLIReady {
				return ErrStateVerification
			}
			return nil
		}
		return nil
	default:
		return fmt.Errorf("ACTION_UNKNOWN: %s", action.Kind)
	}
}

func (e *Effects) configure(ctx context.Context, desired domain.DesiredState) error {
	if desired.Application() != domain.AppHermes {
		handoff, err := apps.PrepareHandoff(apps.Application{ID: string(desired.Application()), Installed: desired.AppInstalled()}, apps.HandoffRequest{Toolchain: mustToolchain(desired), V8StdEndpoint: catalog.V8StdMCP().Endpoint})
		if err != nil {
			return err
		}
		return workspace.WriteFileAtomic(filepath.Join(desired.KitHome(), ".teamkit", "handoff.txt"), []byte(handoff.Command+"\n"), 0o600)
	}
	if err := e.ensureHermes(ctx, desired); err != nil {
		return err
	}
	if e.OfficeCLI == nil {
		return fmt.Errorf("OFFICECLI_REQUIRED")
	}
	if err := e.OfficeCLI.Ensure(ctx); err != nil {
		return err
	}
	if len(e.ProfileEnvironment) > 0 {
		if e.ProfileSecrets == nil {
			return fmt.Errorf("HERMES_PROFILE_SECRET_STORE_REQUIRED")
		}
		if _, err := e.ProfileSecrets.Save(e.ProfileEnvironment); err != nil {
			return err
		}
	}
	if err := e.configureCertificates(desired); err != nil {
		return err
	}
	profile, err := hermes.ProfileFromDesired(desired)
	if err != nil {
		return err
	}
	profile, err = profile.WithOfficeCLI(e.OfficeCLI.Path())
	if err != nil {
		return err
	}
	data, err := profile.RenderForSchema(hermes.PublicProviderProvider(), e.RuntimeContract.ConfigSchema)
	if err != nil {
		return err
	}
	if err := workspace.WriteFileAtomic(profilePath(desired), data, 0o600); err != nil {
		return err
	}
	return writeRoleSoul(desired)
}

func (e *Effects) ensureHermesCompatibility(ctx context.Context, desired domain.DesiredState) error {
	if err := e.ensureHermes(ctx, desired); err != nil {
		return err
	}
	if err := e.ensureRuntimeContract(ctx, desired); err != nil {
		return err
	}
	root := profileDirectory(desired)
	identity := profileIdentity(desired)
	verify := func(gotRoot, gotIdentity string) error {
		if gotRoot != root || gotIdentity != identity {
			return ErrForeignProfile
		}
		owned, err := profileOwnerMatches(desired)
		if err != nil || !owned {
			return ErrForeignProfile
		}
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || unsafeProfileComponent(info) {
			return ErrForeignProfile
		}
		return nil
	}
	if err := verify(root, identity); err != nil {
		return err
	}
	legacyMarker, err := hermes.ExactLegacyOptOutMarker(root)
	if err != nil {
		return err
	}
	if !legacyMarker {
		return nil
	}
	pin, err := catalog.LookupToolchain(desired.Toolchain())
	if err != nil {
		return err
	}
	if _, err := hermes.VerifiedToolchainLock(root, pin); err != nil {
		return fmt.Errorf("%w: selected toolchain lock", hermes.ErrBundledSkillsMigrationFailed)
	}
	if err := privatefile.NormalizeOwnerOnly(filepath.Join(root, ".env")); err != nil {
		return err
	}
	if err := hermes.MigrateOwnedBundledSkills(ctx, root, identity, verify, e.Profile); err != nil {
		return err
	}
	installed, err := hermes.ToolchainInstalled(root, pin)
	if err != nil || !installed {
		return fmt.Errorf("%w: selected toolchain changed during opt-in", hermes.ErrBundledSkillsMigrationFailed)
	}
	return nil
}

func (e *Effects) ensureRuntimeContract(_ context.Context, _ domain.DesiredState) error {
	if e.RuntimeContract.Info.Executable != "" && e.RuntimeContract.ConfigSchema > 0 {
		return nil
	}
	return hermes.ErrConfigSchemaUnsupported
}

func (e *Effects) verifyHermesManagedState(ctx context.Context, desired domain.DesiredState) error {
	if err := e.ensureRuntimeContract(ctx, desired); err != nil {
		return err
	}
	profile, err := hermes.ProfileFromDesired(desired)
	if err != nil {
		return err
	}
	if e.OfficeCLI == nil {
		return fmt.Errorf("OFFICECLI_REQUIRED")
	}
	profile, err = profile.WithOfficeCLI(e.OfficeCLI.Path())
	if err != nil {
		return err
	}
	configData, err := profile.RenderForSchema(hermes.PublicProviderProvider(), e.RuntimeContract.ConfigSchema)
	if err != nil {
		return err
	}
	pin, err := catalog.LookupToolchain(desired.Toolchain())
	if err != nil {
		return err
	}
	identity := profileIdentity(desired)
	return hermes.VerifyManagedProfile(ctx, hermes.ManagedProfileExpectation{
		Runtime:      e.RuntimeContract,
		RuntimeProbe: e.RuntimeProbe,
		Owner: hermes.ProfileOwnerExpectation{
			Identity: identity, ProfileRoot: profileDirectory(desired), OwnerPath: profileOwnerPath(desired), OwnerRecord: []byte(identity + "\n"),
		},
		Config: configData, Environment: filepath.Join(profileDirectory(desired), ".env"), ToolchainPin: pin,
	})
}

func (e *Effects) ensureHermes(ctx context.Context, desired domain.DesiredState) error {
	if err := validateHermesProfilePath(desired); err != nil {
		return err
	}
	if e.HermesEnvironment != nil {
		if err := e.HermesEnvironment(desired.HermesHome()); err != nil {
			return err
		}
	}
	if desired.AppInstalled() {
		ready, err := hermes.ExecutableReady(e.HermesExecutable)
		if err != nil {
			return err
		}
		if !ready {
			return hermes.ErrExecutableUnverified
		}
	} else {
		ready, err := e.managedHermesInstallReady(desired)
		if err != nil {
			return err
		}
		if !ready {
			if e.Installer == nil || strings.TrimSpace(e.InstallerPath) == "" {
				return fmt.Errorf("HERMES_INSTALLER_REQUIRED")
			}
			if err := e.Installer.Apply(ctx, e.InstallerPath); err != nil {
				return err
			}
			ready, err = e.managedHermesInstallReady(desired)
			if err != nil {
				return err
			}
			if !ready {
				return hermes.ErrInstallLayout
			}
		}
		if !regularFileExists(installedMarker(desired)) {
			if err := workspace.WriteFileAtomic(installedMarker(desired), []byte("installed-by-teamkit\n"), 0o600); err != nil {
				return err
			}
		}
		executable := standardHermesExecutable(desired)
		contract, err := hermes.VerifyRuntimeContract(ctx, executable, nil)
		if err != nil {
			return err
		}
		e.HermesExecutable = executable
		e.RuntimeContract = contract
	}
	owned, err := profileOwnerMatches(desired)
	if err != nil {
		return err
	}
	profileDir := profileDirectory(desired)
	profileInfo, profileErr := os.Lstat(profileDir)
	if profileErr != nil && !errors.Is(profileErr, fs.ErrNotExist) {
		return profileErr
	}
	if profileErr == nil && (!profileInfo.IsDir() || unsafeProfileComponent(profileInfo)) {
		return ErrForeignProfile
	}
	claim, creating, err := readProfileClaim(desired)
	if err != nil {
		return err
	}
	if profileErr == nil && !owned {
		proof, proofErr := profileAdoptionProofMatches(profileDir, claim)
		if proofErr != nil || !creating || !proof {
			return ErrForeignProfile
		}
		return completeProfileAdoption(desired, profileDir)
	}
	if owned && profileErr == nil {
		_ = removeProfileAdoptionProof(profileDir)
		return removeProfileCreating(desired)
	}
	if e.Profile == nil {
		return fmt.Errorf("HERMES_PROFILE_REQUIRED")
	}
	if creating {
		staging := filepath.Join(desired.HermesHome(), "profiles", claim.StagingIdentity)
		stagingInfo, stagingErr := os.Lstat(staging)
		if stagingErr == nil && stagingInfo.IsDir() && !unsafeProfileComponent(stagingInfo) {
			proof, proofErr := profileAdoptionProofMatches(staging, claim)
			if proofErr == nil && proof {
				if err := normalizeStagingProfileEnvironment(staging, claim); err != nil {
					return err
				}
				if renameErr := publishStagedProfile(staging, profileDir); renameErr != nil {
					return renameErr
				}
				return completeProfileAdoption(desired, profileDir)
			}
		}
		return ErrProfileInProgress
	}
	claim, err = createProfileClaim(desired)
	if err != nil {
		return err
	}
	staging := filepath.Join(desired.HermesHome(), "profiles", claim.StagingIdentity)
	if err := e.Profile.Create(ctx, claim.StagingIdentity); err != nil {
		if cleanupErr := cleanupStagedProfile(staging); cleanupErr != nil {
			return fmt.Errorf("%w; HERMES_PROFILE_CLEANUP_FAILED: %v", err, cleanupErr)
		}
		if cleanupErr := removeProfileCreating(desired); cleanupErr != nil {
			return fmt.Errorf("%w; HERMES_PROFILE_CLAIM_CLEANUP_FAILED: %v", err, cleanupErr)
		}
		return err
	}
	if err := validateHermesProfilePath(desired); err != nil {
		return err
	}
	info, err := os.Lstat(staging)
	if err != nil || !info.IsDir() || unsafeProfileComponent(info) {
		return fmt.Errorf("HERMES_PROFILE_CREATE_FAILED")
	}
	if err := writeProfileAdoptionProof(staging, claim); err != nil {
		return err
	}
	if err := normalizeStagingProfileEnvironment(staging, claim); err != nil {
		return err
	}
	if err := publishStagedProfile(staging, profileDir); err != nil {
		return err
	}
	return completeProfileAdoption(desired, profileDir)
}

func standardHermesExecutable(desired domain.DesiredState) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(desired.HermesHome(), "hermes-agent", "venv", "Scripts", "hermes.exe")
	}
	return filepath.Join(desired.HermesHome(), "hermes-agent", "venv", "bin", "hermes")
}

func normalizeStagingProfileEnvironment(staging string, claim profileClaim) error {
	if filepath.Base(filepath.Clean(staging)) != claim.StagingIdentity || claim.Identity == "" || claim.Nonce == "" {
		return ErrForeignProfile
	}
	info, err := os.Lstat(staging)
	if err != nil || !info.IsDir() || unsafeProfileComponent(info) {
		return ErrForeignProfile
	}
	final := filepath.Join(filepath.Dir(staging), claim.Identity)
	if _, err := os.Lstat(final); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return ErrForeignProfile
	}
	proof, err := profileAdoptionProofMatches(staging, claim)
	if err != nil || !proof {
		return ErrForeignProfile
	}
	if err := privatefile.NormalizeOwnerOnly(filepath.Join(staging, ".env")); err != nil {
		return err
	}
	proof, err = profileAdoptionProofMatches(staging, claim)
	if err != nil || !proof {
		return ErrForeignProfile
	}
	return nil
}

func cleanupStagedProfile(staging string) error {
	if err := pathsafe.ValidateDirectory(staging); err != nil {
		return err
	}
	info, err := os.Lstat(staging)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || unsafeProfileComponent(info) {
		return ErrForeignProfile
	}
	return os.RemoveAll(staging)
}

func (e *Effects) configureCertificates(desired domain.DesiredState) error {
	if err := validateHermesProfilePath(desired); err != nil {
		return err
	}
	bundle := filepath.Join(desired.HermesHome(), "certs", "ca-bundle.pem")
	expected := e.CertificateSHA256
	if expected == "" {
		expected = DefaultCertificateSHA256
	}
	if e.CertificateArchive != "" {
		file, err := os.Open(e.CertificateArchive)
		if err != nil {
			return err
		}
		defer file.Close()
		hash := sha256.New()
		size, err := io.Copy(hash, file)
		if err != nil {
			return err
		}
		if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected) {
			return ErrCertificateChecksum
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		bundle, err = hermes.ExtractCertificates(file, size, desired.HermesHome())
		if err != nil {
			return err
		}
	} else {
		verifiedBundle, ready, err := hermes.ManagedCertificateBundle(desired.HermesHome(), expected)
		if err != nil {
			return err
		}
		if !ready {
			return ErrCertificateRequired
		}
		bundle = verifiedBundle
	}
	verifiedBundle, ready, err := hermes.ManagedCertificateBundle(desired.HermesHome(), expected)
	if err != nil {
		return err
	}
	if !ready || filepath.Clean(verifiedBundle) != filepath.Clean(bundle) {
		return ErrCertificateRequired
	}
	if e.Secrets == nil {
		return fmt.Errorf("SECRET_STORE_REQUIRED")
	}
	if e.ProfileSecrets == nil {
		return fmt.Errorf("HERMES_PROFILE_SECRET_STORE_REQUIRED")
	}
	_, err = e.Secrets.Save(hermes.ApplicationCAEnvironment(bundle))
	if err != nil {
		return err
	}
	_, err = e.ProfileSecrets.Save(hermes.ApplicationCAEnvironment(bundle))
	return err
}

func (e *Effects) certificateStateReady(desired domain.DesiredState) (bool, error) {
	expected := e.CertificateSHA256
	if expected == "" {
		expected = DefaultCertificateSHA256
	}
	bundle, ready, err := hermes.ManagedCertificateBundle(desired.HermesHome(), expected)
	if err != nil || !ready {
		return false, err
	}
	globalReady, err := hermes.CertificateEnvironmentReady(filepath.Join(desired.HermesHome(), ".env"), bundle)
	if err != nil || !globalReady {
		return false, err
	}
	profileReady, err := hermes.CertificateEnvironmentReady(filepath.Join(profileDirectory(desired), ".env"), bundle)
	if err != nil || !profileReady {
		return false, err
	}
	return true, nil
}

func (e *Effects) finalize(desired domain.DesiredState) error {
	if err := workspace.EnsureOwner(desired.KitHome(), string(desired.Project())); err != nil {
		return err
	}
	if err := workspace.WritePublicEnv(filepath.Join(desired.KitHome(), ".env"), config.Encode(desired)); err != nil {
		return err
	}
	infoDirectory := filepath.Join(desired.KitHome(), ".git", "info")
	if err := pathsafe.EnsureDirectory(infoDirectory, 0o700); err != nil {
		return fmt.Errorf("CONTENT_GIT_REQUIRED: %w", err)
	}
	if err := workspace.EnsureLocalExclude(filepath.Join(infoDirectory, "exclude")); err != nil {
		return err
	}
	rootIgnore := filepath.Join(desired.KitHome(), ".gitignore")
	if err := workspace.EnsureGitignore(rootIgnore); err != nil {
		return err
	}
	hooks := filepath.Join(desired.KitHome(), "db", ".git", "hooks")
	if !exists(filepath.Dir(hooks)) {
		return fmt.Errorf("DATABASE_GIT_REQUIRED")
	}
	installer := e.InstallHooks
	if installer == nil {
		installer = gitx.InstallHooks
	}
	return installer(hooks)
}

func mustToolchain(desired domain.DesiredState) apps.Toolchain {
	pinned, _ := catalog.LookupToolchain(desired.Toolchain())
	return apps.Toolchain{Name: string(pinned.ID), Origin: pinned.Origin, Version: pinned.Commit}
}

func handoffReady(desired domain.DesiredState) (bool, error) {
	if !desired.AppInstalled() {
		return false, nil
	}
	path := filepath.Join(desired.KitHome(), ".teamkit", "handoff.txt")
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrForeignWorkspace
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	handoff, err := apps.PrepareHandoff(apps.Application{ID: string(desired.Application()), Installed: true}, apps.HandoffRequest{Toolchain: mustToolchain(desired), V8StdEndpoint: catalog.V8StdMCP().Endpoint})
	if err != nil {
		return false, err
	}
	return string(data) == handoff.Command+"\n", nil
}
func profileDirectory(desired domain.DesiredState) string {
	return filepath.Join(desired.HermesHome(), "profiles", profileIdentity(desired))
}
func profilePath(desired domain.DesiredState) string {
	return filepath.Join(profileDirectory(desired), "config.yaml")
}
func profileSoulPath(desired domain.DesiredState) string {
	return filepath.Join(profileDirectory(desired), "SOUL.md")
}
func writeRoleSoul(desired domain.DesiredState) error {
	soul, err := hermes.SoulForRole(desired.Role())
	if err != nil {
		return err
	}
	return workspace.WriteFileAtomic(profileSoulPath(desired), soul, 0o600)
}
func profileConfigReady(desired domain.DesiredState, schema int, officeCLIPath string) (bool, error) {
	profile, err := hermes.ProfileFromDesired(desired)
	if err != nil {
		return false, err
	}
	profile, err = profile.WithOfficeCLI(officeCLIPath)
	if err != nil {
		return false, err
	}
	expected, err := profile.RenderForSchema(hermes.PublicProviderProvider(), schema)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(profilePath(desired))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(data, expected), nil
}
func toolchainPath(desired domain.DesiredState) string {
	return filepath.Join(profileDirectory(desired), ".teamkit", "toolchain-source")
}
func profileIdentity(desired domain.DesiredState) string {
	return "1c-" + string(desired.Project()) + "-" + string(desired.Role()) + "-" + string(desired.Toolchain())
}
func profileOwnerPath(desired domain.DesiredState) string {
	return filepath.Join(desired.HermesHome(), ".teamkit", "profiles", profileIdentity(desired)+".owner")
}
func profileCreatingPath(desired domain.DesiredState) string {
	return filepath.Join(desired.HermesHome(), ".teamkit", "profiles", profileIdentity(desired)+".creating")
}

type profileClaim struct {
	Identity        string `json:"identity"`
	StagingIdentity string `json:"staging_identity"`
	Nonce           string `json:"nonce"`
}

func createProfileClaim(desired domain.DesiredState) (profileClaim, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return profileClaim{}, err
	}
	encoded := hex.EncodeToString(nonce)
	claim := profileClaim{
		Identity: profileIdentity(desired), StagingIdentity: "teamkit-" + encoded,
		Nonce: encoded,
	}
	data, err := json.Marshal(claim)
	if err != nil {
		return profileClaim{}, err
	}
	path := profileCreatingPath(desired)
	directory := filepath.Dir(path)
	if err := pathsafe.EnsureDirectory(directory, 0o700); err != nil {
		return profileClaim{}, err
	}
	if err := pathsafe.ValidateRegular(path); err != nil {
		return profileClaim{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return profileClaim{}, ErrProfileInProgress
	}
	if err != nil {
		return profileClaim{}, err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return profileClaim{}, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return profileClaim{}, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return profileClaim{}, err
	}
	return claim, nil
}

func readProfileClaim(desired domain.DesiredState) (profileClaim, bool, error) {
	path := profileCreatingPath(desired)
	if err := pathsafe.ValidateRegular(path); err != nil {
		return profileClaim{}, false, ErrForeignProfile
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return profileClaim{}, false, nil
	}
	if err != nil {
		return profileClaim{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 4096 {
		return profileClaim{}, false, ErrForeignProfile
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return profileClaim{}, false, err
	}
	var claim profileClaim
	if err := json.Unmarshal(data, &claim); err != nil {
		return profileClaim{}, false, ErrForeignProfile
	}
	nonce, err := hex.DecodeString(claim.Nonce)
	if err != nil || len(nonce) != 16 || claim.Identity != profileIdentity(desired) || claim.StagingIdentity != "teamkit-"+claim.Nonce {
		return profileClaim{}, false, ErrForeignProfile
	}
	return claim, true, nil
}

func profileCreatingMatches(desired domain.DesiredState) (bool, error) {
	_, exists, err := readProfileClaim(desired)
	return exists, err
}
func removeProfileCreating(desired domain.DesiredState) error {
	if err := pathsafe.ValidateRegular(profileCreatingPath(desired)); err != nil {
		return err
	}
	err := os.Remove(profileCreatingPath(desired))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func profileAdoptionProofPath(profile string) string {
	return filepath.Join(profile, ".teamkit-adoption.json")
}

func writeProfileAdoptionProof(profile string, claim profileClaim) error {
	if err := pathsafe.ValidateDirectory(profile); err != nil {
		return ErrForeignProfile
	}
	data, err := json.Marshal(claim)
	if err != nil {
		return err
	}
	return workspace.WriteFileAtomic(profileAdoptionProofPath(profile), data, 0o600)
}

func profileAdoptionProofMatches(profile string, claim profileClaim) (bool, error) {
	if claim.Identity == "" || claim.StagingIdentity == "" || claim.Nonce == "" {
		return false, nil
	}
	path := profileAdoptionProofPath(profile)
	if err := pathsafe.ValidateRegular(path); err != nil {
		return false, ErrForeignProfile
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 4096 {
		return false, ErrForeignProfile
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var proof profileClaim
	if err := json.Unmarshal(data, &proof); err != nil {
		return false, ErrForeignProfile
	}
	return proof == claim, nil
}

func publishStagedProfile(staging, final string) error {
	if err := pathsafe.ValidateDirectory(staging); err != nil {
		return ErrForeignProfile
	}
	if err := pathsafe.ValidateDirectory(final); err != nil {
		return ErrForeignProfile
	}
	info, err := os.Lstat(staging)
	if err != nil || !info.IsDir() || unsafeProfileComponent(info) {
		return ErrForeignProfile
	}
	if _, err := os.Lstat(final); err == nil {
		return ErrForeignProfile
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, final); err != nil {
		if _, statErr := os.Lstat(final); statErr == nil {
			return ErrForeignProfile
		}
		return err
	}
	return nil
}

func completeProfileAdoption(desired domain.DesiredState, profile string) error {
	claim, exists, err := readProfileClaim(desired)
	if err != nil || !exists {
		return ErrForeignProfile
	}
	proof, err := profileAdoptionProofMatches(profile, claim)
	if err != nil || !proof {
		return ErrForeignProfile
	}
	if err := workspace.WriteFileAtomic(profileOwnerPath(desired), []byte(profileIdentity(desired)+"\n"), 0o600); err != nil {
		return err
	}
	if err := removeProfileAdoptionProof(profile); err != nil {
		return err
	}
	return removeProfileCreating(desired)
}

func removeProfileAdoptionProof(profile string) error {
	path := profileAdoptionProofPath(profile)
	if err := pathsafe.ValidateRegular(path); err != nil {
		return err
	}
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
func profileOwnerMatches(desired domain.DesiredState) (bool, error) {
	path := profileOwnerPath(desired)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrForeignProfile
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(data)) != profileIdentity(desired) {
		return false, ErrForeignProfile
	}
	return true, nil
}
func installedMarker(desired domain.DesiredState) string {
	return filepath.Join(desired.HermesHome(), ".teamkit", "hermes-installed")
}

func (e *Effects) managedHermesInstallReady(desired domain.DesiredState) (bool, error) {
	check := e.ManagedInstallReady
	if check == nil {
		check = hermes.ManagedInstallReady
	}
	ready, err := check(desired.HermesHome())
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrForeignProfile, err)
	}
	return ready, nil
}
func contentMarkerPath(root string) string {
	return filepath.Join(root, ".teamkit", "content.ready")
}

func databaseMarkerPath(root string) string {
	return filepath.Join(root, ".teamkit", "database.ready")
}

func databaseMarkerReady(root string) (bool, error) {
	path := databaseMarkerPath(root)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrForeignWorkspace
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) == "develop", nil
}
func contentMarkerReady(root, branch string) (bool, error) {
	path := contentMarkerPath(root)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrForeignWorkspace
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) == branch, nil
}
func projectContentBranch(desired domain.DesiredState) string {
	project, _ := catalog.LookupProject(desired.Project())
	return project.ContentBranch
}

func validateHomes(desired domain.DesiredState) error {
	if !filepath.IsAbs(desired.KitHome()) {
		return fmt.Errorf("KIT_HOME_INVALID")
	}
	if desired.Application() == domain.AppHermes && !filepath.IsAbs(desired.HermesHome()) {
		return fmt.Errorf("HERMES_HOME_INVALID")
	}
	return nil
}

func validateWorkspacePaths(desired domain.DesiredState) error {
	root := desired.KitHome()
	for _, directory := range []string{
		root,
		filepath.Join(root, ".teamkit"),
		filepath.Join(root, ".git"),
		filepath.Join(root, "db"),
		filepath.Join(root, "db", ".git"),
	} {
		if err := pathsafe.ValidateDirectory(directory); err != nil {
			return fmt.Errorf("%w: %v", ErrForeignWorkspace, err)
		}
	}
	for _, file := range []string{
		filepath.Join(root, ".env"),
		filepath.Join(root, ".gitignore"),
		filepath.Join(root, ".teamkit", "owner"),
		contentMarkerPath(root),
		databaseMarkerPath(root),
		filepath.Join(root, ".teamkit", "handoff.txt"),
		filepath.Join(root, ".teamkit", "operation.json"),
	} {
		if err := pathsafe.ValidateRegular(file); err != nil {
			return fmt.Errorf("%w: %v", ErrForeignWorkspace, err)
		}
	}
	return nil
}

func safeRoot(desired domain.DesiredState) error {
	root := desired.KitHome()
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrForeignWorkspace
	}
	owner, err := ownerMatches(root, desired.Project())
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) > 0 && !owner {
		return ErrForeignWorkspace
	}
	return nil
}
func rootEntries(root string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}
func ownerMatches(root string, project domain.ProjectID) (bool, error) {
	metadata := filepath.Join(root, ".teamkit")
	metadataInfo, err := os.Lstat(metadata)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil || !metadataInfo.IsDir() || metadataInfo.Mode()&os.ModeSymlink != 0 {
		return false, ErrForeignWorkspace
	}
	owner := filepath.Join(metadata, "owner")
	info, err := os.Lstat(owner)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrForeignWorkspace
	}
	data, err := os.ReadFile(owner)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(data)) != string(project) {
		return false, ErrForeignWorkspace
	}
	return true, nil
}
func safeMarker(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, ErrForeignWorkspace
	}
	return true, nil
}
func safeGitDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, ErrForeignWorkspace
	}
	return true, nil
}
func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
func exists(path string) bool { _, err := os.Stat(path); return err == nil }
