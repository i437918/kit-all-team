//go:build windows

package hermes

import (
	"errors"
	"fmt"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

func TestMaterializeToolchain_RejectsJunctionedProfileAncestorWithoutTouchingTarget(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainAIRules1C)
	if err != nil {
		t.Fatal(err)
	}
	source := testutil.TempDir(t)
	if err := writeToolchainTestSource(source, pin.Commit); err != nil {
		t.Fatal(err)
	}
	root := testutil.TempDir(t)
	external := testutil.TempDir(t)
	profile := filepath.Join(external, "profile")
	if err := os.Mkdir(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "profiles")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}

	err = MaterializeToolchain(source, filepath.Join(junction, "profile"), pin)
	if !errors.Is(err, ErrToolchainLayout) {
		t.Fatalf("MaterializeToolchain() error = %v, want ErrToolchainLayout", err)
	}
	for _, name := range []string{"external", "skills"} {
		if _, statErr := os.Lstat(filepath.Join(profile, name)); !os.IsNotExist(statErr) {
			t.Fatalf("materialization escaped through junction: %s: %v", name, statErr)
		}
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
}

func TestMaterializeToolchain_RejectsJunctionedSelectedSkillWithoutTouchingTarget(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainAIRules1C)
	if err != nil {
		t.Fatal(err)
	}
	source, profile, outside := testutil.TempDir(t), testutil.TempDir(t), testutil.TempDir(t)
	if err := writeToolchainTestSource(source, pin.Commit); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(profile, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "SKILL.md")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(profile, "skills", "fixture")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}

	err = MaterializeToolchain(source, profile, pin)
	if !errors.Is(err, ErrToolchainCollision) {
		t.Fatalf("MaterializeToolchain() error = %v, want ErrToolchainCollision", err)
	}
	contents, readErr := os.ReadFile(sentinel)
	if readErr != nil || string(contents) != "outside" {
		t.Fatalf("external sentinel = %q, %v", contents, readErr)
	}
	if _, statErr := os.Lstat(filepath.Join(profile, ".teamkit")); !os.IsNotExist(statErr) {
		t.Fatalf("pending state created after junction collision: %v", statErr)
	}
}

func TestMaterializeToolchain_RecoveryRestoresJunctionMovedByStagingCrash(t *testing.T) {
	pin, err := catalog.LookupToolchain(domain.ToolchainCC1CSkills)
	if err != nil {
		t.Fatal(err)
	}
	source, profile, outside := testutil.TempDir(t), testutil.TempDir(t), testutil.TempDir(t)
	writeToolchainSourceFixture(t, source, toolchainSkillsSubpath(pin.ID), pin.Commit, map[string]string{"alpha/SKILL.md": "# alpha\n"})
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	crash := errors.New("crash after staging rename")
	err = materializeToolchain(source, profile, pin, MaterializeOptions{
		NonceSource: fixedToolchainNonce,
		AfterStagingVerifyBeforeArchive: func(path string) error {
			if err := os.Rename(path, path+".original"); err != nil {
				return err
			}
			if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", path, outside).CombinedOutput(); err != nil {
				return fmt.Errorf("create staging junction: %w: %s", err, output)
			}
			return nil
		},
		AfterStagingRename: func() error { return crash },
	})
	if !errors.Is(err, crash) {
		t.Fatalf("first error = %v, want crash", err)
	}
	if retryErr := materializeToolchain(source, profile, pin, MaterializeOptions{NonceSource: fixedToolchainNonce}); retryErr == nil {
		t.Fatal("junction moved by crash was accepted")
	}
	if data, readErr := os.ReadFile(sentinel); readErr != nil || string(data) != "outside\n" {
		t.Fatalf("outside sentinel = %q, %v", data, readErr)
	}
}

func writeToolchainTestSource(root, commit string) error {
	for _, directory := range []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, "content", "skills", "fixture"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte(commit+"\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "content", "skills", "fixture", "SKILL.md"), []byte("fixture\n"), 0o600)
}
