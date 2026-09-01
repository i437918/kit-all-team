// Package gitx provides the non-destructive system-Git operations used by Team Kit.
package gitx

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// Command is one argument-vector invocation of system Git.
type Command struct {
	Args []string
	Env  []string
	Dir  string
}

// Result contains bounded command diagnostics. Callers must sanitize it before display.
type Result struct {
	Stdout string
	Stderr string
}

// Runner executes an argument-vector Git command.
type Runner interface {
	Run(context.Context, Command) (Result, error)
}

// SystemRunner invokes the Git executable from PATH without a shell.
type SystemRunner struct{}

// Run invokes git with the command's argument vector and supplied environment.
func (SystemRunner) Run(ctx context.Context, command Command) (Result, error) {
	cmd := exec.CommandContext(ctx, "git", command.Args...)
	cmd.Dir = command.Dir
	cmd.Env = append(sanitizedBaseEnvironment(), command.Env...)
	output, err := cmd.Output()
	result := Result{Stdout: string(output)}
	if exitError, ok := err.(*exec.ExitError); ok {
		result.Stderr = string(exitError.Stderr)
	}
	return result, err
}

func sanitizedBaseEnvironment() []string {
	base := os.Environ()
	result := make([]string, 0, len(base))
	for _, value := range base {
		name := value
		if index := strings.IndexByte(value, '='); index >= 0 {
			name = value[:index]
		}
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "TEAMKIT_GIT_") {
			continue
		}
		result = append(result, value)
	}
	return result
}
