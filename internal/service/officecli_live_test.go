//go:build officecli_live

package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
)

const (
	officeCLILiveReadLimit             = 1 << 20
	officeCLILiveStageTimeout          = 10 * time.Second
	officeCLILiveProcessTimeout        = 30 * time.Second
	officeCLILiveDiagnosticRecordLimit = 4 << 10
	officeCLILiveDiagnosticFieldLimit  = 1 << 10
	officeCLILiveALTDiagnosticMode     = "stderr-stage-v1"
	officeCLILivePinnedALTImage        = "teamkit-alt-p11-officecli:ephemeral"
)

func TestOfficeCLILive_QualifiedAssetAndMCPHandshake(t *testing.T) {
	asset := officeCLILiveAsset(t)
	managedTemp, err := officeCLICanonicalTestRoot(t.TempDir(), filepath.EvalSymlinks)
	if err != nil {
		t.Fatalf("canonicalize managed OfficeCLI test root: %v", err)
	}
	home, profileBaseline, fixture := officeCLILiveProfile(t)
	hermesHome := filepath.Join(managedTemp, "hermes")
	if err := os.MkdirAll(hermesHome, 0o700); err != nil {
		t.Fatal(err)
	}

	service := New(Options{})
	provisioner, err := service.officeCLIProvisioner(asset, hermesHome)
	if err != nil {
		t.Fatalf("create production OfficeCLI provisioner: %v", err)
	}
	officeCLIConfigureALTDiagnostics(t, provisioner)
	assertOfficeCLIManagedExecRoot(t, provisioner.Path())
	if existing := os.Getenv("TEAMKIT_OFFICECLI_EXISTING_PATH"); existing != "" {
		officeCLIPreseedExistingAsset(t, service, provisioner, asset, existing)
	}
	if err := provisioner.Ensure(context.Background()); err != nil {
		t.Fatalf("production OfficeCLI provisioner: %v", err)
	}
	assertOfficeCLIAsset(t, service, provisioner.Path(), asset)
	assertOfficeCLIManagedMode(t, provisioner.Path(), 0o700)
	assertOfficeCLIConfig(t, filepath.Join(home, ".officecli", "config.json"))
	assertOfficeCLIUpdateSiblingsAbsent(t, provisioner.Path())

	if keep := os.Getenv("TEAMKIT_OFFICECLI_KEEP_PATH"); keep != "" {
		officeCLIKeepEvidence(t, service, provisioner.Path(), asset, keep)
	}

	if runtime.GOOS == "windows" {
		officeCLIMCPHandshake(t, provisioner.Path())
		first := officeCLIWindowsProfileManifest(t, home, fixture)
		assertOfficeCLIProfileDelta(t, profileBaseline, first, fixture)
		officeCLIMCPHandshake(t, provisioner.Path())
		second := officeCLIWindowsProfileManifest(t, home, fixture)
		assertOfficeCLIManifestEqual(t, first, second, "second sequential MCP start")
		assertOfficeCLIVersion(t, provisioner.Path(), asset.Version)
		final := officeCLIWindowsProfileManifest(t, home, fixture)
		assertOfficeCLIManifestEqual(t, second, final, "post-MCP version check")
	} else {
		assertOfficeCLIVersion(t, provisioner.Path(), asset.Version)
		officeCLIMCPHandshake(t, provisioner.Path())
		first := officeCLIProfileManifest(t, home, "")
		assertOfficeCLIProfileDelta(t, profileBaseline, first, "")
		officeCLIMCPHandshake(t, provisioner.Path())
		second := officeCLIProfileManifest(t, home, "")
		assertOfficeCLIManifestEqual(t, first, second, "second sequential MCP start")
	}

	assertOfficeCLIAsset(t, service, provisioner.Path(), asset)
	assertOfficeCLIUpdateSiblingsAbsent(t, provisioner.Path())
}

func officeCLIConfigureALTDiagnostics(t *testing.T, provisioner *officeCLIProvisioner) {
	t.Helper()
	enabled, err := officeCLILiveALTDiagnosticsEnabled(
		os.Getenv("TEAMKIT_OFFICECLI_ALT_DIAGNOSTICS"),
		os.Getenv("TEAMKIT_OFFICECLI_ALT_IMAGE"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		return
	}
	capture := provisioner.capture
	if capture == nil {
		capture = officeCLICapture
	}
	diagnostic := &officeCLILiveALTDiagnostic{
		capture: capture,
		emit:    func(record string) { fmt.Fprintln(os.Stderr, record) },
	}
	provisioner.run = platform.ProcessRunnerFunc(diagnostic.run)
	provisioner.capture = diagnostic.read
}

func officeCLILiveALTDiagnosticsEnabled(mode, image string) (bool, error) {
	if mode == "" {
		return false, nil
	}
	if mode != officeCLILiveALTDiagnosticMode {
		return false, fmt.Errorf("unsupported OfficeCLI ALT diagnostic mode %q", mode)
	}
	if image != officeCLILivePinnedALTImage {
		return false, fmt.Errorf("OfficeCLI ALT diagnostics require pinned image %q, got %q", officeCLILivePinnedALTImage, image)
	}
	return true, nil
}

type officeCLILiveALTDiagnostic struct {
	capture func(context.Context, string, []string) (stdout, stderr []byte, err error)
	emit    func(string)
}

func (d *officeCLILiveALTDiagnostic) run(ctx context.Context, name string, args []string) error {
	stdout, stderr, err := d.capture(ctx, name, args)
	d.record("config-set", stdout, stderr, err)
	return err
}

func (d *officeCLILiveALTDiagnostic) read(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
	stdout, stderr, err := d.capture(ctx, name, args)
	d.record("config-read-back", stdout, stderr, err)
	return stdout, stderr, err
}

func (d *officeCLILiveALTDiagnostic) record(stage string, stdout, stderr []byte, err error) {
	if d.emit == nil {
		return
	}
	status, diagnosticErr := "ok", ""
	if err != nil {
		status = "error"
		diagnosticErr = err.Error()
	}
	record := fmt.Sprintf(
		"OFFICECLI_ALT_DIAGNOSTIC stage=%s status=%s error=%q stdout=%q stderr=%q",
		stage,
		status,
		officeCLILiveDiagnosticASCII([]byte(diagnosticErr)),
		officeCLILiveDiagnosticASCII(stdout),
		officeCLILiveDiagnosticASCII(stderr),
	)
	if len(record) > officeCLILiveDiagnosticRecordLimit {
		const suffix = "...[truncated]"
		record = record[:officeCLILiveDiagnosticRecordLimit-len(suffix)] + suffix
	}
	d.emit(record)
}

func officeCLILiveDiagnosticASCII(data []byte) string {
	truncated := len(data) > officeCLILiveDiagnosticFieldLimit
	if truncated {
		data = data[:officeCLILiveDiagnosticFieldLimit]
	}
	var sanitized strings.Builder
	sanitized.Grow(len(data) + 14)
	for _, value := range data {
		switch {
		case value == '\n' || value == '\r' || value == '\t':
			sanitized.WriteByte(' ')
		case value >= 0x20 && value <= 0x7e:
			sanitized.WriteByte(value)
		default:
			sanitized.WriteByte('?')
		}
	}
	if truncated {
		sanitized.WriteString("...[truncated]")
	}
	return sanitized.String()
}

func officeCLICanonicalTestRoot(path string, evaluate func(string) (string, error)) (string, error) {
	canonical, err := evaluate(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(canonical) {
		return "", fmt.Errorf("canonical test root is not absolute: %q", canonical)
	}
	return filepath.Clean(canonical), nil
}

func assertOfficeCLIManagedExecRoot(t *testing.T, managedPath string) {
	t.Helper()
	execRoot := os.Getenv("TEAMKIT_OFFICECLI_EXEC_ROOT")
	if execRoot == "" {
		return
	}
	if !filepath.IsAbs(execRoot) {
		t.Fatalf("TEAMKIT_OFFICECLI_EXEC_ROOT must be absolute: %q", execRoot)
	}
	relative, err := filepath.Rel(filepath.Clean(execRoot), filepath.Clean(managedPath))
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("managed OfficeCLI path %q is outside executable root %q: %v", managedPath, execRoot, err)
	}
}

func officeCLILiveAsset(t *testing.T) catalog.OfficeCLIAsset {
	t.Helper()
	var family domain.OSFamily
	switch runtime.GOOS {
	case "windows":
		family = domain.OSWindows
	case "linux":
		family = domain.OSLinux
	case "darwin":
		family = domain.OSMacOS
	default:
		t.Fatalf("unsupported live-smoke OS %q", runtime.GOOS)
	}
	asset, err := resolveOfficeCLIAsset(family, runtime.GOARCH)
	if err != nil {
		t.Fatalf("select catalog OfficeCLI asset for %s/%s: %v", runtime.GOOS, runtime.GOARCH, err)
	}
	if asset.OS != family || asset.Architecture != runtime.GOARCH {
		t.Fatalf("selected asset is %s/%s, want %s/%s", asset.OS, asset.Architecture, family, runtime.GOARCH)
	}
	return asset
}

func officeCLILiveProfile(t *testing.T) (string, map[string]officeCLIManifestEntry, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("TEAMKIT_OFFICECLI_RUNNER_ENVIRONMENT") != "github-hosted" {
			t.Fatal("Windows OfficeCLI live smoke requires an ephemeral GitHub-hosted OS account")
		}
		home, err := officeCLIUserHome()
		if err != nil || !filepath.IsAbs(home) {
			t.Fatalf("resolve effective Windows Known Folder profile: %q, %v", home, err)
		}
		fixture := filepath.Join(home, ".agents", "skills", "officecli-pptx")
		if _, err := os.Lstat(fixture); err == nil || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("disposable OfficeCLI skill fixture path is not empty: %v", err)
		}
		if err := os.MkdirAll(fixture, 0o700); err != nil {
			t.Fatalf("preseed disposable OfficeCLI skill identity: %v", err)
		}
		if err := os.WriteFile(filepath.Join(fixture, "SKILL.md"), []byte("---\nname: officecli-pptx\n---\ndisposable refresh fixture\n"), 0o600); err != nil {
			t.Fatalf("preseed disposable OfficeCLI SKILL.md: %v", err)
		}
		t.Cleanup(func() { officeCLIRemoveDisposableFixture(t, home, fixture) })
		return home, officeCLIWindowsProfileManifest(t, home, fixture), fixture
	}

	root, err := officeCLICanonicalTestRoot(t.TempDir(), filepath.EvalSymlinks)
	if err != nil {
		t.Fatalf("canonicalize isolated OfficeCLI profile root: %v", err)
	}
	home := filepath.Join(root, "home")
	for _, directory := range []string{
		home,
		filepath.Join(root, "xdg-config"),
		filepath.Join(root, "xdg-cache"),
		filepath.Join(root, "xdg-data"),
		filepath.Join(root, "tmp"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	t.Setenv("TMPDIR", filepath.Join(root, "tmp"))
	t.Setenv("TMP", filepath.Join(root, "tmp"))
	t.Setenv("TEMP", filepath.Join(root, "tmp"))
	isolationBaseline := officeCLIProfileManifest(t, root, "")
	t.Cleanup(func() {
		isolationAfter := officeCLIProfileManifest(t, root, "")
		for _, name := range officeCLIUnexpectedIsolationChanges(isolationBaseline, isolationAfter) {
			t.Errorf("OfficeCLI changed isolated HOME/XDG/TMP path outside config/test-owned TMP root: %s", name)
		}
	})
	return home, officeCLIProfileManifest(t, home, ""), ""
}

func officeCLIUnexpectedIsolationChanges(before, after map[string]officeCLIManifestEntry) []string {
	allowed := map[string]bool{
		"home":                        true,
		"home/.officecli":             true,
		"home/.officecli/config.json": true,
	}
	unexpected := make([]string, 0)
	for _, name := range officeCLIManifestKeys(before, after) {
		if before[name] != after[name] && !allowed[name] {
			if name == "tmp" {
				beforeTMP, beforeExists := before[name]
				afterTMP, afterExists := after[name]
				if beforeExists && afterExists && beforeTMP.Mode.IsDir() && afterTMP.Mode.IsDir() {
					beforeTMP.ModTime = afterTMP.ModTime
					if beforeTMP == afterTMP {
						continue
					}
				}
			}
			unexpected = append(unexpected, name)
		}
	}
	return unexpected
}

func officeCLIRemoveDisposableFixture(t *testing.T, home, fixture string) {
	t.Helper()
	want := filepath.Join(home, ".agents", "skills", "officecli-pptx")
	if filepath.Clean(fixture) != filepath.Clean(want) {
		t.Errorf("refuse unsafe fixture cleanup %q", fixture)
		return
	}
	if err := os.RemoveAll(fixture); err != nil {
		t.Errorf("remove disposable OfficeCLI fixture: %v", err)
	}
	for _, directory := range []string{filepath.Dir(fixture), filepath.Dir(filepath.Dir(fixture))} {
		if entries, err := os.ReadDir(directory); err == nil && len(entries) == 0 {
			if err := os.Remove(directory); err != nil {
				t.Errorf("remove empty disposable fixture directory: %v", err)
			}
		}
	}
}

func officeCLIPreseedExistingAsset(t *testing.T, service *Service, provisioner *officeCLIProvisioner, asset catalog.OfficeCLIAsset, existing string) {
	t.Helper()
	if !filepath.IsAbs(existing) {
		t.Fatalf("TEAMKIT_OFFICECLI_EXISTING_PATH must be absolute: %q", existing)
	}
	data := officeCLIReadAsset(t, existing, asset.Size)
	if !service.digestVerifier()(data, asset.SHA256) {
		t.Fatal("TEAMKIT_OFFICECLI_EXISTING_PATH SHA-256 does not match catalog")
	}
	if err := provisioner.write(provisioner.Path(), data); err != nil {
		t.Fatalf("materialize verified existing OfficeCLI asset through production writer: %v", err)
	}
}

func officeCLIKeepEvidence(t *testing.T, service *Service, source string, asset catalog.OfficeCLIAsset, keep string) {
	t.Helper()
	if !filepath.IsAbs(keep) {
		t.Fatalf("TEAMKIT_OFFICECLI_KEEP_PATH must be an absolute directory: %q", keep)
	}
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatalf("create OfficeCLI evidence directory: %v", err)
	}
	data := officeCLIReadAsset(t, source, asset.Size)
	destination := filepath.Join(keep, asset.FileName)
	if err := service.officeCLIWriter()(destination, data); err != nil {
		t.Fatalf("write verified OfficeCLI evidence copy: %v", err)
	}
	assertOfficeCLIAsset(t, service, destination, asset)
	if !bytes.Equal(data, officeCLIReadAsset(t, destination, asset.Size)) {
		t.Fatal("OfficeCLI evidence copy bytes differ from 0700 managed binary")
	}
}

func assertOfficeCLIAsset(t *testing.T, service *Service, path string, asset catalog.OfficeCLIAsset) {
	t.Helper()
	data := officeCLIReadAsset(t, path, asset.Size)
	if int64(len(data)) != asset.Size {
		t.Fatalf("OfficeCLI size = %d, want %d", len(data), asset.Size)
	}
	if !service.digestVerifier()(data, asset.SHA256) {
		digest := sha256.Sum256(data)
		t.Fatalf("OfficeCLI SHA-256 = %s, want %s", hex.EncodeToString(digest[:]), asset.SHA256)
	}
}

func officeCLIReadAsset(t *testing.T, path string, expectedSize int64) []byte {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("OfficeCLI asset is not a regular file: %v", err)
	}
	if info.Size() != expectedSize || info.Size() > maxOfficeCLIBytes {
		t.Fatalf("OfficeCLI asset size = %d, want %d within limit", info.Size(), expectedSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OfficeCLI asset: %v", err)
	}
	return data
}

func assertOfficeCLIManagedMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("managed OfficeCLI mode = %04o, want %04o", info.Mode().Perm(), want)
	}
}

func assertOfficeCLIConfig(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted OfficeCLI config: %v", err)
	}
	state, err := officeCLIConfigState(data)
	if err != nil || state.AutoUpdate {
		t.Fatalf("persisted OfficeCLI config does not contain autoUpdate=false: %v", err)
	}
}

func assertOfficeCLIUpdateSiblingsAbsent(t *testing.T, binary string) {
	t.Helper()
	for _, suffix := range []string{".update", ".update.partial", ".old"} {
		if _, err := os.Lstat(binary + suffix); err == nil || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("OfficeCLI update sibling %q exists or cannot be checked: %v", suffix, err)
		}
	}
}

func assertOfficeCLIVersion(t *testing.T, binary, version string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), officeCLILiveStageTimeout)
	defer cancel()
	stdout, stderr, err := officeCLICapture(ctx, binary, []string{"--version"})
	if err != nil || len(stderr) != 0 {
		t.Fatalf("OfficeCLI --version failed: %v, stderr=%q", err, stderr)
	}
	got := strings.TrimSpace(string(stdout))
	if got != version && got != "officecli "+version && got != "OfficeCLI "+version {
		t.Fatalf("OfficeCLI --version = %q, want %q", got, version)
	}
}

// officeCLIMCPHandshake is the single reusable protocol implementation used by
// native runners and the ALT p11 test binary. OfficeCLI framing is one JSON-RPC
// object per line; no shell parser participates in this exchange.
func officeCLIMCPHandshake(t *testing.T, binary string) {
	t.Helper()
	processCtx, cancelProcess := context.WithTimeout(context.Background(), officeCLILiveProcessTimeout)
	defer cancelProcess()
	command := exec.CommandContext(processCtx, binary, []string{"mcp"}...)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &officeCLILiveBoundedBuffer{remaining: officeCLILiveReadLimit}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start OfficeCLI MCP: %v", err)
	}
	reader := bufio.NewReaderSize(stdout, officeCLILiveReadLimit+1)

	initialize := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"teamkit-live-smoke","version":"1"}}}` + "\n")
	writeOfficeCLIStage(t, cancelProcess, stdin, initialize)
	initializeResponse := readOfficeCLIResponse(t, cancelProcess, reader, 1)
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(initializeResponse.Result, &initialized); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	if initialized.ProtocolVersion != "2024-11-05" || initialized.ServerInfo.Name != "officecli" {
		t.Fatalf("unexpected initialize result: protocolVersion=%q serverInfo.name=%q", initialized.ProtocolVersion, initialized.ServerInfo.Name)
	}

	notification := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` + "\n")
	writeOfficeCLIStage(t, cancelProcess, stdin, notification)
	listRequest := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n")
	writeOfficeCLIStage(t, cancelProcess, stdin, listRequest)
	listResponse := readOfficeCLIResponse(t, cancelProcess, reader, 2)
	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listResponse.Result, &listed); err != nil {
		t.Fatalf("decode tools/list result: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "officecli" {
		t.Fatalf("tools/list = %#v, want exactly one officecli tool", listed.Tools)
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("close OfficeCLI MCP stdin: %v", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatalf("OfficeCLI MCP exit: %v, stderr=%q", err, stderr.String())
		}
	case <-processCtx.Done():
		t.Fatalf("OfficeCLI MCP did not exit within %s: %v, stderr=%q", officeCLILiveProcessTimeout, processCtx.Err(), stderr.String())
	}
}

type officeCLIRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func readOfficeCLIResponse(t *testing.T, cancel context.CancelFunc, reader *bufio.Reader, requestID int) officeCLIRPCResponse {
	t.Helper()
	deadline := time.NewTimer(officeCLILiveStageTimeout)
	defer deadline.Stop()
	for {
		line := make(chan struct {
			data []byte
			err  error
		}, 1)
		go func() {
			data, err := reader.ReadSlice('\n')
			line <- struct {
				data []byte
				err  error
			}{append([]byte(nil), data...), err}
		}()
		select {
		case result := <-line:
			if errors.Is(result.err, bufio.ErrBufferFull) || len(result.data) > officeCLILiveReadLimit {
				cancel()
				t.Fatalf("OfficeCLI MCP response exceeds %d bytes", officeCLILiveReadLimit)
			}
			if result.err != nil {
				cancel()
				t.Fatalf("read OfficeCLI MCP response: %v", result.err)
			}
			var response officeCLIRPCResponse
			if err := json.Unmarshal(bytes.TrimSpace(result.data), &response); err != nil {
				cancel()
				t.Fatalf("decode OfficeCLI MCP response: %v", err)
			}
			id, err := strconv.Atoi(string(response.ID))
			if err != nil || id != requestID {
				cancel()
				t.Fatalf("OfficeCLI MCP response id = %s, want %d", response.ID, requestID)
			}
			if response.JSONRPC != "2.0" || len(response.Error) != 0 || len(response.Result) == 0 {
				cancel()
				t.Fatalf("invalid OfficeCLI JSON-RPC response: jsonrpc=%q error=%s", response.JSONRPC, response.Error)
			}
			return response
		case <-deadline.C:
			cancel()
			t.Fatalf("OfficeCLI MCP response stage exceeded %s", officeCLILiveStageTimeout)
		}
	}
}

func writeOfficeCLIStage(t *testing.T, cancel context.CancelFunc, writer io.Writer, data []byte) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		_, err := writer.Write(data)
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			cancel()
			t.Fatalf("write OfficeCLI MCP stage: %v", err)
		}
	case <-time.After(officeCLILiveStageTimeout):
		cancel()
		t.Fatalf("OfficeCLI MCP write stage exceeded %s", officeCLILiveStageTimeout)
	}
}

type officeCLILiveBoundedBuffer struct {
	sync.Mutex
	bytes.Buffer
	remaining int
}

func (b *officeCLILiveBoundedBuffer) Write(data []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	if len(data) > b.remaining {
		return 0, fmt.Errorf("OfficeCLI MCP stderr exceeds %d bytes", officeCLILiveReadLimit)
	}
	b.remaining -= len(data)
	return b.Buffer.Write(data)
}

func (b *officeCLILiveBoundedBuffer) String() string {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.String()
}

type officeCLIManifestEntry struct {
	Mode    fs.FileMode
	Size    int64
	ModTime int64
	SHA256  string
}

func officeCLIProfileManifest(t *testing.T, root, hashedSubtree string) map[string]officeCLIManifestEntry {
	t.Helper()
	manifest := make(map[string]officeCLIManifestEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		item := officeCLIManifestEntry{Mode: info.Mode(), Size: info.Size(), ModTime: info.ModTime().UTC().UnixNano()}
		hash := runtime.GOOS != "windows" || relative == ".officecli/config.json"
		if hashedSubtree != "" {
			inside, err := filepath.Rel(hashedSubtree, path)
			hash = hash || (err == nil && inside != ".." && !filepath.IsAbs(inside) && !strings.HasPrefix(inside, ".."+string(filepath.Separator)))
		}
		if hash && info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(data)
			item.SHA256 = hex.EncodeToString(digest[:])
		}
		manifest[relative] = item
		return nil
	})
	if err != nil {
		t.Fatalf("manifest effective OfficeCLI profile %q: %v", root, err)
	}
	return manifest
}

func officeCLIWindowsProfileManifest(t *testing.T, root, fixture string) map[string]officeCLIManifestEntry {
	t.Helper()
	fixtureRelative := ""
	if fixture != "" {
		relative, err := filepath.Rel(root, fixture)
		if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("OfficeCLI fixture %q is outside effective profile %q: %v", fixture, root, err)
		}
		fixtureRelative = filepath.ToSlash(relative)
	}

	manifest := make(map[string]officeCLIManifestEntry)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		item := officeCLIManifestEntry{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			item.Size = info.Size()
			item.ModTime = info.ModTime().UTC().UnixNano()
			hash := relative == ".officecli/config.json" ||
				(fixtureRelative != "" && (relative == fixtureRelative || strings.HasPrefix(relative, fixtureRelative+"/")))
			if hash {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				digest := sha256.Sum256(data)
				item.SHA256 = hex.EncodeToString(digest[:])
			}
		}
		manifest[relative] = item
		if entry.IsDir() && officeCLIWindowsHostedRunnerChurnRoot(relative) {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("manifest effective OfficeCLI profile %q: %v", root, err)
	}
	return manifest
}

// GitHub-hosted Windows services concurrently maintain these two existing
// tool/OS state trees. Their root identity and mode remain manifested, but
// their descendants are excluded so unrelated runner churn is not attributed
// to the bounded OfficeCLI process.
func officeCLIWindowsHostedRunnerChurnRoot(relative string) bool {
	return strings.EqualFold(relative, "AppData") || strings.EqualFold(relative, ".docker")
}

func assertOfficeCLIProfileDelta(t *testing.T, before, after map[string]officeCLIManifestEntry, fixture string) {
	t.Helper()
	allowed := map[string]bool{".officecli": true, ".officecli/config.json": true}
	fixtureRelative := ""
	if fixture != "" {
		home, err := officeCLIUserHome()
		if err != nil {
			t.Fatal(err)
		}
		fixtureRelative, err = filepath.Rel(home, fixture)
		if err != nil {
			t.Fatal(err)
		}
		fixtureRelative = filepath.ToSlash(fixtureRelative)
	}
	for _, name := range officeCLIManifestKeys(before, after) {
		if before[name] == after[name] {
			continue
		}
		if allowed[name] || (fixtureRelative != "" && (name == fixtureRelative || strings.HasPrefix(name, fixtureRelative+"/"))) {
			continue
		}
		t.Errorf("OfficeCLI changed profile path outside exact config/preseed identity: %s", name)
	}
	if _, ok := after[".officecli/config.json"]; !ok {
		t.Error("OfficeCLI profile delta does not contain .officecli/config.json")
	}
}

func assertOfficeCLIManifestEqual(t *testing.T, first, second map[string]officeCLIManifestEntry, scope string) {
	t.Helper()
	for _, name := range officeCLIManifestKeys(first, second) {
		if first[name] != second[name] {
			t.Errorf("%s changed profile manifest entry %s", scope, name)
		}
	}
}

func officeCLIManifestKeys(left, right map[string]officeCLIManifestEntry) []string {
	set := make(map[string]bool, len(left)+len(right))
	for key := range left {
		set[key] = true
	}
	for key := range right {
		set[key] = true
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestOfficeCLILiveHarness_CanonicalizesRedirectedManagedRoot(t *testing.T) {
	redirected := t.TempDir()
	canonical := filepath.Join(filepath.Dir(redirected), "canonical-managed-root")
	got, err := officeCLICanonicalTestRoot(redirected, func(path string) (string, error) {
		if path != redirected {
			t.Fatalf("canonicalizer input = %q, want %q", path, redirected)
		}
		return canonical, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != canonical {
		t.Fatalf("canonical managed root = %q, want %q", got, canonical)
	}
}

func TestOfficeCLILiveHarness_UnixIsolationAllowsOnlyOwnedTMPRoot(t *testing.T) {
	before := map[string]officeCLIManifestEntry{
		"home": {Mode: fs.ModeDir | 0o700, ModTime: 1},
		"tmp":  {Mode: fs.ModeDir | 0o700, Size: 10, ModTime: 1},
	}
	after := map[string]officeCLIManifestEntry{
		"home":                        {Mode: fs.ModeDir | 0o700, ModTime: 1},
		"home/.officecli":             {Mode: fs.ModeDir | 0o700, ModTime: 2},
		"home/.officecli/config.json": {Mode: 0o600, Size: 20, ModTime: 2, SHA256: "config"},
		"home/unexpected":             {Mode: fs.ModeDir | 0o700, ModTime: 2},
		"tmp":                         {Mode: fs.ModeDir | 0o700, Size: 10, ModTime: 2},
		"tmp/unexpected":              {Mode: 0o600, Size: 1, ModTime: 2, SHA256: "tmp"},
	}
	got := officeCLIUnexpectedIsolationChanges(before, after)
	want := []string{"home/unexpected", "tmp/unexpected"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected isolation changes = %v, want %v", got, want)
	}

	for _, test := range []struct {
		name  string
		after map[string]officeCLIManifestEntry
	}{
		{
			name: "mode change",
			after: map[string]officeCLIManifestEntry{
				"home": {Mode: fs.ModeDir | 0o700, ModTime: 1},
				"tmp":  {Mode: fs.ModeDir | 0o777, Size: 10, ModTime: 2},
			},
		},
		{
			name: "file replacement",
			after: map[string]officeCLIManifestEntry{
				"home": {Mode: fs.ModeDir | 0o700, ModTime: 1},
				"tmp":  {Mode: 0o600, Size: 10, ModTime: 2, SHA256: "replacement"},
			},
		},
		{
			name: "disappearance",
			after: map[string]officeCLIManifestEntry{
				"home": {Mode: fs.ModeDir | 0o700, ModTime: 1},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := officeCLIUnexpectedIsolationChanges(before, test.after)
			if strings.Join(got, ",") != "tmp" {
				t.Fatalf("unexpected isolation changes = %v, want [tmp]", got)
			}
		})
	}
}

func TestOfficeCLILiveHarness_WindowsManifestIgnoresRunnerChurnButDetectsNewIdentities(t *testing.T) {
	home := t.TempDir()
	fixture := filepath.Join(home, ".agents", "skills", "officecli-pptx")
	for _, directory := range []string{fixture, filepath.Join(home, "AppData"), filepath.Join(home, ".docker"), filepath.Join(home, "ordinary")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(fixture, "SKILL.md"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := officeCLIWindowsProfileManifest(t, home, fixture)

	if err := os.WriteFile(filepath.Join(home, "AppData", "runner.lock"), []byte("runner"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".docker", "state.json"), []byte("runner"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "ordinary", "officecli-write"), []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "officecli-write"), []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	rogueSkill := filepath.Join(home, ".agents", "skills", "rogue", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(rogueSkill), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rogueSkill, []byte("rogue"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "rogue-home"), 0o700); err != nil {
		t.Fatal(err)
	}
	after := officeCLIWindowsProfileManifest(t, home, fixture)

	changed := make([]string, 0)
	for _, name := range officeCLIManifestKeys(before, after) {
		if before[name] != after[name] {
			changed = append(changed, name)
		}
	}
	want := []string{".agents/officecli-write", ".agents/skills/rogue", ".agents/skills/rogue/SKILL.md", "ordinary/officecli-write", "rogue-home"}
	if strings.Join(changed, ",") != strings.Join(want, ",") {
		t.Fatalf("targeted Windows profile changes = %v, want %v", changed, want)
	}
}

func TestOfficeCLILiveHarness_ALTDiagnosticsRequirePinnedImage(t *testing.T) {
	const pinnedQualificationImage = "teamkit-alt-p11-officecli:ephemeral"
	for _, test := range []struct {
		name    string
		mode    string
		image   string
		enabled bool
		wantErr bool
	}{
		{name: "absent", enabled: false},
		{name: "exact", mode: "stderr-stage-v1", image: pinnedQualificationImage, enabled: true},
		{name: "unknown mode", mode: "other", image: pinnedQualificationImage, wantErr: true},
		{name: "base image", mode: "stderr-stage-v1", image: "registry.altlinux.org/p11/alt@sha256:4c76520bb4935edf624dde76d5e670d54f40938323b185c4c7270881b71fd8ea", wantErr: true},
		{name: "unpinned image", mode: "stderr-stage-v1", image: "registry.altlinux.org/p11/alt:latest", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			enabled, err := officeCLILiveALTDiagnosticsEnabled(test.mode, test.image)
			if enabled != test.enabled || (err != nil) != test.wantErr {
				t.Fatalf("ALT diagnostics enabled=%t error=%v, want enabled=%t error=%t", enabled, err, test.enabled, test.wantErr)
			}
		})
	}
}

func TestOfficeCLILiveHarness_ALTDiagnosticsAreBoundedAndPassThrough(t *testing.T) {
	setErr := errors.New("exit status 1\n" + strings.Repeat("ошибка\x00", 1024))
	readStdout := []byte("false\n")
	readStderr := []byte("warning\r\n")
	records := make([]string, 0, 2)
	diagnostic := officeCLILiveALTDiagnostic{
		capture: func(_ context.Context, _ string, args []string) ([]byte, []byte, error) {
			if len(args) == 3 {
				return []byte(strings.Repeat("set-output\n", 1024)), []byte("libicu\x00missing\n"), setErr
			}
			return readStdout, readStderr, nil
		},
		emit: func(record string) { records = append(records, record) },
	}
	if err := diagnostic.run(context.Background(), "officecli", []string{"config", "autoUpdate", "false"}); !errors.Is(err, setErr) {
		t.Fatalf("diagnostic set error = %v, want original %v", err, setErr)
	}
	stdout, stderr, err := diagnostic.read(context.Background(), "officecli", []string{"config", "autoUpdate"})
	if err != nil || !bytes.Equal(stdout, readStdout) || !bytes.Equal(stderr, readStderr) {
		t.Fatalf("diagnostic read changed production capture result: stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
	if len(records) != 2 || !strings.HasPrefix(records[0], "OFFICECLI_ALT_DIAGNOSTIC stage=config-set status=error ") ||
		!strings.HasPrefix(records[1], "OFFICECLI_ALT_DIAGNOSTIC stage=config-read-back status=ok ") {
		t.Fatalf("ALT diagnostic stage records = %#v", records)
	}
	for _, record := range records {
		if len(record) > officeCLILiveDiagnosticRecordLimit {
			t.Fatalf("ALT diagnostic record exceeds %d bytes: %d", officeCLILiveDiagnosticRecordLimit, len(record))
		}
		for _, value := range []byte(record) {
			if value < 0x20 || value > 0x7e {
				t.Fatalf("ALT diagnostic record contains unsafe byte 0x%02x: %q", value, record)
			}
		}
	}
}
