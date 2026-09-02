package pathsafe

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestReadRegular_ValidationAndReadStayBoundToOpenedHandle(t *testing.T) {
	directory := testutil.TempDir(t)
	path := filepath.Join(directory, ".env")
	if err := os.WriteFile(path, []byte("selected"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(directory, "replacement")
	if err := os.WriteFile(replacement, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := afterRegularOpen
	afterRegularOpen = func(string) error {
		if err := os.Rename(path, path+".opened"); err != nil {
			return err
		}
		return os.Rename(replacement, path)
	}
	defer func() { afterRegularOpen = original }()

	data, err := ReadRegular(path, 64<<10)
	if err != nil || string(data) != "selected" {
		t.Fatalf("ReadRegular() data=%q err=%v", data, err)
	}
}

func TestReadRegular_RejectsOversizeWithoutReturningPartialData(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), ".env")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 17), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadRegular(path, 16)
	if !errors.Is(err, ErrTooLarge) || data != nil {
		t.Fatalf("ReadRegular() data=%q err=%v, want ErrTooLarge", data, err)
	}
}
