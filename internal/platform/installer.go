package platform

import (
	"context"
	"errors"
)

// ErrInstallerRunnerInvalid reports an incomplete fixed installer invocation.
var ErrInstallerRunnerInvalid = errors.New("INSTALLER_RUNNER_INVALID")

// ProcessRunner executes an already-vetted program with an argument vector.
type ProcessRunner interface {
	Run(ctx context.Context, name string, args []string) error
}

// ProcessRunnerFunc adapts a function to ProcessRunner.
type ProcessRunnerFunc func(ctx context.Context, name string, args []string) error

// Run calls f(ctx, name, args).
func (f ProcessRunnerFunc) Run(ctx context.Context, name string, args []string) error {
	return f(ctx, name, args)
}

// FixedInstallerRunner invokes only its configured executable and fixed argv.
// Its injected runner makes it safe to test without launching a real installer.
type FixedInstallerRunner struct {
	Executable string
	Arguments  []string
	RunProcess ProcessRunner
}

// Run forwards a defensive copy of the configured argument vector.
func (r FixedInstallerRunner) Run(ctx context.Context) error {
	if r.Executable == "" || r.RunProcess == nil {
		return ErrInstallerRunnerInvalid
	}
	return r.RunProcess.Run(ctx, r.Executable, append([]string(nil), r.Arguments...))
}
