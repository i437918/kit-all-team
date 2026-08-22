package gitx

import (
	"context"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

type fakeRunner struct {
	commands []Command
	results  []Result
	errors   []error
	err      error
}

func (f *fakeRunner) Run(_ context.Context, command Command) (Result, error) {
	f.commands = append(f.commands, command)
	materializeFakeInit(command)
	if f.err != nil {
		return Result{}, f.err
	}
	result := Result{}
	if len(f.results) > 0 {
		result = f.results[0]
		f.results = f.results[1:]
	}
	if len(f.errors) > 0 {
		err := f.errors[0]
		f.errors = f.errors[1:]
		return result, err
	}
	return result, nil
}

func TestCloneContent_InitializesExistingManagedRootAndUsesSelectedBranch(t *testing.T) {
	runner := &fakeRunner{}
	repo := NewRepository(runner)
	destination := testutil.TempDir(t)
	auth := Credentials{AskPassPath: "C:/safe/askpass.exe", Username: "teamkit-user", Token: "teamkit-secret-canary", CAFile: "C:/safe/company-ca.pem"}
	if err := repo.CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", destination, auth); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 8 {
		t.Fatalf("commands = %d; want 8", len(runner.commands))
	}
	wantPrefix := [][]string{
		{"-C", destination, "init"},
	}
	for i := range wantPrefix {
		if !sameStrings(runner.commands[i].Args, wantPrefix[i]) {
			t.Fatalf("command %d args = %#v; want %#v", i, runner.commands[i].Args, wantPrefix[i])
		}
	}
	if !sameStrings(runner.commands[3].Args, []string{"-C", destination, "config", "remote.origin.url", "https://git.example/content.git"}) ||
		!sameStrings(runner.commands[4].Args, []string{"-C", destination, "config", "remote.origin.fetch", "+refs/heads/content-alpha:refs/remotes/origin/content-alpha"}) {
		t.Fatalf("remote configuration commands=%#v", runner.commands[3:5])
	}
	for index, command := range runner.commands {
		if index == 5 {
			if !hasEnvironment(command.Env, "GIT_ASKPASS=C:/safe/askpass.exe") ||
				!hasEnvironment(command.Env, "TEAMKIT_GIT_USERNAME=teamkit-user") ||
				!hasEnvironment(command.Env, "TEAMKIT_GIT_TOKEN=teamkit-secret-canary") ||
				!hasEnvironment(command.Env, "GIT_SSL_CAINFO=C:/safe/company-ca.pem") {
				t.Fatalf("credential environment = %#v", command.Env)
			}
		} else if strings.Contains(strings.Join(command.Env, "\n"), "teamkit-secret-canary") {
			t.Fatalf("credential reached local command %d: %#v", index, command)
		}
	}
	assertSafeCommands(t, runner.commands, "teamkit-secret-canary")
}

func TestCloneContent_CommandSequenceIsSafeToRetryAfterPartialInitialization(t *testing.T) {
	runner := &fakeRunner{}
	repo := NewRepository(runner)
	destination := testutil.TempDir(t)
	writeContentOwnershipResidue(t, destination)
	for range 2 {
		if err := repo.CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", destination, Credentials{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(runner.commands) != 15 {
		t.Fatalf("commands = %d; want one init followed by two identical seven-command resumes", len(runner.commands))
	}
	for i := 0; i < 7; i++ {
		if !sameStrings(runner.commands[i+1].Args, runner.commands[i+8].Args) {
			t.Fatalf("retry command %d differs: %#v vs %#v", i, runner.commands[i+1].Args, runner.commands[i+8].Args)
		}
	}
}

func TestCloneContent_RejectsForeignUntrackedResidueBeforeGitInit(t *testing.T) {
	for _, test := range []struct {
		name string
		path func(string) string
	}{
		{name: "root notes", path: func(root string) string { return filepath.Join(root, "notes.txt") }},
		{name: "metadata notes", path: func(root string) string { return filepath.Join(root, ".teamkit", "notes.txt") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			destination := testutil.TempDir(t)
			foreign := test.path(destination)
			if err := os.MkdirAll(filepath.Dir(foreign), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			runner := &fakeRunner{}
			err := NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", destination, Credentials{})
			if ErrorCode(err) != "GIT_RESIDUE_UNSAFE" || len(runner.commands) != 0 {
				t.Fatalf("CloneContent error=%v commands=%#v, want pre-mutation GIT_RESIDUE_UNSAFE", err, runner.commands)
			}
			data, readErr := os.ReadFile(foreign)
			if readErr != nil || string(data) != "keep" {
				t.Fatalf("foreign residue=%q, %v; want unchanged", data, readErr)
			}
		})
	}
}

func TestCloneContent_AllowsExactReceiptOwnedUnbornResidue(t *testing.T) {
	destination := testutil.TempDir(t)
	writeContentOwnershipResidue(t, destination)
	if err := os.WriteFile(filepath.Join(destination, ".teamkit", "operation.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: []Result{{}, {Stdout: "# branch.oid (initial)\n# branch.head master\n"}}}
	if err := NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", destination, Credentials{}); err != nil {
		t.Fatalf("CloneContent receipt-owned resume: %v", err)
	}
	if slices.Contains(runner.commands[0].Args, "init") {
		t.Fatalf("receipt-owned partial Git state was reinitialized: %#v", runner.commands[0])
	}
}

func TestCloneContent_RejectsUnownedPartialGitState(t *testing.T) {
	destination := testutil.TempDir(t)
	if err := os.Mkdir(filepath.Join(destination, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	err := NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", destination, Credentials{})
	if ErrorCode(err) != "GIT_RESIDUE_UNSAFE" || len(runner.commands) != 0 {
		t.Fatalf("CloneContent error=%v commands=%#v, want unowned partial state rejected before Git", err, runner.commands)
	}
}

func TestCloneContent_RejectsCommittedForeignBranchWithoutRepointingOrigin(t *testing.T) {
	runner := &fakeRunner{results: []Result{{}, {}, {Stdout: "# branch.oid 0123456789abcdef0123456789abcdef01234567\n# branch.head feature\n"}}}
	destination := testutil.TempDir(t)
	err := NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", destination, Credentials{})
	if ErrorCode(err) != "GIT_BRANCH_MISMATCH" {
		t.Fatalf("error=%v commands=%#v", err, runner.commands)
	}
	for _, command := range runner.commands {
		if strings.Contains(strings.Join(command.Args, " "), "remote.origin.url") {
			t.Fatalf("foreign repository was repointed: %#v", command)
		}
	}
}

func TestEnsureManagedExclude_HidesRetryStateFromContentWorktree(t *testing.T) {
	destination := testutil.TempDir(t)
	if err := ensureManagedExclude(destination); err != nil {
		t.Fatalf("ensureManagedExclude: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destination, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{".env", "/db/", "/.teamkit/"} {
		if !strings.Contains(text, required+"\n") {
			t.Fatalf("exclude=%q missing %q", text, required)
		}
	}
}

func TestSyncPinned_ClonesAbsentCheckoutAtExactCommit(t *testing.T) {
	pinned := "0123456789abcdef0123456789abcdef01234567"
	runner := &fakeRunner{results: []Result{{}, {Stdout: pinned + "\n"}, {}}}
	destination := filepath.Join(testutil.TempDir(t), "toolchain")
	auth := Credentials{CAFile: "C:/safe/company-ca.pem"}
	if err := NewRepository(runner).SyncPinned(context.Background(), "https://git.example/toolchain.git", pinned, destination, auth); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 3 || !slices.Contains(runner.commands[0].Args, "clone") ||
		!slices.Contains(runner.commands[0].Args, "https://git.example/toolchain.git") ||
		!slices.Contains(runner.commands[1].Args, "rev-parse") || !slices.Contains(runner.commands[2].Args, "checkout") {
		t.Fatalf("commands=%#v", runner.commands)
	}
	if !hasEnvironment(runner.commands[0].Env, "GIT_SSL_CAINFO=C:/safe/company-ca.pem") {
		t.Fatalf("pinned network command omitted application-local CA: %#v", runner.commands[0])
	}
	for _, command := range runner.commands[1:] {
		if hasEnvironment(command.Env, "GIT_SSL_CAINFO=C:/safe/company-ca.pem") {
			t.Fatalf("CA reached local command: %#v", command)
		}
	}
	assertSafeCommands(t, runner.commands, "teamkit-secret-canary")
}

func TestSyncPinned_CleanExistingFetchesThenDetachesAtVerifiedCommit(t *testing.T) {
	pinned := "89abcdef0123456789abcdef0123456789abcdef"
	destination := testutil.TempDir(t)
	if err := os.Mkdir(filepath.Join(destination, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	remote := "https://git.example/toolchain.git"
	runner := &fakeRunner{results: []Result{{}, {Stdout: remote + "\n"}, {}, {}, {Stdout: pinned + "\n"}, {}}}
	if err := NewRepository(runner).SyncPinned(context.Background(), remote, pinned, destination, Credentials{}); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 6 || !slices.Contains(runner.commands[1].Args, "remote.origin.url") ||
		!slices.Contains(runner.commands[3].Args, "fetch") || !slices.Contains(runner.commands[4].Args, "rev-parse") ||
		!slices.Contains(runner.commands[5].Args, "checkout") {
		t.Fatalf("commands=%#v", runner.commands)
	}
}

func TestSyncPinned_RejectsUnsafeRemoteOrPinAndMismatchedVerification(t *testing.T) {
	pinned := "0123456789abcdef0123456789abcdef01234567"
	destination := filepath.Join(testutil.TempDir(t), "toolchain")
	for _, invalid := range []string{"", "../" + pinned, "0123456789abcdef0123456789abcdef0123456g", "ABCDEF0123456789abcdef0123456789abcdef"} {
		runner := &fakeRunner{}
		err := NewRepository(runner).SyncPinned(context.Background(), "https://git.example/toolchain.git", invalid, destination, Credentials{})
		if ErrorCode(err) != "GIT_PIN_INVALID" || len(runner.commands) != 0 {
			t.Fatalf("pin %q error = %v, commands = %#v", invalid, err, runner.commands)
		}
	}
	err := NewRepository(&fakeRunner{}).SyncPinned(context.Background(), "http://git.example/toolchain.git", pinned, destination, Credentials{})
	if ErrorCode(err) != "GIT_URL_UNSAFE" {
		t.Fatalf("unsafe remote error = %v, code = %q", err, ErrorCode(err))
	}
	existing := testutil.TempDir(t)
	if err := os.Mkdir(filepath.Join(existing, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	remote := "https://git.example/toolchain.git"
	runner := &fakeRunner{results: []Result{{}, {Stdout: remote + "\n"}, {}, {}, {Stdout: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"}}}
	err = NewRepository(runner).SyncPinned(context.Background(), remote, pinned, existing, Credentials{})
	if ErrorCode(err) != "GIT_PIN_UNVERIFIED" || len(runner.commands) != 5 {
		t.Fatalf("mismatched verification error = %v, commands = %#v", err, runner.commands)
	}
}

func TestSyncPinned_RejectsLocalChanges(t *testing.T) {
	destination := testutil.TempDir(t)
	remote := "https://git.example/toolchain.git"
	runner := &fakeRunner{results: []Result{{}, {Stdout: remote + "\n"}, {Stdout: " M tracked.txt\n"}}}
	err := NewRepository(runner).SyncPinned(context.Background(), remote, "0123456789abcdef0123456789abcdef01234567", destination, Credentials{})
	if ErrorCode(err) != "LOCAL_CHANGES_DETECTED" || len(runner.commands) != 3 {
		t.Fatalf("error = %v, commands = %#v", err, runner.commands)
	}
}

func TestSyncPinned_RejectsMismatchedExistingOriginBeforeFetch(t *testing.T) {
	destination := testutil.TempDir(t)
	runner := &fakeRunner{results: []Result{{}, {Stdout: "https://attacker.invalid/toolchain.git\n"}}}
	err := NewRepository(runner).SyncPinned(
		context.Background(), "https://git.example/toolchain.git",
		"0123456789abcdef0123456789abcdef01234567", destination, Credentials{},
	)
	if ErrorCode(err) != "GIT_REMOTE_MISMATCH" || len(runner.commands) != 2 {
		t.Fatalf("error=%v commands=%#v", err, runner.commands)
	}
	if slices.Contains(runner.commands[1].Args, "fetch") {
		t.Fatalf("mismatched origin was fetched: %#v", runner.commands[1])
	}
}

func TestCloneDatabase_UsesDevelopInWorkspaceDB(t *testing.T) {
	runner := &fakeRunner{}
	repo := NewRepository(runner)
	if err := repo.CloneDatabase(context.Background(), "https://git.example/db.git", testutil.TempDir(t), Credentials{}); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 7 {
		t.Fatalf("commands=%#v", runner.commands)
	}
	fetch := strings.Join(runner.commands[5].Args, " ")
	if !strings.Contains(fetch, "https://git.example/db.git") || !strings.Contains(fetch, "refs/heads/develop:refs/remotes/origin/develop") {
		t.Fatalf("fetch=%#v", runner.commands[5])
	}
}

func TestCloneDatabase_RejectsForeignResidueBeforeGitInit(t *testing.T) {
	workspaceRoot := testutil.TempDir(t)
	foreign := filepath.Join(workspaceRoot, "db", "data.bin")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	err := NewRepository(runner).CloneDatabase(context.Background(), "https://git.example/db.git", workspaceRoot, Credentials{})
	if ErrorCode(err) != "GIT_RESIDUE_UNSAFE" || len(runner.commands) != 0 {
		t.Fatalf("CloneDatabase error=%v commands=%#v, want pre-mutation GIT_RESIDUE_UNSAFE", err, runner.commands)
	}
	data, readErr := os.ReadFile(foreign)
	if readErr != nil || string(data) != "keep" {
		t.Fatalf("foreign residue=%q, %v; want unchanged", data, readErr)
	}
}

func TestCloneDatabase_AllowsReceiptOwnedPartialGitState(t *testing.T) {
	workspaceRoot := testutil.TempDir(t)
	if err := os.MkdirAll(filepath.Join(workspaceRoot, ".teamkit"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, ".teamkit", "operation.json"), []byte("receipt-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "db", ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: []Result{{}, {Stdout: "# branch.oid (initial)\n# branch.head master\n"}}}
	if err := NewRepository(runner).CloneDatabase(context.Background(), "https://git.example/db.git", workspaceRoot, Credentials{}); err != nil {
		t.Fatalf("CloneDatabase receipt-owned resume: %v", err)
	}
	if slices.Contains(runner.commands[0].Args, "init") {
		t.Fatalf("receipt-owned partial Git state was reinitialized: %#v", runner.commands[0])
	}
}

func TestUpdateDatabase_FetchesThenFastForwardsOnlyWhenClean(t *testing.T) {
	remote := "https://git.example/db.git"
	database := testutil.TempDir(t)
	if err := os.Mkdir(filepath.Join(database, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{results: []Result{{}, {Stdout: remote + "\n"}, {Stdout: "develop\n"}, {}, {}, {}, {}}}
	repo := NewRepository(runner)
	if err := repo.UpdateDatabase(context.Background(), database, remote, Credentials{}); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 7 || !strings.Contains(strings.Join(runner.commands[4].Args, " "), remote) {
		t.Fatalf("commands=%#v", runner.commands)
	}
}

func TestUpdateDatabase_RejectsLocalChanges(t *testing.T) {
	remote := "https://git.example/db.git"
	runner := &fakeRunner{results: []Result{{}, {Stdout: remote + "\n"}, {Stdout: "develop\n"}, {Stdout: " M file.txt\n"}}}
	err := NewRepository(runner).UpdateDatabase(context.Background(), "C:/kit/db", remote, Credentials{})
	if ErrorCode(err) != "LOCAL_CHANGES_DETECTED" {
		t.Fatalf("error = %v, code = %q; want LOCAL_CHANGES_DETECTED", err, ErrorCode(err))
	}
	if len(runner.commands) != 4 {
		t.Fatalf("commands = %#v; want only local preflight", runner.commands)
	}
}

func TestUpdateContent_FetchesThenFastForwardsSelectedBranchOnlyWhenClean(t *testing.T) {
	remote := "https://git.example/content.git"
	destination := safeGitWorktree(t)
	auth := Credentials{AskPassPath: "C:/safe/askpass.exe", Username: "teamkit-user", Token: "TEAMKIT_UPDATE_CANARY", CAFile: "C:/safe/ca.pem"}
	runner := &fakeRunner{results: []Result{
		{}, {Stdout: remote + "\n"},
		{Stdout: "content-alpha\n"},
		{}, {}, {},
	}}
	repo := NewRepository(runner)
	if err := repo.UpdateContent(context.Background(), destination, remote, "content-alpha", auth); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 8 {
		t.Fatalf("commands=%#v", runner.commands)
	}
	for index, command := range runner.commands {
		if !slices.Contains(command.Args, destination) {
			t.Fatalf("command %d escaped isolated fixture: %#v", index, command.Args)
		}
		environment := strings.Join(command.Env, "\n")
		if index == 4 {
			if !strings.Contains(environment, "TEAMKIT_GIT_TOKEN=TEAMKIT_UPDATE_CANARY") ||
				!strings.Contains(strings.Join(command.Args, " "), remote) ||
				!strings.Contains(strings.Join(command.Args, " "), "credential.helper=") ||
				!strings.Contains(strings.Join(command.Args, " "), "http.followRedirects=false") {
				t.Fatalf("authenticated fetch is not isolated: %#v", command)
			}
		} else if strings.Contains(environment, "TEAMKIT_UPDATE_CANARY") || strings.Contains(environment, "teamkit-user") {
			t.Fatalf("credential reached local command %d: %#v", index, command)
		}
	}
	assertSafeCommands(t, runner.commands, "TEAMKIT_UPDATE_CANARY")
}

func TestUpdateContent_RejectsMismatchedOriginBeforeCredentials(t *testing.T) {
	destination := safeGitWorktree(t)
	runner := &fakeRunner{results: []Result{{}, {Stdout: "https://attacker.invalid/repository.git\n"}}}
	err := NewRepository(runner).UpdateContent(
		context.Background(), destination, "https://git.example/content.git", "content-alpha",
		Credentials{Username: "user", Token: "TEAMKIT_REMOTE_MISMATCH_CANARY"},
	)
	if ErrorCode(err) != "GIT_REMOTE_MISMATCH" || len(runner.commands) != 2 {
		t.Fatalf("error=%v commands=%#v", err, runner.commands)
	}
	if strings.Contains(strings.Join(runner.commands[1].Env, "\n"), "TEAMKIT_REMOTE_MISMATCH_CANARY") {
		t.Fatalf("credential reached remote preflight: %#v", runner.commands[1])
	}
}

func TestUpdateContent_RejectsBranchAheadOfFetchedCatalogRef(t *testing.T) {
	remote := "https://git.example/content.git"
	destination := safeGitWorktree(t)
	runner := &fakeRunner{
		results: []Result{{}, {Stdout: remote + "\n"}, {Stdout: "content-alpha\n"}, {}, {}, {}, {}},
		errors:  []error{nil, nil, nil, nil, nil, nil, errors.New("not an ancestor")},
	}
	err := NewRepository(runner).UpdateContent(context.Background(), destination, remote, "content-alpha", Credentials{})
	if ErrorCode(err) != "GIT_NON_FAST_FORWARD" {
		t.Fatalf("error=%v commands=%#v", err, runner.commands)
	}
	for _, command := range runner.commands {
		if slices.Contains(command.Args, "merge") {
			t.Fatalf("ahead branch was merged: %#v", command)
		}
	}
}

func TestUpdateContent_RejectsInvalidBranchAndLocalChanges(t *testing.T) {
	remote := "https://git.example/content.git"
	destination := safeGitWorktree(t)
	err := NewRepository(&fakeRunner{}).UpdateContent(context.Background(), destination, remote, "develop", Credentials{})
	if ErrorCode(err) != "CONTENT_BRANCH_INVALID" {
		t.Fatalf("invalid branch error = %v, code = %q", err, ErrorCode(err))
	}
	runner := &fakeRunner{results: []Result{{}, {Stdout: remote + "\n"}, {Stdout: "content-alpha\n"}, {Stdout: " M content.md\n"}}}
	err = NewRepository(runner).UpdateContent(context.Background(), destination, remote, "content-alpha", Credentials{})
	if ErrorCode(err) != "LOCAL_CHANGES_DETECTED" || len(runner.commands) != 4 {
		t.Fatalf("local changes error = %v, commands = %#v", err, runner.commands)
	}
}

func TestObserveBranch_ContentAllowsOnlyExactUnstagedTeamKitGitignoreDelta(t *testing.T) {
	for _, test := range []struct {
		name      string
		mutate    func(*testing.T, string)
		wantError bool
	}{
		{
			name: "exact delta",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := workspace.EnsureGitignore(filepath.Join(root, ".gitignore")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "other gitignore rule",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := workspace.EnsureGitignore(filepath.Join(root, ".gitignore")); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(filepath.Join(root, ".gitignore"), os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteString("foreign-rule\n"); err != nil {
					file.Close()
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			},
			wantError: true,
		},
		{
			name: "indexed delta",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := workspace.EnsureGitignore(filepath.Join(root, ".gitignore")); err != nil {
					t.Fatal(err)
				}
				runGit(t, root, "add", ".gitignore")
			},
			wantError: true,
		},
		{
			name: "other tracked change",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := workspace.EnsureGitignore(filepath.Join(root, ".gitignore")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("changed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := committedContentFixture(t, true)
			test.mutate(t, root)
			err := NewRepository(SystemRunner{}).ObserveBranch(context.Background(), root, "https://git.example/content.git", "content-alpha")
			if test.wantError && ErrorCode(err) != "LOCAL_CHANGES_DETECTED" {
				t.Fatalf("ObserveBranch error=%v, want LOCAL_CHANGES_DETECTED", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("ObserveBranch exact Team Kit delta: %v", err)
			}
		})
	}
}

func TestObserveBranch_ContentAllowsExactIgnoredUntrackedTeamKitGitignore(t *testing.T) {
	root := committedContentFixture(t, false)
	if err := workspace.EnsureLocalExclude(filepath.Join(root, ".git", "info", "exclude")); err != nil {
		t.Fatal(err)
	}
	if err := workspace.EnsureGitignore(filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if err := NewRepository(SystemRunner{}).ObserveBranch(context.Background(), root, "https://git.example/content.git", "content-alpha"); err != nil {
		t.Fatalf("ObserveBranch ignored generated .gitignore: %v", err)
	}
}

func TestObserveBranch_ContentRejectsTamperedIgnoredUntrackedGitignore(t *testing.T) {
	root := committedContentFixture(t, false)
	if err := workspace.EnsureLocalExclude(filepath.Join(root, ".git", "info", "exclude")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".env\n/db/\n/.teamkit/\nforeign-rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewRepository(SystemRunner{}).ObserveBranch(context.Background(), root, "https://git.example/content.git", "content-alpha")
	if ErrorCode(err) != "LOCAL_CHANGES_DETECTED" {
		t.Fatalf("ObserveBranch error=%v, want LOCAL_CHANGES_DETECTED", err)
	}
}

func TestUpdateContent_RejectsRedirectedGitMetadataBeforeFetch(t *testing.T) {
	for _, component := range []string{"objects", "refs", "logs", "info", "hooks"} {
		t.Run(component, func(t *testing.T) {
			directory := testutil.TempDir(t)
			external := testutil.TempDir(t)
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(directory, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			redirected := filepath.Join(directory, ".git", component)
			if err := os.Symlink(external, redirected); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}

			runner := branchRunner("https://git.example/content.git", "content-alpha", func(operation string) error {
				if operation == "fetch" {
					return os.WriteFile(filepath.Join(redirected, "sentinel"), []byte("escaped"), 0o600)
				}
				return nil
			})
			err := NewRepository(runner).UpdateContent(context.Background(), directory, "https://git.example/content.git", "content-alpha", Credentials{})
			if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, pathsafe.ErrUnsafe) {
				t.Fatalf("UpdateContent error=%v, want GIT_METADATA_UNSAFE wrapping pathsafe.ErrUnsafe", err)
			}
			assertSentinel(t, sentinel)
		})
	}
}

func TestUpdateContent_RejectsRedirectedGitMetadataFilesBeforeFetch(t *testing.T) {
	for _, component := range []string{"config", "HEAD", "index"} {
		t.Run(component, func(t *testing.T) {
			directory := testutil.TempDir(t)
			external := testutil.TempDir(t)
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(directory, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			redirected := filepath.Join(directory, ".git", component)
			if err := os.Symlink(sentinel, redirected); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}

			runner := branchRunner("https://git.example/content.git", "content-alpha", func(operation string) error {
				if operation == "fetch" {
					return os.WriteFile(redirected, []byte("escaped"), 0o600)
				}
				return nil
			})
			err := NewRepository(runner).UpdateContent(context.Background(), directory, "https://git.example/content.git", "content-alpha", Credentials{})
			if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, pathsafe.ErrUnsafe) {
				t.Fatalf("UpdateContent error=%v, want GIT_METADATA_UNSAFE wrapping pathsafe.ErrUnsafe", err)
			}
			assertSentinel(t, sentinel)
		})
	}
}

func TestUpdateContent_RejectsRedirectedGitRootFilesBeforeFetch(t *testing.T) {
	for _, component := range []string{"FETCH_HEAD", "packed-refs", "ORIG_HEAD", "config.worktree"} {
		t.Run(component, func(t *testing.T) {
			directory := testutil.TempDir(t)
			external := testutil.TempDir(t)
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(directory, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			redirected := filepath.Join(directory, ".git", component)
			if err := os.Symlink(sentinel, redirected); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}

			fetchCalled := false
			runner := branchRunner("https://git.example/content.git", "content-alpha", func(operation string) error {
				if operation == "fetch" {
					fetchCalled = true
				}
				return nil
			})
			err := NewRepository(runner).UpdateContent(context.Background(), directory, "https://git.example/content.git", "content-alpha", Credentials{})
			if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, pathsafe.ErrUnsafe) {
				t.Fatalf("UpdateContent error=%v, want GIT_METADATA_UNSAFE wrapping pathsafe.ErrUnsafe", err)
			}
			if fetchCalled {
				t.Fatal("fetch ran after redirected Git root file was detected")
			}
			assertSentinel(t, sentinel)
		})
	}
}

func TestUpdateContent_RejectsRedirectedGitRootFileIntroducedBeforeMerge(t *testing.T) {
	directory := testutil.TempDir(t)
	external := testutil.TempDir(t)
	requireSymlink(t, external)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	redirected := filepath.Join(directory, ".git", "FETCH_HEAD")
	mergeCalled := false
	runner := branchRunner("https://git.example/content.git", "content-alpha", func(operation string) error {
		switch operation {
		case "fetch":
			return os.Symlink(sentinel, redirected)
		case "merge":
			mergeCalled = true
		}
		return nil
	})
	err := NewRepository(runner).UpdateContent(context.Background(), directory, "https://git.example/content.git", "content-alpha", Credentials{})
	if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("UpdateContent error=%v, want GIT_METADATA_UNSAFE wrapping pathsafe.ErrUnsafe", err)
	}
	if mergeCalled {
		t.Fatal("merge ran after redirected Git root file was introduced")
	}
	assertSentinel(t, sentinel)
}

func TestCloneContent_RevalidatesGitMetadataBeforeEachConfigMutation(t *testing.T) {
	directory := testutil.TempDir(t)
	external := testutil.TempDir(t)
	requireSymlink(t, external)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	redirected := filepath.Join(directory, ".git", "FETCH_HEAD")
	secondConfigCalled := false
	runner := commandRunnerFunc(func(command Command) (Result, error) {
		materializeFakeInit(command)
		joined := strings.Join(command.Args, " ")
		switch {
		case strings.Contains(joined, "config remote.origin.url"):
			return Result{}, os.Symlink(sentinel, redirected)
		case strings.Contains(joined, "config remote.origin.fetch"):
			secondConfigCalled = true
		}
		return Result{}, nil
	})

	err := NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", directory, Credentials{})
	if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("CloneContent error=%v, want GIT_METADATA_UNSAFE wrapping pathsafe.ErrUnsafe", err)
	}
	if secondConfigCalled {
		t.Fatal("second config mutation ran without revalidating Git metadata")
	}
	assertSentinel(t, sentinel)
}

func TestUpdateContent_RejectsNestedRedirectIntroducedBeforeMerge(t *testing.T) {
	directory := testutil.TempDir(t)
	external := testutil.TempDir(t)
	requireSymlink(t, external)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	objects := filepath.Join(directory, ".git", "objects")
	if err := os.MkdirAll(objects, 0o700); err != nil {
		t.Fatal(err)
	}
	redirected := filepath.Join(objects, "ab")
	runner := branchRunner("https://git.example/content.git", "content-alpha", func(operation string) error {
		switch operation {
		case "fetch":
			return os.Symlink(external, redirected)
		case "merge":
			return os.WriteFile(filepath.Join(redirected, "sentinel"), []byte("escaped"), 0o600)
		default:
			return nil
		}
	})
	err := NewRepository(runner).UpdateContent(context.Background(), directory, "https://git.example/content.git", "content-alpha", Credentials{})
	if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("UpdateContent error=%v, want GIT_METADATA_UNSAFE wrapping pathsafe.ErrUnsafe", err)
	}
	assertSentinel(t, sentinel)
}

func TestCloneContent_RejectsRedirectIntroducedBeforeCheckout(t *testing.T) {
	directory := testutil.TempDir(t)
	external := testutil.TempDir(t)
	requireSymlink(t, external)
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	redirected := filepath.Join(directory, ".git", "objects")
	runner := branchRunner("https://git.example/content.git", "content-alpha", func(operation string) error {
		switch operation {
		case "fetch":
			return os.Symlink(external, redirected)
		case "checkout":
			return os.WriteFile(filepath.Join(redirected, "sentinel"), []byte("escaped"), 0o600)
		default:
			return nil
		}
	})
	err := NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", directory, Credentials{})
	if ErrorCode(err) != "GIT_METADATA_UNSAFE" || !errors.Is(err, pathsafe.ErrUnsafe) {
		t.Fatalf("CloneContent error=%v, want GIT_METADATA_UNSAFE wrapping pathsafe.ErrUnsafe", err)
	}
	assertSentinel(t, sentinel)
}

func TestRepository_SanitizesRunnerErrorsAndRejectsCredentialURLs(t *testing.T) {
	destination := testutil.TempDir(t)
	runner := &fakeRunner{err: errors.New("authentication failed: teamkit-secret-canary for teamkit-user-canary via C:/safe/company-ca.pem")}
	err := NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", destination, Credentials{Username: "teamkit-user-canary", Token: "teamkit-secret-canary", CAFile: "C:/safe/company-ca.pem"})
	if err == nil || strings.Contains(err.Error(), "teamkit-secret-canary") || strings.Contains(err.Error(), "teamkit-user-canary") || strings.Contains(err.Error(), "C:/safe/company-ca.pem") {
		t.Fatalf("error leaked secret: %v", err)
	}
	err = NewRepository(&fakeRunner{}).CloneContent(context.Background(), "https://user:token@git.example/content.git", "content-alpha", destination, Credentials{})
	if ErrorCode(err) != "GIT_URL_UNSAFE" {
		t.Fatalf("credential URL error = %v, code = %q", err, ErrorCode(err))
	}
}

func TestRepository_PreservesBoundedSanitizedRunnerDiagnostics(t *testing.T) {
	auth := Credentials{
		Username: "teamkit-user-canary",
		Token:    "teamkit-user-canary-token-secret",
		CAFile:   "C:/private/teamkit-ca-canary.pem",
	}
	stderr := "fatal: TLS verification failed for " + auth.Username + " using " + auth.Token + " and " + auth.CAFile + "\n" + strings.Repeat("diagnostic ", 600)
	runner := &fakeRunner{
		results: []Result{{Stderr: stderr}},
		errors:  []error{errors.New("exit status 128 for " + auth.Token)},
	}
	err := NewRepository(runner).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", testutil.TempDir(t), auth)
	message := err.Error()
	if ErrorCode(err) != "GIT_COMMAND_FAILED" || !strings.Contains(message, "exit status 128") || !strings.Contains(message, "fatal: TLS verification failed") {
		t.Fatalf("bounded diagnostic lost actionable detail: %v", err)
	}
	for _, canary := range []string{auth.Username, auth.Token, auth.CAFile} {
		if strings.Contains(message, canary) {
			t.Fatalf("diagnostic leaked %q: %v", canary, err)
		}
	}
	if !strings.Contains(message, "[REDACTED]") || len(message) > 2300 {
		t.Fatalf("diagnostic was not redacted and bounded: len=%d error=%v", len(message), err)
	}
}

func TestSafeRemoteURL_AcceptsHTTPSOnly(t *testing.T) {
	if err := safeRemoteURL("https://git.example/teamkit.git"); err != nil {
		t.Fatalf("HTTPS URL rejected: %v", err)
	}
	for _, raw := range []string{
		"http://git.example/teamkit.git",
		"ftp://git.example/teamkit.git",
		"file:///tmp/teamkit.git",
		"https://user:token@git.example/teamkit.git",
	} {
		if err := safeRemoteURL(raw); ErrorCode(err) != "GIT_URL_UNSAFE" {
			t.Fatalf("%q error = %v, code = %q; want GIT_URL_UNSAFE", raw, err, ErrorCode(err))
		}
	}
}

type commandRunnerFunc func(Command) (Result, error)

func (f commandRunnerFunc) Run(_ context.Context, command Command) (Result, error) {
	return f(command)
}

func branchRunner(remote, branch string, sideEffect func(string) error) Runner {
	return commandRunnerFunc(func(command Command) (Result, error) {
		materializeFakeInit(command)
		operation := ""
		for _, candidate := range []string{"fetch", "checkout", "merge", "merge-base"} {
			if slices.Contains(command.Args, candidate) {
				operation = candidate
				break
			}
		}
		if err := sideEffect(operation); err != nil {
			return Result{}, err
		}
		joined := strings.Join(command.Args, " ")
		switch {
		case strings.Contains(joined, "config --get remote.origin.url"):
			return Result{Stdout: remote + "\n"}, nil
		case strings.Contains(joined, "symbolic-ref"):
			return Result{Stdout: branch + "\n"}, nil
		default:
			return Result{}, nil
		}
	})
}

func materializeFakeInit(command Command) {
	if len(command.Args) == 3 && command.Args[0] == "-C" && command.Args[2] == "init" {
		_ = os.MkdirAll(filepath.Join(command.Args[1], ".git"), 0o700)
	}
	if slices.Contains(command.Args, "clone") && len(command.Args) > 0 {
		_ = os.MkdirAll(filepath.Join(command.Args[len(command.Args)-1], ".git"), 0o700)
	}
}

func safeGitWorktree(t *testing.T) string {
	t.Helper()
	worktree := testutil.TempDir(t)
	if err := os.Mkdir(filepath.Join(worktree, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return worktree
}

func assertSentinel(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "outside" {
		t.Fatalf("external sentinel=%q, %v; want outside", data, err)
	}
}

func committedContentFixture(t *testing.T, trackedGitignore bool) string {
	t.Helper()
	root := testutil.TempDir(t)
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "Team Kit Test")
	runGit(t, root, "config", "user.email", "teamkit@example.invalid")
	runGit(t, root, "checkout", "-b", "content-alpha")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	if trackedGitignore {
		if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("dist/\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", ".gitignore")
	}
	runGit(t, root, "commit", "-m", "fixture")
	runGit(t, root, "config", "remote.origin.url", "https://git.example/content.git")
	return root
}

func writeContentOwnershipResidue(t *testing.T, root string) {
	t.Helper()
	metadata := filepath.Join(root, ".teamkit")
	if err := os.MkdirAll(metadata, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"owner": "wms\n", "operation.json": "receipt-owned"} {
		if err := os.WriteFile(filepath.Join(metadata, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("PROJECT=wms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireSymlink(t *testing.T, target string) {
	t.Helper()
	probe := filepath.Join(testutil.TempDir(t), "probe")
	if err := os.Symlink(target, probe); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
}

func assertSafeCommands(t *testing.T, commands []Command, canary string) {
	t.Helper()
	for _, command := range commands {
		joined := strings.Join(command.Args, " ")
		for _, forbidden := range []string{"reset", "stash", "push", canary} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("unsafe command arguments %q contain %q", joined, forbidden)
			}
		}
	}
}

func hasEnvironment(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
