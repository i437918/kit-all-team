package hermes

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

const (
	runtimeConfigSchema34 = "DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n}\n"
	runtimeConfigSchema37 = "DEFAULT_CONFIG = {\n    \"_config_version\": 37,\n}\n"
)

func TestVerifyRuntimeContract_SameVersionUsesProvenSchema(t *testing.T) {
	for schema, config := range map[int]string{34: runtimeConfigSchema34, 37: runtimeConfigSchema37} {
		t.Run(strconv.Itoa(schema), func(t *testing.T) {
			root, executable := writeRuntimeFixture(t, config, []string{"github", "software-development"})
			writeBundledSkill(t, root, filepath.Join("nested", "nested-development"), "nested-development")
			contract, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
			if err != nil {
				t.Fatal(err)
			}
			if contract.ConfigSchema != schema {
				t.Fatalf("schema=%d want=%d", contract.ConfigSchema, schema)
			}
			if got, want := contract.BundledSkills, []string{"github", "nested-development", "software-development"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("skills=%q want=%q", got, want)
			}
		})
	}
}

func TestVerifyRuntimeContract_ParsesPinnedDefaultConfigMapping(t *testing.T) {
	for name, fixture := range map[string]struct {
		config string
		want   int
	}{
		"mapping":              {"\"\"\"ordinary module docstring\"\"\"\nDEFAULT_CONFIG = {\n    \"provider\": \"openai\",\n    \"_config_version\": 37,\n}\n", 37},
		"comment-before-close": {"DEFAULT_CONFIG = {\n    \"_config_version\": 34 # audited managed schema\n}\n", 34},
		"nested-decoy":         {"DEFAULT_CONFIG = {\n    \"metadata\": {\"_config_version\": \"not the schema\"},\n    \"_config_version\": 37, # exact top-level value\n}\n", 37},
		"literal-value":        {"DEFAULT_CONFIG = {\n    \"field_name\": \"_config_version\",\n    \"_config_version\": 34,\n}\n", 34},
	} {
		t.Run(name, func(t *testing.T) {
			root, executable := writeRuntimeFixture(t, fixture.config, []string{"github"})
			contract, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
			if err != nil || contract.ConfigSchema != fixture.want {
				t.Fatalf("VerifyRuntimeContract()=%#v,%v want schema %d", contract, err, fixture.want)
			}
		})
	}
}

func TestVerifyRuntimeContract_RejectsExecutableReplacementDuringProbe(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	capture := runtimeFixtureCapture(root)
	blocked := false
	_, err := VerifyRuntimeContract(context.Background(), executable, func(ctx context.Context, path string, args []string) ([]byte, error) {
		data, captureErr := capture(ctx, path, args)
		if strings.Join(args, " ") == "--version" {
			if removeErr := os.Remove(executable); removeErr != nil {
				if runtime.GOOS == "windows" {
					blocked = true
					return data, captureErr
				}
				t.Fatal(removeErr)
			}
			if writeErr := os.WriteFile(executable, []byte("replacement"), 0o700); writeErr != nil {
				t.Fatal(writeErr)
			}
		}
		return data, captureErr
	})
	if blocked {
		if err != nil {
			t.Fatalf("contract failed after Windows blocked executable replacement: %v", err)
		}
		return
	}
	if !errors.Is(err, ErrExecutableUnverified) {
		t.Fatalf("err=%v, want HERMES_EXECUTABLE_UNVERIFIED", err)
	}
}

func TestVerifyRuntimeContract_RejectsExecutableReplacementAfterLastProbe(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	original := beforeRuntimeRootOpen
	blocked := false
	beforeRuntimeRootOpen = func() {
		beforeRuntimeRootOpen = func() {}
		if err := os.Remove(executable); err != nil {
			if runtime.GOOS == "windows" {
				blocked = true
				return
			}
			t.Fatal(err)
		}
		if err := os.WriteFile(executable, []byte("replacement"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeRuntimeRootOpen = original })
	_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if blocked {
		if err != nil {
			t.Fatalf("contract failed after Windows blocked executable replacement: %v", err)
		}
		return
	}
	if !errors.Is(err, ErrExecutableUnverified) {
		t.Fatalf("err=%v, want HERMES_EXECUTABLE_UNVERIFIED", err)
	}
}

func TestVerifyRuntimeContract_RejectsInventoryCancellation(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := afterRuntimeSchemaProbe
	afterRuntimeSchemaProbe = func(openedInstallRoot) { cancel() }
	t.Cleanup(func() { afterRuntimeSchemaProbe = original })
	_, err := VerifyRuntimeContract(ctx, executable, runtimeFixtureCapture(root))
	if !errors.Is(err, ErrBundledSkillsCatalogUnverified) {
		t.Fatalf("err=%v, want HERMES_BUNDLED_SKILLS_CATALOG_UNVERIFIED", err)
	}
}

func TestVerifyRuntimeContract_RejectsInstallRootSwapDuringInventory(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	replacement := filepath.Join(testutil.TempDir(t), "replacement")
	if err := os.MkdirAll(replacement, 0o700); err != nil {
		t.Fatal(err)
	}
	original := afterRuntimeSchemaProbe
	afterRuntimeSchemaProbe = func(opened openedInstallRoot) {
		afterRuntimeSchemaProbe = func(openedInstallRoot) {}
		simulateInstallRootPathSwap(t, opened, replacement)
	}
	t.Cleanup(func() { afterRuntimeSchemaProbe = original })
	_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if !errors.Is(err, ErrBundledSkillsCatalogUnverified) {
		t.Fatalf("err=%v, want HERMES_BUNDLED_SKILLS_CATALOG_UNVERIFIED", err)
	}
}

func TestVerifyRuntimeContract_RejectsRedirectedConfigDefaults(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	config := filepath.Join(root, "hermes_cli", "config_defaults.py")
	target := filepath.Join(testutil.TempDir(t), "config_defaults.py")
	if err := os.WriteFile(target, []byte(runtimeConfigSchema34), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, config); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if !errors.Is(err, ErrConfigSchemaUnsupported) {
		t.Fatalf("err=%v, want HERMES_CONFIG_SCHEMA_UNSUPPORTED", err)
	}
}

func TestVerifyRuntimeContract_RejectsConfigLeafSwapBetweenStatAndOpen(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	config := filepath.Join(root, "hermes_cli", "config_defaults.py")
	replacement := config + ".replacement"
	if err := os.WriteFile(replacement, []byte(runtimeConfigSchema37), 0o600); err != nil {
		t.Fatal(err)
	}
	original := afterRuntimeLeafLstat
	afterRuntimeLeafLstat = func(relative string) {
		if filepath.ToSlash(relative) != "hermes_cli/config_defaults.py" {
			return
		}
		afterRuntimeLeafLstat = func(string) {}
		if err := os.Remove(config); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, config); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterRuntimeLeafLstat = original })
	_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if !errors.Is(err, ErrConfigSchemaUnsupported) {
		t.Fatalf("err=%v, want HERMES_CONFIG_SCHEMA_UNSUPPORTED", err)
	}
}

func TestVerifyRuntimeContract_RejectsBundledSkillLeafSwapBetweenStatAndOpen(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	skill := filepath.Join(root, "skills", "github", "SKILL.md")
	replacement := skill + ".replacement"
	if err := os.WriteFile(replacement, []byte("---\nname: github\n---\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := afterRuntimeLeafLstat
	afterRuntimeLeafLstat = func(relative string) {
		if filepath.ToSlash(relative) != "skills/github/SKILL.md" {
			return
		}
		afterRuntimeLeafLstat = func(string) {}
		if err := os.Remove(skill); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, skill); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterRuntimeLeafLstat = original })
	_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if !errors.Is(err, ErrBundledSkillsCatalogUnverified) {
		t.Fatalf("err=%v, want HERMES_BUNDLED_SKILLS_CATALOG_UNVERIFIED", err)
	}
}

func TestVerifyRuntimeContract_RejectsInstallRootSwapAfterOpen(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	replacementParent := testutil.TempDir(t)
	original := afterVerifiedInstallRootOpen
	var swapErr error
	afterVerifiedInstallRootOpen = func() {
		afterVerifiedInstallRootOpen = func() {}
		if err := os.Rename(root, filepath.Join(replacementParent, "original")); err != nil {
			swapErr = err
			return
		}
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterVerifiedInstallRootOpen = original })
	_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if swapErr != nil {
		if !errors.Is(swapErr, os.ErrPermission) {
			t.Fatalf("root replacement failed unexpectedly: %v", swapErr)
		}
		if err != nil {
			t.Fatalf("contract failed after the OS blocked replacement: %v", err)
		}
		return
	}
	if !errors.Is(err, ErrConfigSchemaUnsupported) {
		t.Fatalf("err=%v, want HERMES_CONFIG_SCHEMA_UNSUPPORTED", err)
	}
}

func TestVerifyRuntimeContract_RejectsUnprovenConfigSchema(t *testing.T) {
	cases := map[string]string{
		"missing-key":          "DEFAULT_CONFIG = {\n    \"default_model\": \"x\",\n}\n",
		"bare-assignment":      "_config_version = 34\n",
		"duplicate-numeric":    "DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n    \"_config_version\": 37,\n}\n",
		"duplicate-string":     "DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n    \"_config_version\": \"37\",\n}\n",
		"duplicate-malformed":  "DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n    \"_config_version\": unknown,\n}\n",
		"two-mappings":         runtimeConfigSchema34 + "DEFAULT_CONFIG = {\n    \"other\": 1,\n}\n",
		"string":               "DEFAULT_CONFIG = {\n    \"_config_version\": \"34\",\n}\n",
		"too-low":              "DEFAULT_CONFIG = {\n    \"_config_version\": 33,\n}\n",
		"too-high":             "DEFAULT_CONFIG = {\n    \"_config_version\": 38,\n}\n",
		"leading-zero":         "DEFAULT_CONFIG = {\n    \"_config_version\": 034,\n}\n",
		"expression":           "DEFAULT_CONFIG = {\n    \"_config_version\": 34 + 3,\n}\n",
		"semicolon-suffix":     "DEFAULT_CONFIG = {\n    \"_config_version\": 34;\n}\n",
		"class-local":          "class Defaults:\n    DEFAULT_CONFIG = {\n        \"_config_version\": 34,\n    }\n",
		"attribute":            "settings.DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n}\n",
		"triple-quote-decoy":   "DEFAULT_CONFIG = {\n    \"other\": '''\n    \"_config_version\": 34,\n    ''',\n}\n",
		"comment-decoy":        "# DEFAULT_CONFIG = {\n# \"_config_version\": 34,\n# }\n",
		"unterminated-mapping": "DEFAULT_CONFIG = {\n    \"_config_version\": 34,\n",
		"unterminated-string":  runtimeConfigSchema34 + "BROKEN = '''not closed\n",
		"invalid-utf8":         runtimeConfigSchema34 + "\xff",
		"oversized":            strings.Repeat("# padding\n", (512<<10)/10+1),
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if name == "oversized" {
				config += runtimeConfigSchema34
			}
			root, executable := writeRuntimeFixture(t, config, []string{"github"})
			before := fixtureHash(t, filepath.Join(root, "hermes_cli", "config_defaults.py"))
			_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
			if !errors.Is(err, ErrConfigSchemaUnsupported) {
				t.Fatalf("err=%v, want HERMES_CONFIG_SCHEMA_UNSUPPORTED", err)
			}
			if after := fixtureHash(t, filepath.Join(root, "hermes_cli", "config_defaults.py")); after != before {
				t.Fatal("schema fixture was mutated")
			}
		})
	}
}

func TestVerifyRuntimeContract_RequiresCurrentCapabilities(t *testing.T) {
	_, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	_, err := VerifyRuntimeContract(context.Background(), executable, func(_ context.Context, _ string, args []string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--version":
			return runtimeOutput(executable, "0.20.1"), nil
		case "profile create --help":
			return []byte("Usage: hermes profile create [--no-skills]\n"), nil
		case "skills opt-in --help":
			return []byte("Usage: hermes skills opt-in [--sync]\n"), nil
		default:
			return []byte("Usage: hermes profile create\n"), nil
		}
	})
	if !errors.Is(err, ErrExecutableUnverified) {
		t.Fatalf("err=%v, want HERMES_EXECUTABLE_UNVERIFIED", err)
	}
}

func TestInventoryBundledSkills_RejectsRedirectedOrDuplicateInventory(t *testing.T) {
	for _, fixture := range []string{"symlink", "duplicate", "oversize"} {
		t.Run(fixture, func(t *testing.T) {
			root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
			switch fixture {
			case "symlink":
				target := filepath.Join(testutil.TempDir(t), "other")
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(root, "skills", "redirected")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			case "duplicate":
				writeBundledSkill(t, root, "duplicate", "github")
			case "oversize":
				path := filepath.Join(root, "skills", "github", "SKILL.md")
				if err := os.WriteFile(path, []byte("---\nname: github\n---\n"+strings.Repeat("x", 16<<20)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
			if !errors.Is(err, ErrBundledSkillsCatalogUnverified) {
				t.Fatalf("err=%v, want HERMES_BUNDLED_SKILLS_CATALOG_UNVERIFIED", err)
			}
		})
	}
}

func TestInventoryBundledSkills_AllowsSkillBodyBeyondFrontmatterLimit(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	path := filepath.Join(root, "skills", "github", "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: github\n---\n"+strings.Repeat("x", 64<<10)), 0o600); err != nil {
		t.Fatal(err)
	}
	contract, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if err != nil || !contract.HasBundledSkill("github") {
		t.Fatalf("VerifyRuntimeContract()=%#v,%v", contract, err)
	}
}

func TestInventoryBundledSkills_RejectsFrontmatterTerminatorPastLimit(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	path := filepath.Join(root, "skills", "github", "SKILL.md")
	frontmatter := "---\nname: github\nnotes: " + strings.Repeat("x", 64<<10) + "\n---\n"
	if err := os.WriteFile(path, []byte(frontmatter), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if !errors.Is(err, ErrBundledSkillsCatalogUnverified) {
		t.Fatalf("err=%v, want HERMES_BUNDLED_SKILLS_CATALOG_UNVERIFIED", err)
	}
}

func TestVerifyRuntimeContract_RejectsMismatchedBundledManifest(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	manifest := []byte(`{"skills":[{"name":"other","path":"skills/other/SKILL.md"}]}`)
	if err := os.WriteFile(filepath.Join(root, "skills", "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if !errors.Is(err, ErrBundledSkillsCatalogUnverified) {
		t.Fatalf("err=%v, want HERMES_BUNDLED_SKILLS_CATALOG_UNVERIFIED", err)
	}
}

func TestVerifyRuntimeContract_RejectsDuplicateBundledManifestEntry(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github", "software-development"})
	manifest := []byte(`{"skills":[{"name":"github","path":"skills/github/SKILL.md"},{"name":"github","path":"skills/github/SKILL.md"}]}`)
	if err := os.WriteFile(filepath.Join(root, "skills", "manifest.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if !errors.Is(err, ErrBundledSkillsCatalogUnverified) {
		t.Fatalf("err=%v, want HERMES_BUNDLED_SKILLS_CATALOG_UNVERIFIED", err)
	}
}

func TestParseBundledSkill_AcceptsPinnedNameFormsAndFallback(t *testing.T) {
	for name, fixture := range map[string]struct {
		data string
		want string
	}{
		"quoted":   {"---\n  name:\"github\"\n---\n", "github"},
		"unquoted": {"---\nname: github\n---\n", "github"},
		"fallback": {"---\ndescription: no explicit name\n---\n", "folder"},
	} {
		t.Run(name, func(t *testing.T) {
			skill, err := parseBundledSkill("skills/folder/SKILL.md", []byte(fixture.data))
			if err != nil || skill.Name != fixture.want {
				t.Fatalf("parseBundledSkill()=%#v,%v", skill, err)
			}
		})
	}
}

func TestVerifyRuntimeContract_ExcludesExactPinnedMetadataPaths(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	pinned := []string{
		".git", ".github", ".hub", ".archive", ".venv", "venv",
		"node_modules", "site-packages", "__pycache__", ".tox", ".nox",
		".pytest_cache", ".mypy_cache", ".ruff_cache",
	}
	for index, directory := range pinned {
		writeBundledSkill(t, root, filepath.Join(directory, "ignored-"+strconv.Itoa(index)), "ignored-"+strconv.Itoa(index))
	}
	writeBundledSkill(t, root, filepath.Join(".legitimate", "hidden-skill"), "hidden-skill")
	contract, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	if err != nil || !reflect.DeepEqual(contract.BundledSkills, []string{"github", "hidden-skill"}) {
		t.Fatalf("VerifyRuntimeContract()=%#v,%v", contract, err)
	}
}

func TestVerifyRuntimeContract_ExcludesSupportDirsOnlyBelowActualSkillRoot(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"primary"})
	for index, directory := range []string{"references", "templates", "assets", "scripts"} {
		writeBundledSkill(t, root, filepath.Join("primary", directory, "archived-"+strconv.Itoa(index)), "archived-"+strconv.Itoa(index))
		writeBundledSkill(t, root, filepath.Join(directory, "category-"+strconv.Itoa(index)), "category-"+strconv.Itoa(index))
	}
	writeBundledSkill(t, root, filepath.Join("primary", "docs", "references", "nested-legitimate"), "nested-legitimate")
	contract, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	want := []string{"category-0", "category-1", "category-2", "category-3", "nested-legitimate", "primary"}
	if err != nil || !reflect.DeepEqual(contract.BundledSkills, want) {
		t.Fatalf("VerifyRuntimeContract()=%#v,%v want=%q", contract, err, want)
	}
}

func TestVerifyRuntimeContract_RootSkillDoesNotPruneScriptsCategory(t *testing.T) {
	root, executable := writeRuntimeFixture(t, runtimeConfigSchema34, nil)
	writeBundledSkill(t, root, ".", "root-skill")
	writeBundledSkill(t, root, filepath.Join("scripts", "foo"), "foo")
	contract, err := VerifyRuntimeContract(context.Background(), executable, runtimeFixtureCapture(root))
	want := []string{"foo", "root-skill"}
	if err != nil || !reflect.DeepEqual(contract.BundledSkills, want) {
		t.Fatalf("VerifyRuntimeContract()=%#v,%v want=%q", contract, err, want)
	}
}

func TestOpenedInstallRoot_RejectsCancellationAfterTraversalBatch(t *testing.T) {
	rootPath, executable := writeRuntimeFixture(t, runtimeConfigSchema34, nil)
	if err := os.MkdirAll(filepath.Join(rootPath, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := openVerifiedInstallRoot(RuntimeInfo{InstallDir: rootPath, Executable: executable, Version: "0.20.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := afterBundledSkillsReadBatch
	batchRead := false
	afterBundledSkillsReadBatch = func(relative string) {
		if relative == "skills" && !batchRead {
			batchRead = true
			cancel()
		}
	}
	t.Cleanup(func() { afterBundledSkillsReadBatch = original })
	_, err = root.WalkBundledSkills(ctx, defaultBundledInventoryLimits)
	if !batchRead {
		t.Fatal("bundled traversal never read its first batch")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}

func TestOpenedInstallRoot_BoundsLiveDirectoryHandlesBeforeDirectoryLimit(t *testing.T) {
	rootPath, executable := writeRuntimeFixture(t, runtimeConfigSchema34, nil)
	skillsRoot := filepath.Join(rootPath, "skills")
	for index := 0; index <= defaultBundledInventoryLimits.MaxDirectories; index++ {
		if err := os.MkdirAll(filepath.Join(skillsRoot, "directory-"+strconv.Itoa(index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root, err := openVerifiedInstallRoot(RuntimeInfo{InstallDir: rootPath, Executable: executable, Version: "0.20.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	original := afterBundledDirectoryHandleChange
	live, maxLive := 0, 0
	afterBundledDirectoryHandleChange = func(delta int) {
		live += delta
		if live > maxLive {
			maxLive = live
		}
	}
	t.Cleanup(func() { afterBundledDirectoryHandleChange = original })
	if _, err := root.WalkBundledSkills(context.Background(), defaultBundledInventoryLimits); err == nil {
		t.Fatal("WalkBundledSkills() succeeded beyond the 256-directory limit")
	}
	if live != 0 {
		t.Fatalf("live traversal directory handles=%d want=0", live)
	}
	if maxLive > 2 {
		t.Fatalf("max live traversal directory handles=%d want<=2", maxLive)
	}
}

func TestOpenedInstallRoot_EnforcesBundledInventoryResourceLimits(t *testing.T) {
	for name, limits := range map[string]bundledInventoryLimits{
		"directories": {MaxDirectories: 0, MaxDepth: 8, MaxFiles: 4096, MaxBytes: 16 << 20, MaxFrontmatterBytes: 64 << 10},
		"depth":       {MaxDirectories: 256, MaxDepth: 0, MaxFiles: 4096, MaxBytes: 16 << 20, MaxFrontmatterBytes: 64 << 10},
		"files":       {MaxDirectories: 256, MaxDepth: 8, MaxFiles: 0, MaxBytes: 16 << 20, MaxFrontmatterBytes: 64 << 10},
		"bytes":       {MaxDirectories: 256, MaxDepth: 8, MaxFiles: 4096, MaxBytes: 1, MaxFrontmatterBytes: 64 << 10},
	} {
		t.Run(name, func(t *testing.T) {
			rootPath, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
			root, err := openVerifiedInstallRoot(RuntimeInfo{InstallDir: rootPath, Executable: executable, Version: "0.20.1"})
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if _, err := root.WalkBundledSkills(context.Background(), limits); err == nil {
				t.Fatal("WalkBundledSkills() succeeded beyond configured limit")
			}
		})
	}
}

func TestOpenedInstallRoot_RejectsIdentitySwap(t *testing.T) {
	rootPath, executable := writeRuntimeFixture(t, runtimeConfigSchema34, []string{"github"})
	info := RuntimeInfo{Executable: executable, InstallDir: rootPath, Version: "0.20.1"}
	root, err := openVerifiedInstallRoot(info)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	expected := root.Identity()
	replacement := testutil.TempDir(t)
	if err := os.Rename(rootPath, filepath.Join(replacement, "moved")); err != nil {
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("root replacement failed unexpectedly: %v", err)
		}
		if err := root.VerifyIdentity(expected); err != nil {
			t.Fatalf("identity verification failed after OS blocked replacement: %v", err)
		}
		return
	}
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.VerifyIdentity(expected); !errors.Is(err, ErrConfigSchemaUnsupported) {
		t.Fatalf("err=%v, want identity verification failure", err)
	}
}

func writeRuntimeFixture(t *testing.T, config string, skills []string) (string, string) {
	t.Helper()
	root := filepath.Join(testutil.TempDir(t), "install")
	executable := filepath.Join(root, "venv", "bin", "hermes")
	if runtime.GOOS == "windows" {
		executable = filepath.Join(root, "venv", "Scripts", "hermes.exe")
	}
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("exe"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "hermes_cli"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hermes_cli", "config_defaults.py"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, skill := range skills {
		writeBundledSkill(t, root, skill, skill)
	}
	return root, executable
}

func writeBundledSkill(t *testing.T, root, directory, name string) {
	t.Helper()
	path := filepath.Join(root, "skills", directory, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: "+name+"\n---\n# "+name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runtimeFixtureCapture(root string) executableCapture {
	return func(_ context.Context, executable string, args []string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--version":
			return runtimeOutput(executable, "0.20.1"), nil
		case "profile create --help":
			return []byte("Usage: hermes profile create [--no-alias]\n"), nil
		case "skills opt-in --help":
			return []byte("Usage: hermes skills opt-in [--sync]\n"), nil
		default:
			return nil, errors.New("unexpected probe")
		}
	}
}

func fixtureHash(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}
