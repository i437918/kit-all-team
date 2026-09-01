//go:build windows

package service

import (
	"context"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/bootstrap"
	"github.com/mi1man-cmd/kit-all-team/internal/cli"
	"github.com/mi1man-cmd/kit-all-team/internal/credentials"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/engine"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

func TestService_LoadDesiredRejectsJunctionKitHomeBeforeReadingEnv(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	junction := filepath.Join(root, "kit")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	desired := testDesired(t, junction, domain.AppCursor, true, "")
	writeDesiredAt(t, external, desired)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := New(Options{}).loadDesired(junction)
	if !errors.Is(err, bootstrap.ErrForeignWorkspace) {
		t.Fatalf("loadDesired() error = %v, want ErrForeignWorkspace", err)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
}

func TestService_CertificateCacheRejectsJunctionBeforePrivateWriter(t *testing.T) {
	desired := testDesired(t, filepath.Join(testutil.TempDir(t), "kit"), domain.AppHermes, true, filepath.Join(testutil.TempDir(t), "hermes"))
	applicationHome := testutil.TempDir(t)
	metadata := filepath.Join(applicationHome, ".teamkit")
	if err := os.Mkdir(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(metadata, "cache")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	source := filepath.Join(testutil.TempDir(t), "certs.zip")
	if err := os.WriteFile(source, []byte("verified fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	svc := New(Options{
		VerifyDigest: func([]byte, string) bool { return true },
		WritePrivate: func(path string, data []byte) error {
			writes++
			return os.WriteFile(path, data, 0o600)
		},
	})

	_, err := svc.certificateFor(desired, source, applicationHome)
	if !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("certificateFor() error = %v, want ErrForeignProfile", err)
	}
	if writes != 0 {
		t.Fatalf("private writer crossed junction: calls=%d", writes)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
}

func TestService_RetryRejectsJunctionedPlanLeafBeforeStateStore(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "kit")
	desired := testDesired(t, root, domain.AppCursor, true, "")
	if err := workspace.EnsureOwner(root, string(desired.Project())); err != nil {
		t.Fatal(err)
	}
	writeDesired(t, desired)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, ".teamkit", "plan.json")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	stateCalls := 0
	svc := New(Options{StateStore: func(string) (engine.Store, error) {
		stateCalls++
		return nil, errors.New("state store canary")
	}})

	err := svc.Retry(context.Background(), root)
	if !errors.Is(err, bootstrap.ErrForeignWorkspace) {
		t.Fatalf("Retry() error = %v, want ErrForeignWorkspace", err)
	}
	if stateCalls != 0 {
		t.Fatalf("state store opened across plan junction: %d", stateCalls)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
}

func TestService_ApplyRejectsJunctionedApplicationHomeBeforeFilesystemMutation(t *testing.T) {
	base := testutil.TempDir(t)
	kitHome := filepath.Join(base, "kit")
	external := testutil.TempDir(t)
	applicationHome := filepath.Join(base, "application")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", applicationHome, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	desired := testDesired(t, kitHome, domain.AppCursor, true, "")
	storeCalls := 0
	svc := New(Options{
		ApplicationHome: func(domain.DesiredState) (string, error) { return applicationHome, nil },
		SecretStore: func(string) (credentials.SecretStore, error) {
			storeCalls++
			return &recordingSecretStore{}, nil
		},
	})

	_, err := svc.Apply(context.Background(), desired, reconcile.UpdateNone, cli.ApplyInputs{})
	if !errors.Is(err, bootstrap.ErrForeignProfile) {
		t.Fatalf("Apply() error = %v, want ErrForeignProfile", err)
	}
	if storeCalls != 0 {
		t.Fatalf("secret store opened through application junction: %d", storeCalls)
	}
	if _, statErr := os.Lstat(kitHome); !os.IsNotExist(statErr) {
		t.Fatalf("Apply mutated KIT home before rejection: %v", statErr)
	}
}
