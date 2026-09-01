package gitx

import (
	"context"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestObserveClean_ReadsStatusWithoutCredentialsAndStopsAtDirtyWorktree(t *testing.T) {
	directory := testutil.TempDir(t)
	runner := &fakeRunner{results: []Result{{}, {Stdout: " M tracked.txt\n"}}}
	err := NewRepository(runner).ObserveClean(context.Background(), directory)
	if ErrorCode(err) != "LOCAL_CHANGES_DETECTED" {
		t.Fatalf("error=%v code=%q", err, ErrorCode(err))
	}
	if len(runner.commands) != 2 || !slices.Contains(runner.commands[0].Args, "--local") ||
		!slices.Contains(runner.commands[1].Args, "core.fsmonitor=false") || !slices.Contains(runner.commands[1].Args, "status") {
		t.Fatalf("commands=%#v", runner.commands)
	}
	for _, command := range runner.commands {
		environment := strings.Join(command.Env, "\n")
		for _, forbidden := range []string{"TEAMKIT_GIT_TOKEN=", "TEAMKIT_GIT_USERNAME=", "GIT_ASKPASS="} {
			if strings.Contains(environment, forbidden) {
				t.Fatalf("credential environment=%#v", command.Env)
			}
		}
	}
	if filepath.Clean(directory) != directory {
		t.Fatalf("fixture directory was not clean")
	}
}

func TestObserveBranchVerifiesCatalogOriginBranchAndCleanStateWithoutCredentials(t *testing.T) {
	directory := testutil.TempDir(t)
	remote := "https://git.example/project.git"
	runner := &fakeRunner{results: []Result{{}, {Stdout: remote + "\n"}, {Stdout: "develop\n"}, {}}}
	if err := NewRepository(runner).ObserveBranch(context.Background(), directory, remote, "develop"); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 4 {
		t.Fatalf("commands=%#v", runner.commands)
	}
	for _, command := range runner.commands {
		environment := strings.Join(command.Env, "\n")
		if strings.Contains(environment, "TEAMKIT_GIT_") || strings.Contains(environment, "GIT_ASKPASS=") {
			t.Fatalf("credential reached observation: %#v", command)
		}
	}
}

func TestObservePinnedVerifiesCatalogOriginExactHeadAndCleanStateWithoutCredentials(t *testing.T) {
	directory := testutil.TempDir(t)
	remote := "https://git.example/toolchain.git"
	commit := "0123456789abcdef0123456789abcdef01234567"
	runner := &fakeRunner{results: []Result{{}, {Stdout: remote + "\n"}, {Stdout: commit + "\n"}, {}}}
	if err := NewRepository(runner).ObservePinned(context.Background(), directory, remote, commit); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 4 {
		t.Fatalf("commands=%#v", runner.commands)
	}
	if !slices.Contains(runner.commands[0].Args, "--local") ||
		!slices.Contains(runner.commands[1].Args, "remote.origin.url") ||
		!slices.Contains(runner.commands[2].Args, "HEAD^{commit}") ||
		!slices.Contains(runner.commands[3].Args, "status") {
		t.Fatalf("observation order=%#v", runner.commands)
	}
	for _, command := range runner.commands {
		environment := strings.Join(command.Env, "\n")
		if strings.Contains(environment, "TEAMKIT_GIT_") || strings.Contains(environment, "GIT_ASKPASS=") ||
			strings.Contains(environment, "GIT_SSL_CAINFO=") {
			t.Fatalf("credential reached pinned observation: %#v", command)
		}
		if slices.Contains(command.Args, "fetch") {
			t.Fatalf("network command reached pinned observation: %#v", command)
		}
	}
}

func TestObservePinnedRejectsWrongHeadBeforeStatus(t *testing.T) {
	directory := testutil.TempDir(t)
	remote := "https://git.example/toolchain.git"
	want := "0123456789abcdef0123456789abcdef01234567"
	runner := &fakeRunner{results: []Result{{}, {Stdout: remote + "\n"}, {Stdout: strings.Repeat("a", 40) + "\n"}}}
	err := NewRepository(runner).ObservePinned(context.Background(), directory, remote, want)
	if ErrorCode(err) != "GIT_PIN_UNVERIFIED" {
		t.Fatalf("error=%v code=%q", err, ErrorCode(err))
	}
	if len(runner.commands) != 3 {
		t.Fatalf("wrong HEAD must stop before status: %#v", runner.commands)
	}
}

func TestObservePinnedRejectsRemoteAndDirtyDrift(t *testing.T) {
	directory := testutil.TempDir(t)
	remote := "https://git.example/toolchain.git"
	commit := "0123456789abcdef0123456789abcdef01234567"
	for _, test := range []struct {
		name     string
		results  []Result
		wantCode string
		wantRuns int
	}{
		{
			name:     "remote",
			results:  []Result{{}, {Stdout: "https://attacker.invalid/toolchain.git\n"}},
			wantCode: "GIT_REMOTE_MISMATCH",
			wantRuns: 2,
		},
		{
			name:     "dirty",
			results:  []Result{{}, {Stdout: remote + "\n"}, {Stdout: commit + "\n"}, {Stdout: " M skill.md\n"}},
			wantCode: "LOCAL_CHANGES_DETECTED",
			wantRuns: 4,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{results: test.results}
			err := NewRepository(runner).ObservePinned(context.Background(), directory, remote, commit)
			if ErrorCode(err) != test.wantCode || len(runner.commands) != test.wantRuns {
				t.Fatalf("error=%v code=%q commands=%#v", err, ErrorCode(err), runner.commands)
			}
		})
	}
}

func TestObserveBranch_RejectsWorktreeScopedProcessConfiguration(t *testing.T) {
	root := committedContentFixture(t, false)
	runGit(t, root, "config", "extensions.worktreeConfig", "true")
	runGit(t, root, "config", "--worktree", "filter.review.smudge", "review-filter-command")

	err := NewRepository(SystemRunner{}).ObserveBranch(context.Background(), root, "https://git.example/content.git", "content-alpha")
	if ErrorCode(err) != "GIT_CONFIG_UNSAFE" {
		t.Fatalf("ObserveBranch error=%v, want GIT_CONFIG_UNSAFE", err)
	}
}

func TestObserveBranch_RejectsSparseCheckoutConfiguration(t *testing.T) {
	for _, key := range []string{"core.sparseCheckout", "core.sparseCheckoutCone"} {
		t.Run(key, func(t *testing.T) {
			root := committedContentFixture(t, false)
			runGit(t, root, "config", key, "true")

			err := NewRepository(SystemRunner{}).ObserveBranch(context.Background(), root, "https://git.example/content.git", "content-alpha")
			if ErrorCode(err) != "GIT_CONFIG_UNSAFE" {
				t.Fatalf("ObserveBranch error=%v, want GIT_CONFIG_UNSAFE", err)
			}
		})
	}
}

func TestObserveBranch_RejectsProcessLaunchingConfiguration(t *testing.T) {
	for _, key := range []string{"core.alternateRefsCommand", "gc.recentObjectsHook", "core.gitProxy"} {
		t.Run(key, func(t *testing.T) {
			root := committedContentFixture(t, false)
			runGit(t, root, "config", key, "review-process-command")

			err := NewRepository(SystemRunner{}).ObserveBranch(context.Background(), root, "https://git.example/content.git", "content-alpha")
			if ErrorCode(err) != "GIT_CONFIG_UNSAFE" {
				t.Fatalf("ObserveBranch error=%v, want GIT_CONFIG_UNSAFE", err)
			}
		})
	}
}
