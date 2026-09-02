package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/cli"
	"github.com/mi1man-cmd/kit-all-team/internal/credentials"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
	"github.com/mi1man-cmd/kit-all-team/internal/registry"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestNewRunner_DoesNotCreateRegistry(t *testing.T) {
	base := testutil.TempDir(t)
	localAppData := filepath.Join(base, "missing-local")
	xdgConfig := filepath.Join(base, "missing-xdg")
	userHome := filepath.Join(base, "missing-home")
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	registryPath, err := registry.DefaultPath(registry.LocationOptions{GOOS: runtime.GOOS, Getenv: os.Getenv, UserHomeDir: os.UserHomeDir})
	if err != nil {
		t.Fatal(err)
	}
	_ = newRunner(strings.NewReader(""), io.Discard, io.Discard)
	if _, err := os.Lstat(registryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructor touched registry file %q: %v", registryPath, err)
	}
	if _, err := os.Lstat(filepath.Dir(registryPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructor touched registry directory %q: %v", filepath.Dir(registryPath), err)
	}
}

func TestNewRunner_WiresHermesPersistenceAndSharedCredentialChooser(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = writer.Close() })

	runner := newRunner(input, io.Discard, io.Discard)
	if runner.ConfigureHermesHome == nil || reflect.ValueOf(runner.ConfigureHermesHome).Pointer() != reflect.ValueOf(platform.ConfigureHermesHome).Pointer() {
		t.Fatal("production runner does not use platform.ConfigureHermesHome")
	}
	if runner.CredentialFactory == nil {
		t.Fatal("production runner does not provide a credential factory")
	}
	resolver, ok := runner.CredentialFactory().(credentials.Resolver)
	if !ok {
		t.Fatalf("credential source=%T", runner.CredentialFactory())
	}
	reader, readerOK := resolver.Reader.(*credentials.ConsoleReader)
	chooser, chooserOK := resolver.Chooser.(*credentials.ConsoleReader)
	if !readerOK || !chooserOK || reader == nil || reader != chooser {
		t.Fatalf("reader=%T chooser=%T shared=%t", resolver.Reader, resolver.Chooser, readerOK && chooserOK && reader == chooser)
	}
}

func TestNewRunner_DefersOperationalAdaptersForPublicQueries(t *testing.T) {
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()

	runner := newRunner(input, io.Discard, io.Discard)
	if runner.Service != nil || runner.Credentials != nil {
		t.Fatalf("public-query runner initialized adapters: service=%T credentials=%T", runner.Service, runner.Credentials)
	}
	if runner.ServiceFactory == nil || runner.CredentialFactory == nil {
		t.Fatalf("public-query runner is missing lazy factories: %#v", runner)
	}
	serviceCalls, credentialCalls := 0, 0
	runner.ServiceFactory = func() cli.Service {
		serviceCalls++
		return nil
	}
	runner.CredentialFactory = func() cli.CredentialSource {
		credentialCalls++
		return nil
	}
	var stdout bytes.Buffer
	runner.Out = &stdout
	if code := runner.Run(context.Background(), []string{"catalog", "--json"}); code != cli.ExitOK {
		t.Fatalf("catalog exit=%d", code)
	}
	if serviceCalls != 0 || credentialCalls != 0 || !strings.Contains(stdout.String(), `"schema_version":1`) {
		t.Fatalf("service=%d credentials=%d stdout=%q", serviceCalls, credentialCalls, stdout.String())
	}
}
