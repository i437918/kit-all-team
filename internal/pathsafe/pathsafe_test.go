package pathsafe

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestComparisonKey_CleansSafePathsAndSeparatesChildren(t *testing.T) {
	root := testutil.TempDir(t)
	first, err := ComparisonKey(filepath.Join(root, "."))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComparisonKey(root)
	if err != nil {
		t.Fatal(err)
	}
	child, err := ComparisonKey(filepath.Join(root, "child"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second || child == first {
		t.Fatalf("first=%q second=%q child=%q", first, second, child)
	}
}

func TestComparisonKey_RejectsRedirectedExistingComponent(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	link := filepath.Join(root, "redirect")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ComparisonKey(filepath.Join(link, "kit")); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("err=%v", err)
	}
}

func TestEnsureDirectory_RejectsSymlinkedAncestorWithoutTouchingTarget(t *testing.T) {
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	err := EnsureDirectory(filepath.Join(link, "new-directory"), 0o700)
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("EnsureDirectory() error = %v, want ErrUnsafe", err)
	}
	if _, err := os.Stat(filepath.Join(external, "new-directory")); !os.IsNotExist(err) {
		t.Fatalf("write escaped through symlink: %v", err)
	}
	contents, err := os.ReadFile(sentinel)
	if err != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, err)
	}
}

func TestValidateRegular_RejectsSymlinkLeaf(t *testing.T) {
	root := testutil.TempDir(t)
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "leaf")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := ValidateRegular(link); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("ValidateRegular() error = %v, want ErrUnsafe", err)
	}
}

func TestValidateDirectory_RejectsRegularFileAncestor(t *testing.T) {
	root := testutil.TempDir(t)
	ancestor := filepath.Join(root, "regular-file")
	if err := os.WriteFile(ancestor, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ValidateDirectory(filepath.Join(ancestor, "child"))
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("ValidateDirectory() error = %v, want ErrUnsafe", err)
	}
	contents, readErr := os.ReadFile(ancestor)
	if readErr != nil || string(contents) != "unchanged" {
		t.Fatalf("regular-file ancestor = %q, %v", contents, readErr)
	}
}

func TestValidateComponents_ENOTDIRStillFindsRegularFileAncestor(t *testing.T) {
	root := testutil.TempDir(t)
	ancestor := filepath.Join(root, "regular-file")
	if err := os.WriteFile(ancestor, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(ancestor, "child")

	_, _, err := validateComponentsWithLstat(target, func(path string) (os.FileInfo, error) {
		if path == target {
			return nil, &os.PathError{Op: "lstat", Path: path, Err: syscall.ENOTDIR}
		}
		return os.Lstat(path)
	})
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("validateComponentsWithLstat() error = %v, want ErrUnsafe", err)
	}
}

func TestOverlaps_RecognizesSamePathAndBothNestingDirections(t *testing.T) {
	root := testutil.TempDir(t)
	child := filepath.Join(root, "child")
	sibling := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-sibling")
	for _, test := range []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "same", left: root, right: filepath.Join(root, "."), want: true},
		{name: "right nested", left: root, right: child, want: true},
		{name: "left nested", left: child, right: root, want: true},
		{name: "sibling", left: root, right: sibling, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Overlaps(test.left, test.right)
			if err != nil || got != test.want {
				t.Fatalf("Overlaps(%q, %q) = %t, %v; want %t, nil", test.left, test.right, got, err, test.want)
			}
		})
	}
}
