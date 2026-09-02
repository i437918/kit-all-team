package hermes

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

var (
	ErrProfileRunner = errors.New("Hermes profile runner is required")
	ErrProfileName   = errors.New("Hermes profile name is invalid")
	ErrProfileDoctor = errors.New("HERMES_PROFILE_DOCTOR_FAILED")
)

var profileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// CommandRunner runs a command without putting credentials in its arguments.
type CommandRunner interface {
	Run(context.Context, string, []string) error
	Capture(context.Context, string, []string) ([]byte, error)
}

// ProfileCLI invokes the stable, non-interactive Hermes profile commands.
type ProfileCLI struct {
	Executable string
	Runner     CommandRunner
}

func (c ProfileCLI) Create(ctx context.Context, identity string) error {
	if err := c.validate(identity); err != nil {
		return err
	}
	return c.Runner.Run(ctx, c.Executable, []string{"profile", "create", identity, "--no-alias"})
}

// OptInBundledSkills restores Hermes-provided skills for one profile through
// the only supported Hermes migration command.
func (c ProfileCLI) OptInBundledSkills(ctx context.Context, identity string) error {
	if err := c.validate(identity); err != nil {
		return err
	}
	return c.Runner.Run(ctx, c.Executable, []string{"-p", identity, "skills", "opt-in", "--sync"})
}

func (c ProfileCLI) Doctor(ctx context.Context, identity string) error {
	if err := c.validate(identity); err != nil {
		return err
	}
	output, err := c.Runner.Capture(ctx, c.Executable, []string{"-p", identity, "doctor"})
	if err != nil {
		return fmt.Errorf("%w: subprocess", ErrProfileDoctor)
	}
	if len(output) > maxHermesCommandOutput {
		return fmt.Errorf("%w: output limit", ErrProfileDoctor)
	}
	return ParseDoctorTerminal(output)
}

func (c ProfileCLI) validate(identity string) error {
	if c.Runner == nil {
		return ErrProfileRunner
	}
	if c.Executable == "" || !profileNamePattern.MatchString(identity) {
		return ErrProfileName
	}
	return nil
}
