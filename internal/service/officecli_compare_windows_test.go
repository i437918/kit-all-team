//go:build windows

package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestOfficeCLIConfigPathMatches_WindowsUsesCaseInsensitiveFilename(t *testing.T) {
	home := testutil.TempDir(t)
	expected := filepath.Join(home, ".officecli", "config.json")
	actual := filepath.Join(home, ".officecli", strings.ToUpper("config.json"))
	matches, err := officeCLIConfigPathMatches(actual, expected)
	if err != nil || !matches {
		t.Fatalf("officeCLIConfigPathMatches() = %v, %v; want true, nil", matches, err)
	}
}
