package hermes

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

const legacyBundledSkillsOptOutMarker = "This profile opted out of bundled-skill seeding (`hermes profile create --no-skills`).\nDelete this file to re-enable sync on the next `hermes update`.\n"

type bundledSkillsOptInFunc func(context.Context, string) error

func (f bundledSkillsOptInFunc) OptInBundledSkills(ctx context.Context, identity string) error {
	return f(ctx, identity)
}

func TestExactLegacyOptOutMarker_AcceptsOnlyHermesOwnedBytes(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		marker  []byte
		want    bool
		wantErr error
	}{
		{name: "lf", marker: []byte(legacyBundledSkillsOptOutMarker), want: true},
		{name: "crlf", marker: []byte(strings.ReplaceAll(legacyBundledSkillsOptOutMarker, "\n", "\r\n")), want: true},
		{name: "one byte differs", marker: []byte(strings.Replace(legacyBundledSkillsOptOutMarker, "sync", "Sync", 1)), wantErr: ErrBundledSkillsUserOptOut},
		{name: "too large", marker: []byte(strings.Repeat("x", 257)), wantErr: ErrBundledSkillsUserOptOut},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			profile := testutil.TempDir(t)
			if err := os.WriteFile(filepath.Join(profile, ".no-bundled-skills"), fixture.marker, 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := ExactLegacyOptOutMarker(profile)
			if got != fixture.want || !errors.Is(err, fixture.wantErr) {
				t.Fatalf("ExactLegacyOptOutMarker() = %v, %v; want %v, %v", got, err, fixture.want, fixture.wantErr)
			}
		})
	}

	t.Run("redirected marker", func(t *testing.T) {
		profile, external := testutil.TempDir(t), testutil.TempDir(t)
		target := filepath.Join(external, "marker")
		if err := os.WriteFile(target, []byte(legacyBundledSkillsOptOutMarker), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(profile, ".no-bundled-skills")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if got, err := ExactLegacyOptOutMarker(profile); got || !errors.Is(err, ErrBundledSkillsUserOptOut) {
			t.Fatalf("ExactLegacyOptOutMarker() = %v, %v", got, err)
		}
	})
}

func TestMigrateOwnedBundledSkills_OptInExactOwnedMarkerAndPreservesUserData(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		marker string
	}{
		{name: "lf", marker: legacyBundledSkillsOptOutMarker},
		{name: "crlf", marker: strings.ReplaceAll(legacyBundledSkillsOptOutMarker, "\n", "\r\n")},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			marker := fixture.marker
			profile := migrationProfileFixture(t, []byte(marker))
			before := migrationDataHashes(t, profile)
			identity := "1c-apa-developer-cc_1c_skills"
			var calls []string
			verifications := 0
			verify := func(root, gotIdentity string) error {
				verifications++
				if root != profile || gotIdentity != identity {
					t.Fatalf("ownership verifier inputs = %q, %q", root, gotIdentity)
				}
				return nil
			}
			cli := bundledSkillsOptInFunc(func(_ context.Context, gotIdentity string) error {
				calls = append(calls, gotIdentity)
				return os.Remove(filepath.Join(profile, ".no-bundled-skills"))
			})

			if err := MigrateOwnedBundledSkills(context.Background(), profile, identity, verify, cli); err != nil {
				t.Fatalf("MigrateOwnedBundledSkills() error = %v", err)
			}
			if !reflect.DeepEqual(calls, []string{identity}) || verifications != 3 {
				t.Fatalf("calls=%#v verifications=%d", calls, verifications)
			}
			if _, err := os.Lstat(filepath.Join(profile, ".no-bundled-skills")); !os.IsNotExist(err) {
				t.Fatalf("legacy marker remains: %v", err)
			}
			if after := migrationDataHashes(t, profile); !reflect.DeepEqual(after, before) {
				t.Fatalf("user data changed: before=%#v after=%#v", before, after)
			}
			if err := MigrateOwnedBundledSkills(context.Background(), profile, identity, verify, cli); err != nil {
				t.Fatalf("idempotent MigrateOwnedBundledSkills() error = %v", err)
			}
			if !reflect.DeepEqual(calls, []string{identity}) {
				t.Fatalf("idempotent calls=%#v", calls)
			}
		})
	}
}

func TestMigrateOwnedBundledSkills_PreservesInvalidMarkerWithoutRunningHermes(t *testing.T) {
	profile := migrationProfileFixture(t, []byte(strings.Replace(legacyBundledSkillsOptOutMarker, "sync", "Sync", 1)))
	before := migrationDataHashes(t, profile)
	calls := 0
	err := MigrateOwnedBundledSkills(context.Background(), profile, "1c-apa-developer-cc_1c_skills", func(string, string) error { return nil }, bundledSkillsOptInFunc(func(context.Context, string) error {
		calls++
		return nil
	}))
	if !errors.Is(err, ErrBundledSkillsUserOptOut) || calls != 0 {
		t.Fatalf("MigrateOwnedBundledSkills() error=%v calls=%d", err, calls)
	}
	if after := migrationDataHashes(t, profile); !reflect.DeepEqual(after, before) {
		t.Fatalf("user data changed: before=%#v after=%#v", before, after)
	}
}

func TestMigrateOwnedBundledSkills_PreservesOversizedMarkerWithoutRunningHermes(t *testing.T) {
	profile := migrationProfileFixture(t, []byte(strings.Repeat("x", 257)))
	before := migrationDataHashes(t, profile)
	calls := 0
	err := MigrateOwnedBundledSkills(context.Background(), profile, "1c-apa-developer-cc_1c_skills", func(string, string) error { return nil }, bundledSkillsOptInFunc(func(context.Context, string) error {
		calls++
		return nil
	}))
	if !errors.Is(err, ErrBundledSkillsUserOptOut) || calls != 0 {
		t.Fatalf("MigrateOwnedBundledSkills() error=%v calls=%d", err, calls)
	}
	if after := migrationDataHashes(t, profile); !reflect.DeepEqual(after, before) {
		t.Fatalf("user data changed: before=%#v after=%#v", before, after)
	}
}

func TestMigrateOwnedBundledSkills_PreservesRedirectedMarkerWithoutRunningHermes(t *testing.T) {
	profile, external := migrationProfileFixture(t, nil), testutil.TempDir(t)
	marker := filepath.Join(profile, ".no-bundled-skills")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(external, "marker")
	if err := os.WriteFile(target, []byte(legacyBundledSkillsOptOutMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, marker); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before := migrationDataHashes(t, profile)
	calls := 0
	err := MigrateOwnedBundledSkills(context.Background(), profile, "1c-apa-developer-cc_1c_skills", func(string, string) error { return nil }, bundledSkillsOptInFunc(func(context.Context, string) error {
		calls++
		return nil
	}))
	if !errors.Is(err, ErrBundledSkillsUserOptOut) || calls != 0 {
		t.Fatalf("MigrateOwnedBundledSkills() error=%v calls=%d", err, calls)
	}
	if after := migrationDataHashes(t, profile); !reflect.DeepEqual(after, before) {
		t.Fatalf("user data changed: before=%#v after=%#v", before, after)
	}
}

func TestMigrateOwnedBundledSkills_RejectsForeignProfileBeforeMarkerRead(t *testing.T) {
	profile := migrationProfileFixture(t, []byte(legacyBundledSkillsOptOutMarker))
	before := migrationDataHashes(t, profile)
	calls := 0
	err := MigrateOwnedBundledSkills(context.Background(), profile, "1c-apa-developer-cc_1c_skills", func(string, string) error {
		return ErrForeignHermesProfile
	}, bundledSkillsOptInFunc(func(context.Context, string) error {
		calls++
		return nil
	}))
	if !errors.Is(err, ErrForeignHermesProfile) || calls != 0 {
		t.Fatalf("MigrateOwnedBundledSkills() error=%v calls=%d", err, calls)
	}
	if after := migrationDataHashes(t, profile); !reflect.DeepEqual(after, before) {
		t.Fatalf("foreign profile data changed: before=%#v after=%#v", before, after)
	}
}

func TestMigrateOwnedBundledSkills_RejectsRedirectedProfileRootBeforeRunningHermes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction fixture is Windows-specific")
	}
	parent, external := testutil.TempDir(t), migrationProfileFixture(t, []byte(legacyBundledSkillsOptOutMarker))
	before := migrationDataHashes(t, external)
	profile := filepath.Join(parent, "profile")
	if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", profile, external).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	calls := 0
	err := MigrateOwnedBundledSkills(context.Background(), profile, "1c-apa-developer-cc_1c_skills", func(string, string) error { return nil }, bundledSkillsOptInFunc(func(context.Context, string) error {
		calls++
		return nil
	}))
	if !errors.Is(err, ErrForeignHermesProfile) || calls != 0 {
		t.Fatalf("MigrateOwnedBundledSkills() error=%v calls=%d", err, calls)
	}
	if after := migrationDataHashes(t, external); !reflect.DeepEqual(after, before) {
		t.Fatalf("junction target data changed: before=%#v after=%#v", before, after)
	}
}

func TestMigrateOwnedBundledSkills_RejectsRootReplacementOnMissingMarker(t *testing.T) {
	profile := migrationProfileFixture(t, nil)
	if err := os.Remove(filepath.Join(profile, ".no-bundled-skills")); err != nil {
		t.Fatal(err)
	}
	parked := filepath.Join(filepath.Dir(profile), "original-profile")
	previous := afterProfileMarkerRead
	afterProfileMarkerRead = func() {
		if err := os.Rename(profile, parked); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(profile, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterProfileMarkerRead = previous })
	calls := 0
	err := MigrateOwnedBundledSkills(context.Background(), profile, "1c-apa-developer-cc_1c_skills", func(string, string) error { return nil }, bundledSkillsOptInFunc(func(context.Context, string) error {
		calls++
		return nil
	}))
	if !errors.Is(err, ErrForeignHermesProfile) || calls != 0 {
		t.Fatalf("MigrateOwnedBundledSkills() error=%v calls=%d", err, calls)
	}
}

func TestMigrateOwnedBundledSkills_FailsClosedAfterCommand(t *testing.T) {
	identity := "1c-apa-developer-cc_1c_skills"
	t.Run("marker remains", func(t *testing.T) {
		profile := migrationProfileFixture(t, []byte(legacyBundledSkillsOptOutMarker))
		err := MigrateOwnedBundledSkills(context.Background(), profile, identity, func(string, string) error { return nil }, bundledSkillsOptInFunc(func(context.Context, string) error { return nil }))
		if !errors.Is(err, ErrBundledSkillsMigrationFailed) {
			t.Fatalf("MigrateOwnedBundledSkills() error=%v", err)
		}
	})

	t.Run("marker recreated with distinct bytes", func(t *testing.T) {
		profile := migrationProfileFixture(t, []byte(legacyBundledSkillsOptOutMarker))
		err := MigrateOwnedBundledSkills(context.Background(), profile, identity, func(string, string) error { return nil }, bundledSkillsOptInFunc(func(context.Context, string) error {
			if err := os.Remove(filepath.Join(profile, ".no-bundled-skills")); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(profile, ".no-bundled-skills"), []byte("different marker"), 0o600)
		}))
		if !errors.Is(err, ErrBundledSkillsMigrationFailed) {
			t.Fatalf("MigrateOwnedBundledSkills() error=%v", err)
		}
	})

	t.Run("ownership changes", func(t *testing.T) {
		profile := migrationProfileFixture(t, []byte(legacyBundledSkillsOptOutMarker))
		checks := 0
		err := MigrateOwnedBundledSkills(context.Background(), profile, identity, func(string, string) error {
			checks++
			if checks == 3 {
				return ErrForeignHermesProfile
			}
			return nil
		}, bundledSkillsOptInFunc(func(context.Context, string) error {
			return os.Remove(filepath.Join(profile, ".no-bundled-skills"))
		}))
		if !errors.Is(err, ErrForeignHermesProfile) {
			t.Fatalf("MigrateOwnedBundledSkills() error=%v", err)
		}
	})

	t.Run("profile root replaced", func(t *testing.T) {
		profile := migrationProfileFixture(t, []byte(legacyBundledSkillsOptOutMarker))
		parked := filepath.Join(filepath.Dir(profile), "original-profile")
		err := MigrateOwnedBundledSkills(context.Background(), profile, identity, func(string, string) error { return nil }, bundledSkillsOptInFunc(func(context.Context, string) error {
			if err := os.Remove(filepath.Join(profile, ".no-bundled-skills")); err != nil {
				return err
			}
			if err := os.Rename(profile, parked); err != nil {
				return err
			}
			return os.Mkdir(profile, 0o700)
		}))
		if !errors.Is(err, ErrForeignHermesProfile) {
			t.Fatalf("MigrateOwnedBundledSkills() error=%v", err)
		}
	})

	t.Run("command error with replaced root", func(t *testing.T) {
		profile := migrationProfileFixture(t, []byte(legacyBundledSkillsOptOutMarker))
		parked := filepath.Join(filepath.Dir(profile), "original-profile")
		err := MigrateOwnedBundledSkills(context.Background(), profile, identity, func(string, string) error { return nil }, bundledSkillsOptInFunc(func(context.Context, string) error {
			if err := os.Rename(profile, parked); err != nil {
				return err
			}
			if err := os.Mkdir(profile, 0o700); err != nil {
				return err
			}
			return errors.New("hermes command failed")
		}))
		if !errors.Is(err, ErrForeignHermesProfile) {
			t.Fatalf("MigrateOwnedBundledSkills() error=%v", err)
		}
	})
}

func migrationProfileFixture(t *testing.T, marker []byte) string {
	t.Helper()
	profile := testutil.TempDir(t)
	for path, data := range map[string]string{
		"sessions/session.json":   "session-canary",
		"skills/Learned/SKILL.md": "learned-canary",
		"user-data/notes.txt":     "user-canary",
		".no-bundled-skills":      string(marker),
	} {
		full := filepath.Join(profile, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return profile
}

func migrationDataHashes(t *testing.T, profile string) map[string][sha256.Size]byte {
	t.Helper()
	result := make(map[string][sha256.Size]byte)
	for _, relative := range []string{"sessions/session.json", "skills/Learned/SKILL.md", "user-data/notes.txt"} {
		data, err := os.ReadFile(filepath.Join(profile, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		result[relative] = sha256.Sum256(data)
	}
	return result
}
