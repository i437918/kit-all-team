package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/mi1man-cmd/kit-all-team/internal/bootstrap"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

func TestOfficeCLIProvisioner_CacheHitDisablesAutoUpdateWithoutDownload(t *testing.T) {
	p, binary, _ := testOfficeCLIProvisioner(t, []byte("verified OfficeCLI"))
	writeOfficeCLIBinary(t, binary, []byte("verified OfficeCLI"))
	writeOfficeCLIConfig(t, p.configPath, true, false)
	before := fileSHA256(t, binary)
	downloads, writes, setters, queries := 0, 0, 0, 0
	p.download = DownloadFunc(func(context.Context, string) ([]byte, error) {
		downloads++
		return nil, errors.New("must not download")
	})
	p.write = func(string, []byte) error { writes++; return errors.New("must not write") }
	p.run = platform.ProcessRunnerFunc(func(_ context.Context, name string, args []string) error {
		setters++
		if name != binary || fmt.Sprint(args) != "[config autoUpdate false]" {
			return fmt.Errorf("setter = %q %v", name, args)
		}
		writeOfficeCLIConfig(t, p.configPath, false, false)
		return nil
	})
	p.capture = func(_ context.Context, name string, args []string) ([]byte, []byte, error) {
		queries++
		if name != binary || fmt.Sprint(args) != "[config autoUpdate]" {
			return nil, nil, fmt.Errorf("query = %q %v", name, args)
		}
		return []byte("false\n"), nil, nil
	}
	if err := p.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if downloads != 0 || writes != 0 || setters != 1 || queries != 1 {
		t.Fatalf("download=%d write=%d setter=%d query=%d; want 0,0,1,1", downloads, writes, setters, queries)
	}
	if after := fileSHA256(t, binary); after != before {
		t.Fatalf("binary SHA changed: before %s after %s", before, after)
	}
	ready, err := p.Ready(context.Background())
	if err != nil || !ready {
		t.Fatalf("Ready() = %v, %v; want true, nil", ready, err)
	}
}

func TestOfficeCLIProvisioner_DownloadsVerifiesAndPublishes0700(t *testing.T) {
	p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	if err := p.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	got, err := os.ReadFile(binary)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("binary = %q, %v; want %q", got, err, payload)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(binary)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("binary mode = %v, %v; want 0700", info.Mode(), err)
		}
	}
}

func TestOfficeCLIBinaryModeReady_RequiresExactOwnerOnlyPOSIXMode(t *testing.T) {
	for _, tt := range []struct {
		name string
		goos string
		mode os.FileMode
		want bool
	}{
		{name: "posix owner only", goos: "linux", mode: 0o700, want: true},
		{name: "posix group readable executable", goos: "linux", mode: 0o755, want: false},
		{name: "posix world writable", goos: "linux", mode: 0o777, want: false},
		{name: "windows ignores POSIX mode", goos: "windows", mode: 0o777, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := officeCLIBinaryModeReady(tt.goos, tt.mode); got != tt.want {
				t.Fatalf("officeCLIBinaryModeReady(%q, %04o) = %v; want %v", tt.goos, tt.mode, got, tt.want)
			}
		})
	}
}

func TestOfficeCLIProvisioner_RepairsWidenedPOSIXModeBeforeExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX permission-mode contract")
	}
	for _, mode := range []os.FileMode{0o755, 0o777} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
			writeOfficeCLIBinary(t, binary, payload)
			if err := os.Chmod(binary, mode); err != nil {
				t.Fatal(err)
			}
			writeOfficeCLIConfig(t, p.configPath, false, false)

			ready, err := p.Ready(context.Background())
			if err != nil || ready {
				t.Fatalf("Ready() before repair = %v, %v; want false, nil", ready, err)
			}

			downloads, writes, setters, queries := 0, 0, 0, 0
			p.download = DownloadFunc(func(context.Context, string) ([]byte, error) {
				downloads++
				return append([]byte(nil), payload...), nil
			})
			p.write = func(path string, data []byte) error {
				writes++
				return workspace.WriteFileAtomic(path, data, 0o700)
			}
			requireExactBinary := func() error {
				info, err := os.Stat(binary)
				if err != nil {
					return err
				}
				if info.Mode().Perm() != 0o700 {
					return fmt.Errorf("binary mode before execution = %04o; want 0700", info.Mode().Perm())
				}
				got, err := os.ReadFile(binary)
				if err != nil {
					return err
				}
				if !bytes.Equal(got, payload) {
					return fmt.Errorf("binary bytes before execution = %q; want verified payload", got)
				}
				return nil
			}
			p.run = platform.ProcessRunnerFunc(func(_ context.Context, name string, args []string) error {
				setters++
				if name != binary || fmt.Sprint(args) != "[config autoUpdate false]" {
					return fmt.Errorf("setter = %q %v", name, args)
				}
				if err := requireExactBinary(); err != nil {
					return err
				}
				writeOfficeCLIConfig(t, p.configPath, false, false)
				return nil
			})
			p.capture = func(_ context.Context, name string, args []string) ([]byte, []byte, error) {
				queries++
				if name != binary || fmt.Sprint(args) != "[config autoUpdate]" {
					return nil, nil, fmt.Errorf("query = %q %v", name, args)
				}
				if err := requireExactBinary(); err != nil {
					return nil, nil, err
				}
				return []byte("false\n"), nil, nil
			}

			if err := p.Ensure(context.Background()); err != nil {
				t.Fatalf("Ensure() error = %v", err)
			}
			if downloads != 1 || writes != 1 || setters != 1 || queries != 1 {
				t.Fatalf("download=%d write=%d setter=%d query=%d; want 1,1,1,1", downloads, writes, setters, queries)
			}
			if err := requireExactBinary(); err != nil {
				t.Fatal(err)
			}
			if got := fileSHA256(t, binary); got != p.asset.SHA256 {
				t.Fatalf("binary SHA = %s; want %s", got, p.asset.SHA256)
			}
			ready, err = p.Ready(context.Background())
			if err != nil || !ready {
				t.Fatalf("Ready() after repair = %v, %v; want true, nil", ready, err)
			}
		})
	}
}

func TestOfficeCLIProvisioner_RejectsChecksumBeforeWrite(t *testing.T) {
	p, binary, _ := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	p.asset.SHA256 = stringsRepeat("0", 64)
	if err := p.Ensure(context.Background()); !errors.Is(err, errOfficeCLIAssetChecksum) {
		t.Fatalf("Ensure() error = %v; want OFFICECLI_ASSET_CHECKSUM_MISMATCH", err)
	}
	if _, err := os.Lstat(binary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("binary written despite checksum failure: %v", err)
	}
}

func TestOfficeCLIProvisioner_RejectsEmptyWrongSizeAndOversized(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
		want error
	}{
		{name: "empty", data: nil, want: errOfficeCLIAssetChecksum},
		{name: "wrong size", data: []byte("short"), want: errOfficeCLIAssetChecksum},
		{name: "oversized", data: bytes.Repeat([]byte("x"), maxOfficeCLIBytes+1), want: errOfficeCLIAssetTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, binary, _ := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
			p.download = DownloadFunc(func(context.Context, string) ([]byte, error) { return tt.data, nil })
			if err := p.Ensure(context.Background()); !errors.Is(err, tt.want) {
				t.Fatalf("Ensure() error = %v; want %v", err, tt.want)
			}
			if _, err := os.Lstat(binary); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("binary written on invalid payload: %v", err)
			}
		})
	}
}

func TestOfficeCLIProvisioner_RejectsCatalogAssetAboveCeiling(t *testing.T) {
	p, _, _ := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	p.asset.Size = maxOfficeCLIBytes + 1
	if err := p.Ensure(context.Background()); !errors.Is(err, errOfficeCLIAssetTooLarge) {
		t.Fatalf("Ensure() error = %v; want OFFICECLI_ASSET_TOO_LARGE", err)
	}
}

func TestOfficeCLIProvisioner_ReadyDetectsTamperAndNonExecutable(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, path string, payload []byte)
	}{
		{name: "tampered", setup: func(t *testing.T, path string, _ []byte) { writeOfficeCLIBinary(t, path, []byte("tampered")) }},
		{name: "not executable", setup: func(t *testing.T, path string, payload []byte) {
			writeOfficeCLIBinary(t, path, payload)
			if runtime.GOOS != "windows" {
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "not executable" && runtime.GOOS == "windows" {
				t.Skip("Windows has no POSIX executable-bit contract")
			}
			p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
			tt.setup(t, binary, payload)
			writeOfficeCLIConfig(t, p.configPath, false, false)
			ready, err := p.Ready(context.Background())
			if err != nil || ready {
				t.Fatalf("Ready() = %v, %v; want false, nil", ready, err)
			}
		})
	}
}

func TestOfficeCLIProvisioner_RejectsRedirectedManagedPath(t *testing.T) {
	p, binary, _ := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	external := testutil.TempDir(t)
	redirectOfficeCLIPath(t, filepath.Dir(binary), external)
	if err := p.Ensure(context.Background()); !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("Ensure() error = %v; want FOREIGN_HERMES_PROFILE", err)
	}
}

func TestOfficeCLIProvisioner_RejectsPathOutsideManagedLocation(t *testing.T) {
	p, _, _ := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	p.path = filepath.Join(testutil.TempDir(t), "officecli")
	downloads, runs := 0, 0
	p.download = DownloadFunc(func(context.Context, string) ([]byte, error) { downloads++; return nil, nil })
	p.run = platform.ProcessRunnerFunc(func(context.Context, string, []string) error { runs++; return nil })
	if err := p.Ensure(context.Background()); !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("Ensure() error = %v; want FOREIGN_HERMES_PROFILE", err)
	}
	if downloads != 0 || runs != 0 {
		t.Fatalf("outside managed path performed downloads=%d runs=%d", downloads, runs)
	}
}

func TestService_OfficeCLIProvisionerUsesManagedPathAndBoundedDownloader(t *testing.T) {
	hermesHome := testutil.TempDir(t)
	asset := catalog.OfficeCLIAsset{Version: "1.0.144", URL: "https://example.invalid/officecli", Size: 1, SHA256: stringsRepeat("0", 64)}
	p, err := New(Options{}).officeCLIProvisioner(asset, hermesHome)
	if err != nil {
		t.Fatalf("officeCLIProvisioner() error = %v", err)
	}
	if want := filepath.Join(hermesHome, ".teamkit", "officecli", asset.Version, officeCLIExecutableName()); p.Path() != want {
		t.Fatalf("Path() = %q; want %q", p.Path(), want)
	}
	downloader, ok := p.download.(httpDownloader)
	if !ok || downloader.maxBytes != maxOfficeCLIBytes || downloader.tooLargeError != "OFFICECLI_ASSET_TOO_LARGE" {
		t.Fatalf("OfficeCLI downloader = %#v; want 48 MiB OFFICECLI_ASSET_TOO_LARGE", p.download)
	}
}

func TestService_OfficeCLIProvisioner_ReResolvesEffectiveHomeBeforePreflight(t *testing.T) {
	initial, changed := testutil.TempDir(t), testutil.TempDir(t)
	previous := officeCLIUserHomeResolver
	calls := 0
	officeCLIUserHomeResolver = func() (string, error) {
		calls++
		if calls == 1 {
			return initial, nil
		}
		return changed, nil
	}
	t.Cleanup(func() { officeCLIUserHomeResolver = previous })
	hermesHome := testutil.TempDir(t)
	asset := catalog.OfficeCLIAsset{Version: "1.0.144", URL: "https://example.invalid/officecli", Size: 1, SHA256: stringsRepeat("0", 64)}
	p, err := New(Options{}).officeCLIProvisioner(asset, hermesHome)
	if err != nil {
		t.Fatal(err)
	}
	downloads, runs := 0, 0
	p.download = DownloadFunc(func(context.Context, string) ([]byte, error) { downloads++; return nil, nil })
	p.run = platform.ProcessRunnerFunc(func(context.Context, string, []string) error { runs++; return nil })
	if err := p.Ensure(context.Background()); !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("Ensure() error = %v; want FOREIGN_HERMES_PROFILE", err)
	}
	if calls < 2 || downloads != 0 || runs != 0 {
		t.Fatalf("resolver=%d downloads=%d runs=%d; want resolver >= 2 and no effects", calls, downloads, runs)
	}
}

func TestService_OfficeCLIProvisioner_ReadyReResolvesEffectiveHome(t *testing.T) {
	initial, changed := testutil.TempDir(t), testutil.TempDir(t)
	previous := officeCLIUserHomeResolver
	calls := 0
	officeCLIUserHomeResolver = func() (string, error) {
		calls++
		if calls == 1 {
			return initial, nil
		}
		return changed, nil
	}
	t.Cleanup(func() { officeCLIUserHomeResolver = previous })
	payload := []byte("x")
	digest := sha256.Sum256(payload)
	asset := catalog.OfficeCLIAsset{Version: "1.0.144", URL: "https://example.invalid/officecli", Size: int64(len(payload)), SHA256: hex.EncodeToString(digest[:])}
	p, err := New(Options{}).officeCLIProvisioner(asset, testutil.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	writeOfficeCLIBinary(t, p.Path(), payload)
	writeOfficeCLIConfig(t, p.configPath, false, false)
	ready, err := p.Ready(context.Background())
	if ready || !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("Ready() = %v, %v; want false, FOREIGN_HERMES_PROFILE", ready, err)
	}
	if calls < 2 {
		t.Fatalf("resolver calls = %d; want at least 2", calls)
	}
}

func TestOfficeCLIManagedPath_RejectsUnsafeVersionSegments(t *testing.T) {
	hermesHome := testutil.TempDir(t)
	for _, version := range []string{".", "..", "1/2", `1\2`} {
		t.Run(version, func(t *testing.T) {
			if _, err := officeCLIManagedPath(hermesHome, version); !errors.Is(err, bootstrap.ErrForeignProfile) {
				t.Fatalf("officeCLIManagedPath(%q) error = %v; want FOREIGN_HERMES_PROFILE", version, err)
			}
		})
	}
}

func TestOfficeCLIManagedPath_ContainsExactVersionAndExecutable(t *testing.T) {
	hermesHome := testutil.TempDir(t)
	path, err := officeCLIManagedPath(hermesHome, "1.0.144")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(hermesHome, ".teamkit", "officecli")
	relative, err := filepath.Rel(root, path)
	if err != nil || relative != filepath.Join("1.0.144", officeCLIExecutableName()) || filepath.IsAbs(relative) {
		t.Fatalf("managed path relative = %q, %v", relative, err)
	}
}

func TestOfficeCLIProvisioner_EnsureRepairsTamperedRegularFile(t *testing.T) {
	p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	writeOfficeCLIBinary(t, binary, []byte("tampered"))
	if err := p.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(binary)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("repaired binary = %q, %v", got, err)
	}
}

func TestOfficeCLIProvisioner_NeverExecutesUnverifiedBinary(t *testing.T) {
	p, binary, _ := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	writeOfficeCLIBinary(t, binary, []byte("tampered"))
	p.asset.SHA256 = stringsRepeat("0", 64)
	runs := 0
	p.run = platform.ProcessRunnerFunc(func(context.Context, string, []string) error { runs++; return nil })
	if err := p.Ensure(context.Background()); !errors.Is(err, errOfficeCLIAssetChecksum) {
		t.Fatalf("Ensure() error = %v", err)
	}
	if runs != 0 {
		t.Fatalf("executed %d unverified binaries", runs)
	}
}

func TestOfficeCLIProvisioner_RejectsRedirectedConfigOrLogPathBeforeProcess(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, p *officeCLIProvisioner)
	}{
		{name: "config directory", setup: func(t *testing.T, p *officeCLIProvisioner) {
			redirectOfficeCLIPath(t, filepath.Dir(p.configPath), testutil.TempDir(t))
		}},
		{name: "log", setup: func(t *testing.T, p *officeCLIProvisioner) {
			writeOfficeCLIConfig(t, p.configPath, false, true)
			redirectOfficeCLIPath(t, filepath.Join(filepath.Dir(p.configPath), "officecli.log"), testutil.TempDir(t))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _ := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
			tt.setup(t, p)
			runs := 0
			p.run = platform.ProcessRunnerFunc(func(context.Context, string, []string) error { runs++; return nil })
			if err := p.Ensure(context.Background()); !errors.Is(err, bootstrap.ErrForeignProfile) {
				t.Fatalf("Ensure() error = %v", err)
			}
			if runs != 0 {
				t.Fatalf("process started %d times", runs)
			}
		})
	}
}

func TestOfficeCLIProvisioner_ReadyRejectsRedirectedConfigOrLogPath(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, p *officeCLIProvisioner)
	}{
		{name: "config directory", setup: func(t *testing.T, p *officeCLIProvisioner) {
			redirectOfficeCLIPath(t, filepath.Dir(p.configPath), testutil.TempDir(t))
		}},
		{name: "log", setup: func(t *testing.T, p *officeCLIProvisioner) {
			writeOfficeCLIConfig(t, p.configPath, false, true)
			redirectOfficeCLIPath(t, filepath.Join(filepath.Dir(p.configPath), "officecli.log"), testutil.TempDir(t))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
			writeOfficeCLIBinary(t, binary, payload)
			tt.setup(t, p)
			ready, err := p.Ready(context.Background())
			if ready || !errors.Is(err, bootstrap.ErrForeignProfile) {
				t.Fatalf("Ready() = %v, %v", ready, err)
			}
		})
	}
}

func TestOfficeCLIUserHome_MatchesOfficeCLIEffectiveProfile(t *testing.T) {
	home, err := officeCLIUserHome()
	if err != nil || !filepath.IsAbs(home) {
		t.Fatalf("officeCLIUserHome() = %q, %v; want absolute profile", home, err)
	}
}

func TestOfficeCLIProvisioner_RejectsConfigSetFailureAndNonFalseReadback(t *testing.T) {
	for _, tt := range []struct {
		name   string
		runErr error
		stdout []byte
	}{
		{name: "setter error", runErr: errors.New("failed")}, {name: "readback true", stdout: []byte("true\n")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _ := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
			p.run = platform.ProcessRunnerFunc(func(context.Context, string, []string) error { return tt.runErr })
			p.capture = func(context.Context, string, []string) ([]byte, []byte, error) { return tt.stdout, nil, nil }
			if err := p.Ensure(context.Background()); !errors.Is(err, errOfficeCLIAutoUpdateConfig) {
				t.Fatalf("Ensure() error = %v", err)
			}
		})
	}
}

func TestOfficeCLIProvisioner_RejectsConfigTimeoutAndOversizedOutput(t *testing.T) {
	for _, tt := range []struct {
		name    string
		capture func(context.Context, string, []string) ([]byte, []byte, error)
	}{
		{name: "timeout", capture: func(context.Context, string, []string) ([]byte, []byte, error) {
			return nil, nil, context.DeadlineExceeded
		}},
		{name: "oversized", capture: func(context.Context, string, []string) ([]byte, []byte, error) {
			return bytes.Repeat([]byte("x"), maxHermesCommandOutput+1), nil, nil
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, _, _ := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
			p.capture = tt.capture
			if err := p.Ensure(context.Background()); !errors.Is(err, errOfficeCLIAutoUpdateConfig) {
				t.Fatalf("Ensure() error = %v", err)
			}
		})
	}
}

func TestOfficeCLIProvisioner_UsesFixedTenSecondChildDeadline(t *testing.T) {
	p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	writeOfficeCLIBinary(t, binary, payload)
	writeOfficeCLIConfig(t, p.configPath, true, false)
	setters, queries := 0, 0
	p.run = platform.ProcessRunnerFunc(func(ctx context.Context, name string, args []string) error {
		setters++
		requireOfficeCLIChildDeadline(t, ctx)
		if name != binary || fmt.Sprint(args) != "[config autoUpdate false]" {
			return fmt.Errorf("setter = %q %v", name, args)
		}
		writeOfficeCLIConfig(t, p.configPath, false, false)
		return nil
	})
	p.capture = func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		queries++
		requireOfficeCLIChildDeadline(t, ctx)
		if name != binary || fmt.Sprint(args) != "[config autoUpdate]" {
			return nil, nil, fmt.Errorf("query = %q %v", name, args)
		}
		return []byte("false\n"), nil, nil
	}
	if err := p.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if setters != 1 || queries != 1 {
		t.Fatalf("setter=%d query=%d; want 1,1", setters, queries)
	}
}

func TestOfficeCLIOutputBuffer_EnforcesCombinedLimit(t *testing.T) {
	limit := &officeCLIOutputLimit{remaining: maxHermesCommandOutput}
	stdout := officeCLIOutputBuffer{limit: limit}
	stderr := officeCLIOutputBuffer{limit: limit}
	if _, err := stdout.Write(bytes.Repeat([]byte("x"), maxHermesCommandOutput)); err != nil {
		t.Fatalf("stdout write error = %v", err)
	}
	if _, err := stderr.Write([]byte("x")); !errors.Is(err, errOfficeCLIAutoUpdateConfig) {
		t.Fatalf("stderr write error = %v; want OFFICECLI_AUTOUPDATE_CONFIG_FAILED", err)
	}
}

func TestOfficeCLIOutputBuffer_EnforcesCombinedLimitConcurrently(t *testing.T) {
	limit := &officeCLIOutputLimit{remaining: maxHermesCommandOutput}
	stdout := officeCLIOutputBuffer{limit: limit}
	stderr := officeCLIOutputBuffer{limit: limit}
	var group sync.WaitGroup
	results := make(chan error, maxHermesCommandOutput*2)
	for _, output := range []*officeCLIOutputBuffer{&stdout, &stderr} {
		group.Add(1)
		go func(output *officeCLIOutputBuffer) {
			defer group.Done()
			for range maxHermesCommandOutput {
				_, err := output.Write([]byte("x"))
				results <- err
			}
		}(output)
	}
	group.Wait()
	close(results)
	overflows := 0
	for err := range results {
		if errors.Is(err, errOfficeCLIAutoUpdateConfig) {
			overflows++
		} else if err != nil {
			t.Fatalf("write error = %v", err)
		}
	}
	if got := stdout.Len() + stderr.Len(); got != maxHermesCommandOutput || limit.remaining != 0 || overflows != maxHermesCommandOutput {
		t.Fatalf("accepted=%d remaining=%d overflows=%d; want %d,0,%d", got, limit.remaining, overflows, maxHermesCommandOutput, maxHermesCommandOutput)
	}
}

func TestOfficeCLIProvisioner_ReadyRequiresPersistedAutoUpdateFalse(t *testing.T) {
	p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	writeOfficeCLIBinary(t, binary, payload)
	writeOfficeCLIConfig(t, p.configPath, true, false)
	ready, err := p.Ready(context.Background())
	if err != nil || ready {
		t.Fatalf("Ready() = %v, %v; want false, nil", ready, err)
	}
}

func TestOfficeCLIConfigState_RejectsMissingMalformedDuplicateNonBoolAndTrue(t *testing.T) {
	for _, text := range []string{"", `{`, `{"autoUpdate":false}`, `{"lastUpdateCheck":null,"latestVersion":null,"autoUpdate":false,"autoUpdate":false,"log":false,"installedBinaryVersion":null,"lastSkillRefreshVersion":null}`, `{"lastUpdateCheck":null,"latestVersion":null,"autoUpdate":"false","log":false,"installedBinaryVersion":null,"lastSkillRefreshVersion":null}`, `{"lastUpdateCheck":null,"latestVersion":null,"autoUpdate":null,"log":false,"installedBinaryVersion":null,"lastSkillRefreshVersion":null}`, officeCLIConfigJSON(true, false)} {
		if state, err := officeCLIConfigState([]byte(text)); err == nil && !state.AutoUpdate {
			t.Fatalf("officeCLIConfigState(%q) = %#v, nil; want rejection", text, state)
		}
	}
}

func TestOfficeCLIConfigState_RejectsKnownFieldTypeMismatchAndWrongCase(t *testing.T) {
	for _, text := range []string{
		`{"lastUpdateCheck":1,"latestVersion":null,"autoUpdate":false,"log":false,"installedBinaryVersion":null,"lastSkillRefreshVersion":null}`,
		`{"lastUpdateCheck":null,"latestVersion":true,"autoUpdate":false,"log":false,"installedBinaryVersion":null,"lastSkillRefreshVersion":null}`,
		`{"lastUpdateCheck":null,"latestVersion":null,"autoUpdate":false,"log":null,"installedBinaryVersion":null,"lastSkillRefreshVersion":null}`,
		`{"lastUpdateCheck":null,"latestVersion":null,"autoUpdate":false,"log":false,"installedBinaryVersion":[],"lastSkillRefreshVersion":null}`,
		`{"lastUpdateCheck":null,"latestVersion":null,"autoUpdate":false,"log":false,"installedBinaryVersion":null,"lastSkillRefreshVersion":{}}`,
		`{"lastUpdateCheck":null,"latestVersion":null,"autoUpdate":false,"log":false,"installedBinaryVersion":null,"lastSkillRefreshVersion":null,"unknown":false}`,
		`{"lastUpdateCheck":null,"latestVersion":null,"autoupdate":false,"log":false,"installedBinaryVersion":null,"lastSkillRefreshVersion":null}`,
		`{"lastUpdateCheck":null,"latestVersion":null,"autoUpdate":false,"log":false,"installedBinaryVersion":null,"lastSkillRefreshVersion":null} trailing`,
	} {
		if _, err := officeCLIConfigState([]byte(text)); err == nil {
			t.Fatalf("officeCLIConfigState(%q) succeeded; want rejection", text)
		}
	}
}

func TestOfficeCLIConfigState_AcceptsValidNullableDateTime(t *testing.T) {
	text := `{"lastUpdateCheck":"2026-08-18T12:34:56.789Z","latestVersion":null,"autoUpdate":false,"log":true,"installedBinaryVersion":"1.0.144","lastSkillRefreshVersion":null}`
	state, err := officeCLIConfigState([]byte(text))
	if err != nil || state.AutoUpdate || !state.Log {
		t.Fatalf("officeCLIConfigState() = %#v, %v", state, err)
	}
}

func TestOfficeCLIProvisioner_ConfigDoesNotChangeBinarySHA(t *testing.T) {
	p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	writeOfficeCLIBinary(t, binary, payload)
	writeOfficeCLIConfig(t, p.configPath, true, false)
	before := fileSHA256(t, binary)
	if err := p.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after := fileSHA256(t, binary); after != before {
		t.Fatalf("binary SHA changed: before %s after %s", before, after)
	}
}

func TestOfficeCLIProvisioner_ReadyDetectsUpdateSiblings(t *testing.T) {
	for _, suffix := range []string{".update", ".update.partial", ".old"} {
		t.Run(suffix, func(t *testing.T) {
			p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
			writeOfficeCLIBinary(t, binary, payload)
			writeOfficeCLIConfig(t, p.configPath, false, false)
			if err := os.WriteFile(binary+suffix, []byte("drift"), 0o600); err != nil {
				t.Fatal(err)
			}
			ready, err := p.Ready(context.Background())
			if err != nil || ready {
				t.Fatalf("Ready() = %v, %v; want false, nil", ready, err)
			}
		})
	}
}

func TestOfficeCLIProvisioner_RemoveFailureHasStableError(t *testing.T) {
	p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	writeOfficeCLIBinary(t, binary, payload)
	writeOfficeCLIConfig(t, p.configPath, true, false)
	if err := os.WriteFile(binary+".old", []byte("drift"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.remove = func(string) error { return errors.New("locked") }
	if err := p.Ensure(context.Background()); !errors.Is(err, errOfficeCLIUpdateArtifactRemove) {
		t.Fatalf("Ensure() error = %v; want OFFICECLI_UPDATE_ARTIFACT_REMOVE_FAILED", err)
	}
}

func TestOfficeCLIProvisioner_EnsureRemovesRegularUpdateSiblingsAfterPolicySet(t *testing.T) {
	p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	writeOfficeCLIBinary(t, binary, payload)
	writeOfficeCLIConfig(t, p.configPath, true, false)
	untouched := filepath.Join(filepath.Dir(binary), "keep")
	if err := os.WriteFile(untouched, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".update", ".update.partial", ".old"} {
		if err := os.WriteFile(binary+suffix, []byte("drift"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".update", ".update.partial", ".old"} {
		if _, err := os.Lstat(binary + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("sibling %s remains: %v", suffix, err)
		}
	}
	if data, err := os.ReadFile(untouched); err != nil || string(data) != "keep" {
		t.Fatalf("unrelated file = %q, %v", data, err)
	}
}

func TestOfficeCLIProvisioner_RejectsUnsafeUpdateSibling(t *testing.T) {
	p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	writeOfficeCLIBinary(t, binary, payload)
	writeOfficeCLIConfig(t, p.configPath, false, false)
	if err := os.WriteFile(binary+".update", []byte("normal drift"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(binary+".old", 0o700); err != nil {
		t.Fatal(err)
	}
	runs := 0
	p.run = platform.ProcessRunnerFunc(func(context.Context, string, []string) error { runs++; return nil })
	if err := p.Ensure(context.Background()); !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("Ensure() error = %v", err)
	}
	if runs != 0 {
		t.Fatalf("process started %d times before unsafe update sibling was rejected", runs)
	}
}

func TestOfficeCLIProvisioner_LeavesExistingValidFileBytesUntouched(t *testing.T) {
	p, binary, payload := testOfficeCLIProvisioner(t, []byte("pinned OfficeCLI"))
	writeOfficeCLIBinary(t, binary, payload)
	writeOfficeCLIConfig(t, p.configPath, false, false)
	before, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	p.write = func(string, []byte) error { return errors.New("must not write") }
	if err := p.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(binary)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("binary after Ensure = %q, %v", after, err)
	}
}

func testOfficeCLIProvisioner(t *testing.T, payload []byte) (*officeCLIProvisioner, string, []byte) {
	t.Helper()
	home, hermesHome := testutil.TempDir(t), testutil.TempDir(t)
	binary := filepath.Join(hermesHome, ".teamkit", "officecli", "1.0.144", officeCLIExecutableName())
	digest := sha256.Sum256(payload)
	p := &officeCLIProvisioner{asset: catalog.OfficeCLIAsset{Version: "1.0.144", URL: "https://example.invalid/officecli", Size: int64(len(payload)), SHA256: hex.EncodeToString(digest[:])}, path: binary, configPath: filepath.Join(home, ".officecli", "config.json"), download: DownloadFunc(func(context.Context, string) ([]byte, error) { return append([]byte(nil), payload...), nil }), verify: func(data []byte, expected string) bool {
		got := sha256.Sum256(data)
		return hex.EncodeToString(got[:]) == expected
	}, write: func(path string, data []byte) error { return workspace.WriteFileAtomic(path, data, 0o700) }, run: platform.ProcessRunnerFunc(func(_ context.Context, _ string, args []string) error {
		if fmt.Sprint(args) != "[config autoUpdate false]" {
			return fmt.Errorf("unexpected setter arguments: %v", args)
		}
		writeOfficeCLIConfig(t, filepath.Join(home, ".officecli", "config.json"), false, false)
		return nil
	}), capture: func(_ context.Context, _ string, args []string) ([]byte, []byte, error) {
		if fmt.Sprint(args) != "[config autoUpdate]" {
			return nil, nil, fmt.Errorf("unexpected query arguments: %v", args)
		}
		return []byte("false\n"), nil, nil
	}, readConfig: func(path string) ([]byte, error) { return os.ReadFile(path) }, userHome: func() (string, error) { return home, nil }, hermesHome: hermesHome}
	return p, binary, payload
}

func writeOfficeCLIBinary(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := workspace.WriteFileAtomic(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
}
func writeOfficeCLIConfig(t *testing.T, path string, autoUpdate, log bool) {
	t.Helper()
	if err := workspace.WriteFileAtomic(path, []byte(officeCLIConfigJSON(autoUpdate, log)), 0o600); err != nil {
		t.Fatal(err)
	}
}
func officeCLIConfigJSON(autoUpdate, log bool) string {
	return fmt.Sprintf(`{"lastUpdateCheck":null,"latestVersion":null,"autoUpdate":%t,"log":%t,"installedBinaryVersion":null,"lastSkillRefreshVersion":null}`, autoUpdate, log)
}
func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
func stringsRepeat(value string, count int) string { return string(bytes.Repeat([]byte(value), count)) }

func requireOfficeCLIChildDeadline(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("child context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining > officeCLIChildTimeout || remaining < officeCLIChildTimeout-time.Second {
		t.Fatalf("child deadline remaining = %v; want approximately %v", remaining, officeCLIChildTimeout)
	}
}
