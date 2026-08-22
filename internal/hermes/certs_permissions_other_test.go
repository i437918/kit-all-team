//go:build !windows

package hermes

import (
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCertificateEnvironmentReady_RejectsBroadPOSIXPermissions(t *testing.T) {
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
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ready, err := CertificateEnvironmentReady(path, bundle)
	if err == nil || ready {
		t.Fatalf("CertificateEnvironmentReady() = %v, %v; want private-permission rejection", ready, err)
	}
}
