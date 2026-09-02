package gitx

import (
	"context"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestValidateFetchedContentTree_RejectsPortableReservedRootEntriesWithoutCredentials(t *testing.T) {
	for _, entry := range []string{
		".env", ".ENV", ".env/secret", ".env ",
		".teamkit", ".TeamKit", ".TEAMKIT/receipt", ".teamkit.",
		"db", "DB", "db/config", `DB\config`, "db.",
	} {
		t.Run(entry, func(t *testing.T) {
			destination := safeGitWorktree(t)
			runner := &fakeRunner{results: []Result{{Stdout: entry + "\x00"}}}
			auth := Credentials{Username: "tree-user-canary", Token: "tree-token-canary", CAFile: "tree-ca-canary"}
			err := NewRepository(runner).validateFetchedContentTree(context.Background(), destination, "refs/remotes/origin/content-alpha", auth)
			if ErrorCode(err) != "GIT_RESERVED_PATH_COLLISION" {
				t.Fatalf("entry %q error = %v; want GIT_RESERVED_PATH_COLLISION", entry, err)
			}
			if len(runner.commands) != 1 {
				t.Fatalf("commands = %#v; want one local tree inspection", runner.commands)
			}
			command := runner.commands[0]
			if !slices.Contains(command.Args, "ls-tree") || !slices.Contains(command.Args, "refs/remotes/origin/content-alpha") {
				t.Fatalf("tree inspection args = %#v", command.Args)
			}
			environment := strings.Join(command.Env, "\n")
			for _, secret := range []string{auth.Username, auth.Token, auth.CAFile, "GIT_ASKPASS="} {
				if strings.Contains(environment, secret) {
					t.Fatalf("local tree inspection received credential %q: %#v", secret, command.Env)
				}
			}
		})
	}
}

func TestCloneContent_ReservedFetchedTreeLeavesPrivateFileAndLocalRefUnchanged(t *testing.T) {
	destination := testutil.TempDir(t)
	runGit(t, destination, "init")
	writeContentOwnershipResidue(t, destination)
	privatePath := filepath.Join(destination, ".env")
	privateBefore := []byte("PROJECT=wms\nTOKEN=private-canary\n")
	if err := os.WriteFile(privatePath, privateBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	initialRef := runGitOutput(t, destination, "symbolic-ref", "HEAD")

	source := testutil.TempDir(t)
	runGit(t, source, "init")
	runGit(t, source, "config", "user.name", "Team Kit Tests")
	runGit(t, source, "config", "user.email", "teamkit-tests@example.invalid")
	runGit(t, source, "checkout", "-b", "content-alpha")
	reserved := filepath.Join(source, ".TeamKit", "payload.txt")
	if err := os.MkdirAll(filepath.Dir(reserved), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reserved, []byte("attacker"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", ".TeamKit/payload.txt")
	runGit(t, source, "commit", "-m", "reserved collision")
	runGit(t, destination, "fetch", source, "content-alpha:refs/remotes/origin/content-alpha")

	err := NewRepository(fetchNoopRunner{}).CloneContent(context.Background(), "https://git.example/content.git", "content-alpha", destination, Credentials{})
	if ErrorCode(err) != "GIT_RESERVED_PATH_COLLISION" {
		t.Fatalf("CloneContent error = %v; want GIT_RESERVED_PATH_COLLISION", err)
	}
	assertBytesUnchanged(t, privatePath, privateBefore)
	if got := runGitOutput(t, destination, "symbolic-ref", "HEAD"); got != initialRef {
		t.Fatalf("HEAD ref = %q; want unchanged %q", got, initialRef)
	}
	if output, showErr := gitCommand(destination, "show-ref", "--verify", "--quiet", "refs/heads/content-alpha").CombinedOutput(); showErr == nil {
		t.Fatalf("local content branch was created despite rejection: %s", output)
	}
}

func TestUpdateContent_ReservedFetchedTreeLeavesPrivateFileAndBranchRefUnchanged(t *testing.T) {
	destination := testutil.TempDir(t)
	runGit(t, destination, "init")
	runGit(t, destination, "config", "user.name", "Team Kit Tests")
	runGit(t, destination, "config", "user.email", "teamkit-tests@example.invalid")
	runGit(t, destination, "checkout", "-b", "content-alpha")
	if err := os.WriteFile(filepath.Join(destination, "content.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, destination, "add", "content.txt")
	runGit(t, destination, "commit", "-m", "base")
	base := runGitOutput(t, destination, "rev-parse", "refs/heads/content-alpha")
	runGit(t, destination, "checkout", "-b", "fixture-reserved")
	if err := os.MkdirAll(filepath.Join(destination, "DB"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "DB", "payload.txt"), []byte("attacker"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, destination, "add", "DB/payload.txt")
	runGit(t, destination, "commit", "-m", "reserved collision")
	target := runGitOutput(t, destination, "rev-parse", "HEAD")
	runGit(t, destination, "checkout", "content-alpha")
	runGit(t, destination, "update-ref", "refs/remotes/teamkit/content-alpha", target)
	runGit(t, destination, "config", "remote.origin.url", "https://git.example/content.git")
	if err := ensureManagedExclude(destination); err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(destination, ".env")
	privateBefore := []byte("PROJECT=wms\nTOKEN=private-canary\n")
	if err := os.WriteFile(privatePath, privateBefore, 0o600); err != nil {
		t.Fatal(err)
	}

	err := NewRepository(fetchNoopRunner{}).UpdateContent(context.Background(), destination, "https://git.example/content.git", "content-alpha", Credentials{})
	if ErrorCode(err) != "GIT_RESERVED_PATH_COLLISION" {
		t.Fatalf("UpdateContent error = %v; want GIT_RESERVED_PATH_COLLISION", err)
	}
	assertBytesUnchanged(t, privatePath, privateBefore)
	if got := runGitOutput(t, destination, "rev-parse", "refs/heads/content-alpha"); got != base {
		t.Fatalf("content branch ref = %q; want unchanged %q", got, base)
	}
}

type fetchNoopRunner struct{}

func (fetchNoopRunner) Run(ctx context.Context, command Command) (Result, error) {
	if slices.Contains(command.Args, "fetch") {
		return Result{}, nil
	}
	return SystemRunner{}.Run(ctx, command)
}

func assertBytesUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s changed: got %q want %q", path, got, want)
	}
}
