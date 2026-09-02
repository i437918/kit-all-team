package hermes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
)

const (
	// HermesCompatibleVersion is the external Hermes release accepted for the
	// pinned profile and doctor contracts used by this build.
	HermesCompatibleVersion = HermesMinimumVersion
	maxVersionOutputBytes   = 64 << 10
)

var pythonVersionLine = regexp.MustCompile(`^Python: [0-9]+\.[0-9]+\.[0-9]+[A-Za-z0-9.+_-]*$`)

// ErrExecutableUnverified reports an absent, unsafe, shadowed, or incompatible
// user-managed Hermes executable.
var ErrExecutableUnverified = errors.New("HERMES_EXECUTABLE_UNVERIFIED")

type executableLookPath func(string) (string, error)
type executableCapture func(context.Context, string, []string) ([]byte, error)

// ResolveCompatibleExecutable resolves only the first PATH candidate, makes it
// absolute, validates its filesystem shape, and checks the pinned version
// contract. It never searches past an incompatible shadowing candidate.
func ResolveCompatibleExecutable(ctx context.Context, lookPath executableLookPath, capture executableCapture) (string, error) {
	if lookPath == nil || capture == nil {
		return "", ErrExecutableUnverified
	}
	path, err := lookPath("hermes")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrExecutableUnverified, err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrExecutableUnverified, err)
	}
	path = filepath.Clean(path)
	ready, err := executableReady(path)
	if err != nil || !ready {
		return "", fmt.Errorf("%w: executable path is unsafe", ErrExecutableUnverified)
	}
	info, err := VerifyExecutable(ctx, path, capture)
	if err != nil {
		return "", err
	}
	return info.Executable, nil
}

// ResolveCompatibleSystemExecutable verifies the first Hermes executable on
// the current PATH without mutating application or system state.
func ResolveCompatibleSystemExecutable(ctx context.Context) (string, error) {
	return ResolveCompatibleExecutable(ctx, exec.LookPath, func(ctx context.Context, name string, args []string) ([]byte, error) {
		return captureCommandBounded(ctx, name, args, maxVersionOutputBytes)
	})
}

func compatibleVersionOutput(executable string, output []byte) bool {
	_, err := ParseRuntimeInfo(executable, output)
	return err == nil
}

func VerifyExecutable(ctx context.Context, path string, capture executableCapture) (RuntimeInfo, error) {
	if capture == nil {
		return RuntimeInfo{}, ErrExecutableUnverified
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return RuntimeInfo{}, fmt.Errorf("%w: invalid executable path", ErrExecutableUnverified)
	}
	abs = filepath.Clean(abs)
	ready, err := executableReady(abs)
	if err != nil || !ready {
		return RuntimeInfo{}, fmt.Errorf("%w: executable path is unsafe", ErrExecutableUnverified)
	}
	output, err := capture(ctx, abs, []string{"--version"})
	if err != nil {
		return RuntimeInfo{}, fmt.Errorf("%w: version command failed", ErrExecutableUnverified)
	}
	info, err := ParseRuntimeInfo(abs, output)
	if err != nil {
		return RuntimeInfo{}, err
	}
	return info, nil
}

type boundedBuffer struct {
	mu       sync.Mutex
	data     bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit + 1 - b.data.Len()
	if remaining > 0 {
		take := len(p)
		if take > remaining {
			take = remaining
		}
		_, _ = b.data.Write(p[:take])
	}
	if b.data.Len() > b.limit || len(p) > remaining {
		b.overflow = true
	}
	return len(p), nil
}

func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data.Bytes()...)
}

func captureCommandBounded(ctx context.Context, name string, args []string, limit int) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	buffer := &boundedBuffer{limit: limit}
	command.Stdout, command.Stderr = buffer, buffer
	err := command.Run()
	if buffer.overflow {
		return nil, ErrExecutableUnverified
	}
	return buffer.Bytes(), err
}

// ExecutableReady rechecks the absolute executable's filesystem safety.
func ExecutableReady(path string) (bool, error) {
	ready, err := executableReady(path)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrExecutableUnverified, err)
	}
	return ready, nil
}
