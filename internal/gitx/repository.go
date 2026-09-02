package gitx

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

// Credentials identify an askpass program and environment-only token for Git.
type Credentials struct {
	AskPassPath string
	Username    string
	Token       string
	CAFile      string
}

// Repository performs only non-destructive source and database Git operations.
type Repository struct {
	runner Runner
}

// Error identifies a stable Git safety error code.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// ErrorCode returns the stable code for err, or an empty string for ordinary errors.
func ErrorCode(err error) string {
	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}

// NewRepository binds Git operations to runner. Tests can provide a deterministic fake.
func NewRepository(runner Runner) Repository { return Repository{runner: runner} }

// CloneContent materializes the specified catalog content branch in destination.
// The workspace may already contain Team Kit's private .teamkit directory, so
// this uses an idempotent init/fetch/checkout sequence instead of `git clone`.
func (r Repository) CloneContent(ctx context.Context, remote, branch, destination string, auth Credentials) error {
	if err := safeRemoteURL(remote); err != nil {
		return err
	}
	if !strings.HasPrefix(branch, "content-") {
		return &Error{Code: "CONTENT_BRANCH_INVALID", Err: fmt.Errorf("content branch is invalid")}
	}
	if !filepath.IsAbs(destination) {
		return &Error{Code: "GIT_DESTINATION_INVALID", Err: fmt.Errorf("content destination must be absolute")}
	}
	hasGit, err := preflightContentClone(destination)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	if !hasGit {
		if err := r.runRepositoryCreationMutation(ctx, destination, Command{Args: []string{"-C", destination, "init"}, Env: localEnvironment()}, auth); err != nil {
			return err
		}
	}
	if err := r.validateLocalConfig(ctx, destination, auth); err != nil {
		return err
	}
	committed, currentBranch, err := r.inspectHead(ctx, destination, auth)
	if err != nil {
		return err
	}
	if committed {
		if currentBranch != branch {
			return &Error{Code: "GIT_BRANCH_MISMATCH", Err: fmt.Errorf("existing content repository is on another branch")}
		}
		return r.updateBranch(ctx, destination, remote, branch, auth)
	}
	if err := validateUnbornContentResidue(destination, true, !hasGit); err != nil {
		return err
	}
	if err := ensureManagedExclude(destination); err != nil {
		return err
	}
	commands := [][]string{
		{"-C", destination, "config", "remote.origin.url", remote},
		{"-C", destination, "config", "remote.origin.fetch", "+refs/heads/" + branch + ":refs/remotes/origin/" + branch},
	}
	for _, args := range commands {
		if err := r.runMutation(ctx, destination, Command{Args: args, Env: localEnvironment()}, auth); err != nil {
			return err
		}
	}
	if err := r.runMutation(ctx, destination, Command{
		Args: hardenedArgs(destination, "fetch", "--no-tags", remote, "+refs/heads/"+branch+":refs/remotes/origin/"+branch),
		Env:  auth.authenticatedEnvironment(),
	}, auth); err != nil {
		return err
	}
	if err := r.validateFetchedContentTree(ctx, destination, "refs/remotes/origin/"+branch, auth); err != nil {
		return err
	}
	if err := r.runMutation(ctx, destination, Command{
		Args: hardenedArgs(destination, "checkout", "-B", branch, "--track", "origin/"+branch),
		Env:  localEnvironment(),
	}, auth); err != nil {
		return err
	}
	return nil
}

func ensureManagedExclude(destination string) error {
	return workspace.EnsureLocalExclude(filepath.Join(destination, ".git", "info", "exclude"))
}

// CloneDatabase clones the fixed develop branch into workspace/db.
func (r Repository) CloneDatabase(ctx context.Context, remote, workspace string, auth Credentials) error {
	if err := safeRemoteURL(remote); err != nil {
		return err
	}
	destination := filepath.Join(workspace, "db")
	hasGit, err := preflightDatabaseClone(workspace, destination)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	if !hasGit {
		if err := r.runRepositoryCreationMutation(ctx, destination, Command{Args: []string{"-C", destination, "init"}, Env: localEnvironment()}, auth); err != nil {
			return err
		}
	}
	if err := r.validateLocalConfig(ctx, destination, auth); err != nil {
		return err
	}
	committed, currentBranch, err := r.inspectHead(ctx, destination, auth)
	if err != nil {
		return err
	}
	if committed {
		if currentBranch != "develop" {
			return &Error{Code: "GIT_BRANCH_MISMATCH", Err: fmt.Errorf("existing database repository is on another branch")}
		}
		return r.updateBranch(ctx, destination, remote, "develop", auth)
	}
	if err := validateUnbornDatabaseResidue(destination); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"-C", destination, "config", "remote.origin.url", remote},
		{"-C", destination, "config", "remote.origin.fetch", "+refs/heads/develop:refs/remotes/origin/develop"},
	} {
		if err := r.runMutation(ctx, destination, Command{Args: args, Env: localEnvironment()}, auth); err != nil {
			return err
		}
	}
	if err := r.runMutation(ctx, destination, Command{
		Args: hardenedArgs(destination, "fetch", "--no-tags", remote, "+refs/heads/develop:refs/remotes/origin/develop"),
		Env:  auth.authenticatedEnvironment(),
	}, auth); err != nil {
		return err
	}
	return r.runMutation(ctx, destination, Command{
		Args: hardenedArgs(destination, "checkout", "-B", "develop", "--track", "origin/develop"),
		Env:  localEnvironment(),
	}, auth)
}

func (r Repository) inspectHead(ctx context.Context, directory string, auth Credentials) (bool, string, error) {
	result, err := r.runResult(ctx, Command{
		Args: hardenedArgs(directory, "status", "--porcelain=v2", "--branch"),
		Env:  localEnvironment(),
	}, auth)
	if err != nil {
		return false, "", err
	}
	committed := false
	branch := ""
	for _, line := range strings.Split(strings.ReplaceAll(result.Stdout, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "# branch.oid ") {
			committed = strings.TrimSpace(strings.TrimPrefix(line, "# branch.oid ")) != "(initial)"
		}
		if strings.HasPrefix(line, "# branch.head ") {
			branch = strings.TrimSpace(strings.TrimPrefix(line, "# branch.head "))
		}
	}
	return committed, branch, nil
}

// UpdateDatabase fetches develop and fast-forwards it only after proving it is clean.
func (r Repository) UpdateDatabase(ctx context.Context, database, remote string, auth Credentials) error {
	return r.updateBranch(ctx, database, remote, "develop", auth)
}

// UpdateContent fetches and fast-forwards the selected content branch after proving it is clean.
func (r Repository) UpdateContent(ctx context.Context, content, remote, branch string, auth Credentials) error {
	if err := safeRemoteURL(remote); err != nil {
		return err
	}
	if !strings.HasPrefix(branch, "content-") {
		return &Error{Code: "CONTENT_BRANCH_INVALID", Err: fmt.Errorf("content branch is invalid")}
	}
	return r.updateBranch(ctx, content, remote, branch, auth)
}

// SyncPinned materializes exactly commit at destination without changing any other Git state.
func (r Repository) SyncPinned(ctx context.Context, remote, commit, destination string, auth Credentials) error {
	if err := safeRemoteURL(remote); err != nil {
		return err
	}
	if !validPinnedCommit(commit) {
		return &Error{Code: "GIT_PIN_INVALID", Err: fmt.Errorf("commit pin must be 40 lowercase hexadecimal characters")}
	}
	info, err := os.Stat(destination)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := r.runRepositoryCreationMutation(ctx, destination, Command{
			Args: hardenedGlobalArgs("clone", "--no-checkout", "--no-tags", remote, destination),
			Env:  auth.authenticatedEnvironment(),
		}, auth); err != nil {
			return err
		}
	} else {
		if !info.IsDir() {
			return &Error{Code: "GIT_DESTINATION_INVALID", Err: fmt.Errorf("pinned destination is not a directory")}
		}
		if err := r.validateLocalConfig(ctx, destination, auth); err != nil {
			return err
		}
		origin, err := r.runResult(ctx, Command{Args: hardenedArgs(destination, "config", "--get", "remote.origin.url"), Env: localEnvironment()}, auth)
		if err != nil {
			return err
		}
		if strings.TrimSpace(origin.Stdout) != remote {
			return &Error{Code: "GIT_REMOTE_MISMATCH", Err: fmt.Errorf("pinned repository origin does not match the catalog")}
		}
		status, err := r.runResult(ctx, Command{Args: hardenedArgs(destination, "status", "--porcelain"), Env: localEnvironment()}, auth)
		if err != nil {
			return err
		}
		if strings.TrimSpace(status.Stdout) != "" {
			return &Error{Code: "LOCAL_CHANGES_DETECTED", Err: fmt.Errorf("pinned repository contains local changes")}
		}
		if err := r.runMutation(ctx, destination, Command{
			Args: hardenedArgs(destination, "fetch", "--no-tags", remote),
			Env:  auth.authenticatedEnvironment(),
		}, auth); err != nil {
			return err
		}
	}
	verified, err := r.runResult(ctx, Command{Args: hardenedArgs(destination, "rev-parse", "--verify", commit+"^{commit}"), Env: localEnvironment()}, auth)
	if err != nil {
		return err
	}
	if strings.TrimSpace(verified.Stdout) != commit {
		return &Error{Code: "GIT_PIN_UNVERIFIED", Err: fmt.Errorf("pinned commit could not be verified")}
	}
	return r.runMutation(ctx, destination, Command{Args: hardenedArgs(destination, "checkout", "--detach", commit), Env: localEnvironment()}, auth)
}

func (r Repository) updateBranch(ctx context.Context, directory, remote, branch string, auth Credentials) error {
	if err := r.observeBranch(ctx, directory, remote, branch, auth); err != nil {
		return err
	}
	if err := r.runMutation(ctx, directory, Command{
		Args: hardenedArgs(directory, "fetch", "--no-tags", remote, "+refs/heads/"+branch+":refs/remotes/teamkit/"+branch),
		Env:  auth.authenticatedEnvironment(),
	}, auth); err != nil {
		return err
	}
	if strings.HasPrefix(branch, "content-") {
		if err := r.validateFetchedContentTree(ctx, directory, "refs/remotes/teamkit/"+branch, auth); err != nil {
			return err
		}
	}
	if _, err := r.runResult(ctx, Command{
		Args: hardenedArgs(directory, "merge-base", "--is-ancestor", "HEAD", "refs/remotes/teamkit/"+branch),
		Env:  localEnvironment(),
	}, auth); err != nil {
		return &Error{Code: "GIT_NON_FAST_FORWARD", Err: fmt.Errorf("local branch is not an ancestor of the catalog branch")}
	}
	return r.runMutation(ctx, directory, Command{
		Args: hardenedArgs(directory, "merge", "--ff-only", "--no-edit", "refs/remotes/teamkit/"+branch),
		Env:  localEnvironment(),
	}, auth)
}

// ObserveBranch verifies a catalog checkout without credentials or network I/O.
func (r Repository) ObserveBranch(ctx context.Context, directory, remote, branch string) error {
	return r.observeBranch(ctx, directory, remote, branch, Credentials{})
}

func (r Repository) observeBranch(ctx context.Context, directory, remote, branch string, auth Credentials) error {
	if err := safeRemoteURL(remote); err != nil {
		return err
	}
	if err := r.validateLocalConfig(ctx, directory, auth); err != nil {
		return err
	}
	origin, err := r.runResult(ctx, Command{Args: hardenedArgs(directory, "config", "--get", "remote.origin.url"), Env: localEnvironment()}, auth)
	if err != nil {
		return err
	}
	if strings.TrimSpace(origin.Stdout) != remote {
		return &Error{Code: "GIT_REMOTE_MISMATCH", Err: fmt.Errorf("repository origin does not match the project catalog")}
	}
	current, err := r.runResult(ctx, Command{Args: hardenedArgs(directory, "symbolic-ref", "--quiet", "--short", "HEAD"), Env: localEnvironment()}, auth)
	if err != nil {
		return &Error{Code: "GIT_BRANCH_MISMATCH", Err: fmt.Errorf("repository branch is detached or unreadable")}
	}
	if strings.TrimSpace(current.Stdout) != branch {
		return &Error{Code: "GIT_BRANCH_MISMATCH", Err: fmt.Errorf("repository is not on the catalog branch")}
	}
	status, err := r.runResult(ctx, Command{Args: hardenedArgs(directory, "status", "--porcelain"), Env: localEnvironment()}, auth)
	if err != nil {
		return err
	}
	if strings.HasPrefix(branch, "content-") {
		if err := r.validateContentWorktree(ctx, directory, status.Stdout, auth); err != nil {
			return err
		}
	} else if strings.TrimSpace(status.Stdout) != "" {
		return &Error{Code: "LOCAL_CHANGES_DETECTED", Err: fmt.Errorf("database contains local changes")}
	}
	return nil
}

func (r Repository) validateLocalConfig(ctx context.Context, directory string, auth Credentials) error {
	result, err := r.runResult(ctx, Command{
		Args: hardenedArgs(directory, "config", "--local", "--no-includes", "--null", "--list"),
		Env:  localEnvironment(),
	}, auth)
	if err != nil {
		return err
	}
	for _, item := range strings.Split(result.Stdout, "\x00") {
		key := item
		if index := strings.IndexAny(key, "\n="); index >= 0 {
			key = key[:index]
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if unsafeLocalConfigKey(key) {
			return &Error{Code: "GIT_CONFIG_UNSAFE", Err: fmt.Errorf("repository contains network- or process-affecting local configuration")}
		}
	}
	return nil
}

func unsafeLocalConfigKey(key string) bool {
	for _, prefix := range []string{
		"alias.", "credential.", "diff.", "difftool.", "filter.", "http.",
		"include.", "includeif.", "merge.", "mergetool.", "pager.", "submodule.", "tar.", "url.",
	} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	if strings.HasSuffix(key, ".uploadpack") || strings.HasSuffix(key, ".receivepack") {
		return true
	}
	switch key {
	case "core.alternaterefscommand", "core.attributesfile", "core.editor", "core.fsmonitor",
		"core.gitproxy", "core.hookspath", "core.pager", "core.sparsecheckout",
		"core.sparsecheckoutcone", "core.sshcommand", "core.worktree",
		"extensions.worktreeconfig", "gc.recentobjectshook", "gpg.program",
		"gpg.ssh.program", "gpg.x509.program", "index.sparse", "interactive.difffilter", "sequence.editor":
		return true
	default:
		return false
	}
}

func hardenedArgs(directory string, operation ...string) []string {
	args := []string{
		"--no-optional-locks",
		"-c", "credential.helper=",
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=" + filepath.Join(directory, ".git", "teamkit-disabled-hooks"),
		"-c", "http.extraHeader=",
		"-c", "http.followRedirects=false",
		"-c", "protocol.file.allow=never",
		"-C", directory,
	}
	return append(args, operation...)
}

func hardenedGlobalArgs(operation ...string) []string {
	args := []string{
		"--no-optional-locks",
		"-c", "credential.helper=",
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "http.extraHeader=",
		"-c", "http.followRedirects=false",
		"-c", "protocol.file.allow=never",
	}
	return append(args, operation...)
}

func localEnvironment() []string {
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_COUNT=0",
	}
}

func (r Repository) run(ctx context.Context, command Command, auth Credentials) error {
	_, err := r.runResult(ctx, command, auth)
	return err
}

func (r Repository) runMutation(ctx context.Context, worktree string, command Command, auth Credentials) error {
	if err := validateGitMutationMetadata(worktree); err != nil {
		return err
	}
	if err := r.run(ctx, command, auth); err != nil {
		return err
	}
	return validateGitMutationMetadata(worktree)
}

func (r Repository) runRepositoryCreationMutation(ctx context.Context, worktree string, command Command, auth Credentials) error {
	if err := validateGitRepositoryCreationMetadata(worktree); err != nil {
		return err
	}
	if err := r.run(ctx, command, auth); err != nil {
		return err
	}
	return validateGitMutationMetadata(worktree)
}

func (r Repository) runResult(ctx context.Context, command Command, auth Credentials) (Result, error) {
	if r.runner == nil {
		return Result{}, &Error{Code: "GIT_RUNNER_REQUIRED", Err: fmt.Errorf("git runner is required")}
	}
	result, err := r.runner.Run(ctx, command)
	if err != nil {
		return Result{}, &Error{Code: "GIT_COMMAND_FAILED", Err: fmt.Errorf("git command failed: %s", gitCommandDiagnostic(result, err, auth))}
	}
	return result, nil
}

func (c Credentials) environment() []string {
	environment := []string{"GIT_TERMINAL_PROMPT=0"}
	if c.AskPassPath != "" {
		environment = append(environment, "GIT_ASKPASS="+c.AskPassPath)
	}
	if c.Username != "" {
		environment = append(environment, "TEAMKIT_GIT_USERNAME="+c.Username)
	}
	if c.Token != "" {
		environment = append(environment, "TEAMKIT_GIT_TOKEN="+c.Token)
	}
	if c.CAFile != "" {
		environment = append(environment, "GIT_SSL_CAINFO="+c.CAFile)
	}
	return environment
}

func (c Credentials) authenticatedEnvironment() []string {
	environment := c.environment()
	return append(environment,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_COUNT=0",
	)
}

func safeRemoteURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return &Error{Code: "GIT_URL_UNSAFE", Err: fmt.Errorf("remote URL must not contain credentials")}
	}
	return nil
}

func sanitize(message string, credentials Credentials) string {
	values := []string{credentials.Username, credentials.Token, credentials.CAFile}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		if value != "" {
			message = strings.ReplaceAll(message, value, "[REDACTED]")
		}
	}
	return message
}

const maxGitDiagnosticBytes = 2048

func gitCommandDiagnostic(result Result, err error, credentials Credentials) string {
	parts := []string{err.Error()}
	if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
		parts = append(parts, "stderr: "+stderr)
	}
	diagnostic := sanitize(strings.Join(parts, "; "), credentials)
	diagnostic = strings.Join(strings.Fields(strings.ToValidUTF8(diagnostic, "?")), " ")
	if len(diagnostic) <= maxGitDiagnosticBytes {
		return diagnostic
	}
	const suffix = "...[truncated]"
	limit := maxGitDiagnosticBytes - len(suffix)
	for limit > 0 && !utf8.ValidString(diagnostic[:limit]) {
		limit--
	}
	return diagnostic[:limit] + suffix
}

var pinnedCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func validPinnedCommit(commit string) bool { return pinnedCommitPattern.MatchString(commit) }
