package hermes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"testing"
)

func TestCertificates_NormalizesSuppliedCertsDirectoryToManagedRoot(t *testing.T) {
	home := testutil.TempDir(t)
	archive := zipFixtures(t, map[string]string{
		"certs/ca-bundle.pem":            "bundle-data",
		"certs/enterprise-ca-chain.pem": "chain-data",
	})
	digest := sha256.Sum256(archive)

	bundle, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), home)
	if err != nil {
		t.Fatal(err)
	}
	wantBundle := filepath.Join(home, "certs", "ca-bundle.pem")
	if bundle != wantBundle {
		t.Fatalf("bundle path = %q, want %q", bundle, wantBundle)
	}
	if data, err := os.ReadFile(wantBundle); err != nil || string(data) != "bundle-data" {
		t.Fatalf("managed bundle = %q, %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(home, "certs", "certs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive prefix was published as a duplicate certs directory: %v", err)
	}

	retained, ready, err := ManagedCertificateBundle(home, hex.EncodeToString(digest[:]))
	if err != nil || !ready || retained != wantBundle {
		t.Fatalf("ManagedCertificateBundle() = %q, %v, %v; want %q, true, nil", retained, ready, err, wantBundle)
	}
}

func TestCertificates_RejectsMixedOrAmbiguousArchiveLayouts(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "mixed prefixed and root entries",
			files: map[string]string{
				"certs/ca-bundle.pem": "bundle-data",
				"company-ca.pem":      "company-data",
			},
		},
		{
			name: "duplicate after prefix normalization",
			files: map[string]string{
				"ca-bundle.pem":       "root-bundle",
				"certs/ca-bundle.pem": "prefixed-bundle",
			},
		},
		{
			name: "prefix traversal",
			files: map[string]string{
				"certs/../ca-bundle.pem": "bundle-data",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := zipFixtures(t, test.files)
			_, err := ExtractCertificates(bytes.NewReader(archive), int64(len(archive)), testutil.TempDir(t))
			if !errors.Is(err, ErrArchivePath) {
				t.Fatalf("ExtractCertificates() error = %v, want ErrArchivePath", err)
			}
		})
	}
}
