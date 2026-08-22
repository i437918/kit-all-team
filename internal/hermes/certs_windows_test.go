//go:build windows

package hermes

import (
	"bytes"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCertificates_RejectsJunctionDestinationWithoutTouchingTarget(t *testing.T) {
	home := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(home, "certs")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	archive := zipFixture(t, "ca-bundle.pem", "replacement")

	_, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home)
	if !errors.Is(err, ErrArchivePath) {
		t.Fatalf("ExtractCertificates() error = %v, want ErrArchivePath", err)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(external, "ca-bundle.pem")); !os.IsNotExist(statErr) {
		t.Fatalf("archive escaped through junction: %v", statErr)
	}
}

func TestCertificateEnvironmentReady_WindowsRejectsInheritedBroadDACL(t *testing.T) {
	home := testutil.TempDir(t)
	bundle := filepath.Join(home, "certs", "ca-bundle.pem")
	if err := os.MkdirAll(filepath.Dir(bundle), 0o700); err != nil {
		t.Fatal(err)
	}
	values := ApplicationCAEnvironment(bundle)
	values[CustomLLMProvider().APIKeyEnvironment] = "provider-test-value"
	var lines []string
	for key, value := range values {
		lines = append(lines, key+"="+value)
	}
	path := filepath.Join(home, ".env")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ready, err := CertificateEnvironmentReady(path, bundle)
	if err == nil || ready {
		t.Fatalf("CertificateEnvironmentReady() = %v, %v; want private-DACL rejection", ready, err)
	}
}
