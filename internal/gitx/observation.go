package gitx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

// ObserveClean reports local worktree changes without fetching, merging, or
// accepting credentials. It is safe to call while planning.
func (r Repository) ObserveClean(ctx context.Context, directory string) error {
	if !filepath.IsAbs(directory) {
		return &Error{Code: "GIT_DESTINATION_INVALID", Err: fmt.Errorf("worktree must be absolute")}
	}
	if err := r.validateLocalConfig(ctx, directory, Credentials{}); err != nil {
		return err
	}
	status, err := r.runResult(ctx, Command{Args: hardenedArgs(directory, "status", "--porcelain"), Env: localEnvironment()}, Credentials{})
	if err != nil {
		return err
	}
	if strings.TrimSpace(status.Stdout) != "" {
		return &Error{Code: "LOCAL_CHANGES_DETECTED", Err: fmt.Errorf("repository contains local changes")}
	}
	return nil
}

// ObservePinned verifies an existing pinned checkout using only local Git
// state. It never fetches and never accepts credentials.
func (r Repository) ObservePinned(ctx context.Context, directory, remote, commit string) error {
	if err := safeRemoteURL(remote); err != nil {
		return err
	}
	if !validPinnedCommit(commit) {
		return &Error{Code: "GIT_PIN_INVALID", Err: fmt.Errorf("commit pin must be 40 lowercase hexadecimal characters")}
	}
	if !filepath.IsAbs(directory) {
		return &Error{Code: "GIT_DESTINATION_INVALID", Err: fmt.Errorf("worktree must be absolute")}
	}
	if err := r.validateLocalConfig(ctx, directory, Credentials{}); err != nil {
		return err
	}
	origin, err := r.runResult(ctx, Command{Args: hardenedArgs(directory, "config", "--get", "remote.origin.url"), Env: localEnvironment()}, Credentials{})
	if err != nil {
		return err
	}
	if strings.TrimSpace(origin.Stdout) != remote {
		return &Error{Code: "GIT_REMOTE_MISMATCH", Err: fmt.Errorf("pinned repository origin does not match the catalog")}
	}
	head, err := r.runResult(ctx, Command{Args: hardenedArgs(directory, "rev-parse", "--verify", "HEAD^{commit}"), Env: localEnvironment()}, Credentials{})
	if err != nil || strings.TrimSpace(head.Stdout) != commit {
		return &Error{Code: "GIT_PIN_UNVERIFIED", Err: fmt.Errorf("pinned repository HEAD does not match the catalog")}
	}
	status, err := r.runResult(ctx, Command{Args: hardenedArgs(directory, "status", "--porcelain"), Env: localEnvironment()}, Credentials{})
	if err != nil {
		return err
	}
	if strings.TrimSpace(status.Stdout) != "" {
		return &Error{Code: "LOCAL_CHANGES_DETECTED", Err: fmt.Errorf("pinned repository contains local changes")}
	}
	return nil
}

func (r Repository) validateContentWorktree(ctx context.Context, directory, status string, auth Credentials) error {
	status = strings.TrimSuffix(strings.ReplaceAll(status, "\r\n", "\n"), "\n")
	if status != "" && status != " M .gitignore" && status != "?? .gitignore" {
		return &Error{Code: "LOCAL_CHANGES_DETECTED", Err: fmt.Errorf("content repository contains changes outside Team Kit's root .gitignore delta")}
	}
	path := filepath.Join(directory, ".gitignore")
	if err := pathsafe.ValidateRegular(path); err != nil {
		return &Error{Code: "LOCAL_CHANGES_DETECTED", Err: fmt.Errorf("root .gitignore is unsafe: %w", err)}
	}
	current, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if status == "" {
			return nil
		}
		return &Error{Code: "LOCAL_CHANGES_DETECTED", Err: fmt.Errorf("root .gitignore delta is unreadable")}
	}
	if err != nil {
		return err
	}
	indexed, err := r.runResult(ctx, Command{
		Args: hardenedArgs(directory, "ls-files", "--stage", "--", ".gitignore"),
		Env:  localEnvironment(),
	}, auth)
	if err != nil {
		return err
	}
	var original []byte
	if strings.TrimSpace(indexed.Stdout) != "" {
		head, err := r.runResult(ctx, Command{
			Args: hardenedArgs(directory, "show", "HEAD:.gitignore"),
			Env:  localEnvironment(),
		}, auth)
		if err != nil {
			return err
		}
		original = []byte(head.Stdout)
	}
	if !workspace.IsTeamKitGitignore(original, current) {
		return &Error{Code: "LOCAL_CHANGES_DETECTED", Err: fmt.Errorf("root .gitignore contains changes not owned by Team Kit")}
	}
	return nil
}
