package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mi1man-cmd/kit-all-team/internal/bootstrap"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

var (
	errOfficeCLIAssetChecksum        = errors.New("OFFICECLI_ASSET_CHECKSUM_MISMATCH")
	errOfficeCLIAssetTooLarge        = errors.New("OFFICECLI_ASSET_TOO_LARGE")
	errOfficeCLIAutoUpdateConfig     = errors.New("OFFICECLI_AUTOUPDATE_CONFIG_FAILED")
	errOfficeCLIUpdateArtifactRemove = errors.New("OFFICECLI_UPDATE_ARTIFACT_REMOVE_FAILED")
)

var officeCLIUserHomeResolver = officeCLIUserHome

const officeCLIChildTimeout = 10 * time.Second

func resolveOfficeCLIAsset(family domain.OSFamily, macArchitecture string) (catalog.OfficeCLIAsset, error) {
	switch family {
	case domain.OSWindows, domain.OSLinux, domain.OSALTLinux:
		return catalog.LookupOfficeCLIAsset(family, "amd64")
	case domain.OSMacOS:
		if macArchitecture != "amd64" && macArchitecture != "arm64" {
			return catalog.OfficeCLIAsset{}, catalog.ErrOfficeCLIPlatformUnsupported
		}
		return catalog.LookupOfficeCLIAsset(family, macArchitecture)
	default:
		return catalog.OfficeCLIAsset{}, catalog.ErrOfficeCLIPlatformUnsupported
	}
}

// officeCLIProvisioner materializes one catalog-pinned OfficeCLI executable
// and enforces its user-global no-auto-update policy.
type officeCLIProvisioner struct {
	asset      catalog.OfficeCLIAsset
	path       string
	configPath string
	download   DownloadPort
	verify     func([]byte, string) bool
	write      func(string, []byte) error
	run        platform.ProcessRunner
	capture    func(context.Context, string, []string) (stdout, stderr []byte, err error)
	readConfig func(string) ([]byte, error)
	remove     func(string) error
	userHome   func() (string, error)
	hermesHome string
}

func (s *Service) officeCLIProvisioner(asset catalog.OfficeCLIAsset, hermesHome string) (*officeCLIProvisioner, error) {
	managedPath, err := officeCLIManagedPath(hermesHome, asset.Version)
	if err != nil {
		return nil, err
	}
	home, err := officeCLIUserHomeResolver()
	if err != nil {
		return nil, err
	}
	return &officeCLIProvisioner{
		asset: asset, path: managedPath, configPath: filepath.Join(home, ".officecli", "config.json"), hermesHome: hermesHome,
		download: s.downloader(maxOfficeCLIBytes, "OFFICECLI_ASSET_TOO_LARGE"), verify: s.digestVerifier(),
		write: s.officeCLIWriter(), run: s.processRunner(), capture: officeCLICapture,
		readConfig: func(path string) ([]byte, error) { return pathsafe.ReadRegular(path, maxHermesCommandOutput) }, remove: os.Remove,
		userHome: officeCLIUserHomeResolver,
	}, nil
}

// Path returns the exact verified executable path, never an upstream filename.
func (p *officeCLIProvisioner) Path() string { return p.path }

// Ensure repairs only a missing or invalid regular asset, then persists and
// proves OfficeCLI's no-auto-update setting before removing known update drift.
func (p *officeCLIProvisioner) Ensure(ctx context.Context) error {
	valid, err := p.binaryReady()
	if err != nil {
		return err
	}
	if _, _, err := p.profileState(); err != nil {
		return err
	}
	if _, err := p.updateSiblings(); err != nil {
		return err
	}
	if !valid {
		data, err := p.downloadAsset(ctx)
		if err != nil {
			return err
		}
		writer := p.write
		if writer == nil {
			writer = func(path string, data []byte) error { return workspace.WriteFileAtomic(path, data, 0o700) }
		}
		if err := writer(p.path, data); err != nil {
			return err
		}
	}
	if valid, err = p.binaryReady(); err != nil {
		return err
	} else if !valid {
		return errOfficeCLIAssetChecksum
	}
	if _, _, err := p.profileState(); err != nil {
		return err
	}
	if err := p.setAutoUpdate(ctx); err != nil {
		return err
	}
	if valid, err = p.binaryReady(); err != nil {
		return err
	} else if !valid {
		return errOfficeCLIAssetChecksum
	}
	if _, _, err := p.profileState(); err != nil {
		return err
	}
	if err := p.readAutoUpdate(ctx); err != nil {
		return err
	}
	if err := p.removeUpdateSiblings(); err != nil {
		return err
	}
	ready, err := p.Ready(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return errOfficeCLIAutoUpdateConfig
	}
	return nil
}

// Ready validates only persisted data and never starts OfficeCLI.
func (p *officeCLIProvisioner) Ready(context.Context) (bool, error) {
	valid, err := p.binaryReady()
	if err != nil || !valid {
		return false, err
	}
	updates, err := p.updateSiblings()
	if err != nil || updates {
		return false, err
	}
	state, exists, err := p.profileState()
	if err != nil || !exists {
		return false, err
	}
	return !state.AutoUpdate, nil
}

func (p *officeCLIProvisioner) downloadAsset(ctx context.Context) ([]byte, error) {
	if p.asset.Size > maxOfficeCLIBytes {
		return nil, errOfficeCLIAssetTooLarge
	}
	if p.download == nil || p.asset.URL == "" || p.asset.Size <= 0 || p.verify == nil {
		return nil, errOfficeCLIAssetChecksum
	}
	data, err := p.download.Download(ctx, p.asset.URL)
	if err != nil {
		return nil, fmt.Errorf("OFFICECLI_ASSET_DOWNLOAD_FAILED: %w", err)
	}
	if int64(len(data)) > maxOfficeCLIBytes {
		return nil, errOfficeCLIAssetTooLarge
	}
	if len(data) == 0 || int64(len(data)) != p.asset.Size || !p.verify(data, p.asset.SHA256) {
		return nil, errOfficeCLIAssetChecksum
	}
	return data, nil
}

func (p *officeCLIProvisioner) binaryReady() (bool, error) {
	if _, err := p.managedBinaryPath(); err != nil {
		return false, err
	}
	if err := pathsafe.ValidateRegular(p.path); err != nil {
		return false, foreignOfficeCLIPath(err)
	}
	info, err := os.Lstat(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, foreignOfficeCLIPath(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, foreignOfficeCLIPath(fmt.Errorf("managed binary is not regular"))
	}
	data, err := pathsafe.ReadRegular(p.path, maxOfficeCLIBytes)
	if errors.Is(err, pathsafe.ErrTooLarge) {
		return false, nil
	}
	if err != nil {
		return false, foreignOfficeCLIPath(err)
	}
	if len(data) == 0 || int64(len(data)) != p.asset.Size || p.verify == nil || !p.verify(data, p.asset.SHA256) {
		return false, nil
	}
	if !officeCLIBinaryModeReady(runtime.GOOS, info.Mode()) {
		return false, nil
	}
	return true, nil
}

func officeCLIBinaryModeReady(goos string, mode os.FileMode) bool {
	return goos == "windows" || mode.Perm() == 0o700
}

func (p *officeCLIProvisioner) profileState() (officeCLIConfig, bool, error) {
	configPath, err := p.expectedConfigPath()
	if err != nil {
		return officeCLIConfig{}, false, err
	}
	directory := filepath.Dir(configPath)
	if err := pathsafe.ValidateDirectory(directory); err != nil {
		return officeCLIConfig{}, false, foreignOfficeCLIPath(err)
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return officeCLIConfig{}, false, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return officeCLIConfig{}, false, foreignOfficeCLIPath(err)
	}
	if err := pathsafe.ValidateRegular(configPath); err != nil {
		return officeCLIConfig{}, false, foreignOfficeCLIPath(err)
	}
	info, err = os.Lstat(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return officeCLIConfig{}, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return officeCLIConfig{}, false, foreignOfficeCLIPath(err)
	}
	reader := p.readConfig
	if reader == nil {
		reader = func(path string) ([]byte, error) { return pathsafe.ReadRegular(path, maxHermesCommandOutput) }
	}
	data, err := reader(configPath)
	if errors.Is(err, pathsafe.ErrTooLarge) {
		return officeCLIConfig{}, false, nil
	}
	if err != nil {
		return officeCLIConfig{}, false, foreignOfficeCLIPath(err)
	}
	state, err := officeCLIConfigState(data)
	if err != nil {
		return officeCLIConfig{}, false, nil
	}
	if state.Log {
		logPath := filepath.Join(directory, "officecli.log")
		if err := pathsafe.ValidateRegular(logPath); err != nil {
			return officeCLIConfig{}, false, foreignOfficeCLIPath(err)
		}
		info, err := os.Lstat(logPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return officeCLIConfig{}, false, foreignOfficeCLIPath(err)
		}
		if err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return officeCLIConfig{}, false, foreignOfficeCLIPath(fmt.Errorf("OfficeCLI log is not regular"))
		}
	}
	return state, true, nil
}

func (p *officeCLIProvisioner) expectedConfigPath() (string, error) {
	homeResolver := p.userHome
	if homeResolver == nil {
		homeResolver = officeCLIUserHome
	}
	home, err := homeResolver()
	if err != nil {
		return "", foreignOfficeCLIPath(err)
	}
	canonicalHome, err := pathsafe.CanonicalPath(home)
	if err != nil {
		return "", foreignOfficeCLIPath(err)
	}
	expected := filepath.Join(canonicalHome, ".officecli", "config.json")
	matches, err := officeCLIConfigPathMatches(p.configPath, expected)
	if err != nil {
		return "", foreignOfficeCLIPath(err)
	}
	if !matches {
		return "", foreignOfficeCLIPath(fmt.Errorf("config path is outside OfficeCLI profile"))
	}
	return expected, nil
}

func officeCLIConfigPathMatches(actual, expected string) (bool, error) {
	if !filepath.IsAbs(actual) || !filepath.IsAbs(expected) || !officeCLIPathComponentEqual(filepath.Base(actual), filepath.Base(expected)) {
		return false, nil
	}
	actualParent, err := pathsafe.ComparisonKey(filepath.Dir(actual))
	if err != nil {
		return false, err
	}
	expectedParent, err := pathsafe.ComparisonKey(filepath.Dir(expected))
	if err != nil {
		return false, err
	}
	return actualParent == expectedParent, nil
}

func (p *officeCLIProvisioner) setAutoUpdate(ctx context.Context) error {
	if p.run == nil {
		return errOfficeCLIAutoUpdateConfig
	}
	child, cancel := context.WithTimeout(ctx, officeCLIChildTimeout)
	defer cancel()
	if err := p.run.Run(child, p.path, []string{"config", "autoUpdate", "false"}); err != nil {
		return errOfficeCLIAutoUpdateConfig
	}
	return nil
}

func (p *officeCLIProvisioner) readAutoUpdate(ctx context.Context) error {
	capture := p.capture
	if capture == nil {
		capture = officeCLICapture
	}
	child, cancel := context.WithTimeout(ctx, officeCLIChildTimeout)
	defer cancel()
	stdout, stderr, err := capture(child, p.path, []string{"config", "autoUpdate"})
	if err != nil || len(stdout)+len(stderr) > maxHermesCommandOutput || len(stderr) != 0 || !bytes.Equal(bytes.Trim(stdout, "\r\n"), []byte("false")) {
		return errOfficeCLIAutoUpdateConfig
	}
	return nil
}

func (p *officeCLIProvisioner) updateSiblings() (bool, error) {
	if _, err := p.managedBinaryPath(); err != nil {
		return false, err
	}
	parent := filepath.Dir(p.path)
	if err := pathsafe.ValidateDirectory(parent); err != nil {
		return false, foreignOfficeCLIPath(err)
	}
	found := false
	for _, suffix := range []string{".update", ".update.partial", ".old"} {
		path := p.path + suffix
		if filepath.Dir(path) != parent {
			return false, foreignOfficeCLIPath(fmt.Errorf("update artifact escaped managed parent"))
		}
		if err := pathsafe.ValidateRegular(path); err != nil {
			return false, foreignOfficeCLIPath(err)
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return false, foreignOfficeCLIPath(err)
		}
		found = true
	}
	return found, nil
}

func (p *officeCLIProvisioner) removeUpdateSiblings() error {
	updates, err := p.updateSiblings()
	if err != nil || !updates {
		return err
	}
	for _, suffix := range []string{".update", ".update.partial", ".old"} {
		path := p.path + suffix
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return foreignOfficeCLIPath(err)
		}
		remove := p.remove
		if remove == nil {
			remove = os.Remove
		}
		if err := remove(path); err != nil {
			return fmt.Errorf("%w: %v", errOfficeCLIUpdateArtifactRemove, err)
		}
	}
	return nil
}

func officeCLIManagedPath(hermesHome, version string) (string, error) {
	if version == "" || version == "." || version == ".." || filepath.Base(version) != version || strings.ContainsAny(version, `/\\`) {
		return "", foreignOfficeCLIPath(fmt.Errorf("invalid OfficeCLI version"))
	}
	canonicalHome, err := pathsafe.CanonicalPath(hermesHome)
	if err != nil {
		return "", foreignOfficeCLIPath(err)
	}
	root := filepath.Join(canonicalHome, ".teamkit", "officecli")
	path := filepath.Join(root, version, officeCLIExecutableName())
	relative, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(relative) || relative != filepath.Join(version, officeCLIExecutableName()) {
		return "", foreignOfficeCLIPath(fmt.Errorf("managed OfficeCLI path escaped cache"))
	}
	return path, nil
}

func officeCLIExecutableName() string {
	if runtime.GOOS == "windows" {
		return "officecli.exe"
	}
	return "officecli"
}

func (p *officeCLIProvisioner) managedBinaryPath() (string, error) {
	expected, err := officeCLIManagedPath(p.hermesHome, p.asset.Version)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p.path) || filepath.Clean(p.path) != expected {
		return "", foreignOfficeCLIPath(fmt.Errorf("managed binary path is outside OfficeCLI cache"))
	}
	return expected, nil
}

type officeCLIConfig struct {
	AutoUpdate bool
	Log        bool
}

func officeCLIConfigState(data []byte) (officeCLIConfig, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return officeCLIConfig{}, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return officeCLIConfig{}, fmt.Errorf("OfficeCLI config is not an object")
	}
	seen := map[string]bool{}
	state := officeCLIConfig{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return officeCLIConfig{}, err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return officeCLIConfig{}, fmt.Errorf("OfficeCLI config contains duplicate key")
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return officeCLIConfig{}, err
		}
		switch key {
		case "lastUpdateCheck":
			if err := nullableDateTime(value); err != nil {
				return officeCLIConfig{}, err
			}
		case "latestVersion", "installedBinaryVersion", "lastSkillRefreshVersion":
			if err := nullableString(value); err != nil {
				return officeCLIConfig{}, err
			}
		case "autoUpdate":
			value, err := strictBoolean(value)
			if err != nil {
				return officeCLIConfig{}, err
			}
			state.AutoUpdate = value
		case "log":
			value, err := strictBoolean(value)
			if err != nil {
				return officeCLIConfig{}, err
			}
			state.Log = value
		default:
			return officeCLIConfig{}, fmt.Errorf("OfficeCLI config has unknown key")
		}
	}
	token, err = decoder.Token()
	if err != nil {
		return officeCLIConfig{}, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '}' {
		return officeCLIConfig{}, fmt.Errorf("OfficeCLI config object is incomplete")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return officeCLIConfig{}, fmt.Errorf("OfficeCLI config has trailing data")
	}
	for _, key := range []string{"lastUpdateCheck", "latestVersion", "autoUpdate", "log", "installedBinaryVersion", "lastSkillRefreshVersion"} {
		if !seen[key] {
			return officeCLIConfig{}, fmt.Errorf("OfficeCLI config misses required key")
		}
	}
	return state, nil
}

func nullableDateTime(value json.RawMessage) error {
	if bytes.Equal(value, []byte("null")) {
		return nil
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return err
	}
	_, err := time.Parse(time.RFC3339, text)
	return err
}

func nullableString(value json.RawMessage) error {
	if bytes.Equal(value, []byte("null")) {
		return nil
	}
	var text string
	return json.Unmarshal(value, &text)
}

func strictBoolean(value json.RawMessage) (bool, error) {
	if bytes.Equal(value, []byte("true")) {
		return true, nil
	}
	if bytes.Equal(value, []byte("false")) {
		return false, nil
	}
	return false, fmt.Errorf("OfficeCLI config value is not a boolean")
}

type officeCLIOutputLimit struct {
	mu        sync.Mutex
	remaining int
}

type officeCLIOutputBuffer struct {
	bytes.Buffer
	exceeded bool
	limit    *officeCLIOutputLimit
}

func (b *officeCLIOutputBuffer) Write(data []byte) (int, error) {
	if b.limit == nil {
		b.exceeded = true
		return 0, errOfficeCLIAutoUpdateConfig
	}
	b.limit.mu.Lock()
	defer b.limit.mu.Unlock()
	if len(data) > b.limit.remaining {
		b.exceeded = true
		return 0, errOfficeCLIAutoUpdateConfig
	}
	b.limit.remaining -= len(data)
	return b.Buffer.Write(data)
}

func officeCLICapture(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	limit := &officeCLIOutputLimit{remaining: maxHermesCommandOutput}
	stdout, stderr := officeCLIOutputBuffer{limit: limit}, officeCLIOutputBuffer{limit: limit}
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, nil, errOfficeCLIAutoUpdateConfig
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func foreignOfficeCLIPath(err error) error {
	if err == nil {
		err = fmt.Errorf("unsafe OfficeCLI path")
	}
	return fmt.Errorf("%w: %v", bootstrap.ErrForeignProfile, err)
}
