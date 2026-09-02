//go:build !windows

package privatefile

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestValidatePOSIXMetadata_RequiresCurrentOwnerAndModeNoBroaderThan0600(t *testing.T) {
	const currentOwner = uint32(1000)
	for _, test := range []struct {
		name      string
		mode      fs.FileMode
		owner     uint32
		wantError bool
	}{
		{name: "exact 0600", mode: 0o600, owner: currentOwner},
		{name: "stricter 0400", mode: 0o400, owner: currentOwner},
		{name: "stricter 0000", mode: 0o000, owner: currentOwner},
		{name: "foreign owner", mode: 0o600, owner: currentOwner + 1, wantError: true},
		{name: "owner execute", mode: 0o700, owner: currentOwner, wantError: true},
		{name: "group read", mode: 0o640, owner: currentOwner, wantError: true},
		{name: "other read", mode: 0o604, owner: currentOwner, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validatePOSIXMetadata(test.mode, test.owner, currentOwner)
			if (err != nil) != test.wantError {
				t.Fatalf("validatePOSIXMetadata(%#o, %d, %d) error = %v, wantError=%t", test.mode, test.owner, currentOwner, err, test.wantError)
			}
		})
	}
}

func TestOpenValidated_POSIXRejectsSymlinkAndBroadPermissions(t *testing.T) {
	directory := testutil.TempDir(t)
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	broad := filepath.Join(directory, "broad")
	if err := os.WriteFile(broad, []byte("canary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(broad, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, broad} {
		file, err := OpenValidated(path)
		if file != nil {
			_ = file.Close()
		}
		if !errors.Is(err, ErrUnsafePermissions) {
			t.Fatalf("OpenValidated(%q) error = %v, want ErrUnsafePermissions", path, err)
		}
	}
}

func TestOpenValidated_POSIXValidationAndReadStayBoundToOpenedHandle(t *testing.T) {
	directory := testutil.TempDir(t)
	path := filepath.Join(directory, "registry")
	if err := os.WriteFile(path, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(directory, "replacement")
	if err := os.WriteFile(replacement, []byte("unsafe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(replacement, 0o644); err != nil {
		t.Fatal(err)
	}
	original := afterValidatedOpen
	afterValidatedOpen = func(string) error { return os.Rename(replacement, path) }
	defer func() { afterValidatedOpen = original }()

	file, err := OpenValidated(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil || string(body) != "safe" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}
