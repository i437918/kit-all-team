package gitx

import (
	"fmt"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewAskPassSession_ReturnsEnvironmentCredentialsWithoutPersistingSecrets(t *testing.T) {
	root := testutil.TempDir(t)
	username := fmt.Sprintf("teamkit-user-canary-%d", os.Getpid())
	token := fmt.Sprintf("teamkit-token-canary-%d", os.Getpid())
	session, err := NewAskPassSession(root, Credentials{
		Username: username,
		Token:    token,
		CAFile:   filepath.Join(root, "company-ca.pem"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	credentials := session.Credentials()
	if credentials.AskPassPath == "" || credentials.Username != username || credentials.Token != token {
		t.Fatalf("credentials = %#v", credentials)
	}
	if filepath.Dir(credentials.AskPassPath) != root {
		t.Fatalf("helper %q is not directly under %q", credentials.AskPassPath, root)
	}
	if runtime.GOOS == "windows" && filepath.Ext(credentials.AskPassPath) != ".exe" {
		t.Fatalf("Windows helper extension = %q; want .exe", filepath.Ext(credentials.AskPassPath))
	}
	if runtime.GOOS != "windows" && filepath.Ext(credentials.AskPassPath) == ".sh" {
		t.Fatalf("POSIX helper must be a self-executable, not a shell script: %q", credentials.AskPassPath)
	}
	data, err := os.ReadFile(credentials.AskPassPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{username, token} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("askpass helper leaked %q: %q", secret, data)
		}
	}
}

func TestNewAskPassSession_UsesExactSelfExecutableOnEveryPlatform(t *testing.T) {
	root := testutil.TempDir(t)
	source := filepath.Join(root, "source-helper")
	if runtime.GOOS == "windows" {
		source += ".exe"
	}
	want := []byte("teamkit-self-executable-fixture")
	if err := os.WriteFile(source, want, 0o700); err != nil {
		t.Fatal(err)
	}
	session, err := newAskPassSessionWithExecutable(root, Credentials{Username: "expected", Token: "secret"}, source)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	got, err := os.ReadFile(session.Credentials().AskPassPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("askpass helper is not an exact copy of the validated Team Kit executable")
	}
}

func TestIsAskPassInvocation_AcceptsExactHelperOnEveryPlatform(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": executable}
	if !IsAskPassInvocation(func(key string) string { return values[key] }) {
		t.Fatal("exact self-executable askpass invocation was not recognized")
	}
}

func TestAskPassSession_CloseRemovesOnlyOwnedHelper(t *testing.T) {
	root := testutil.TempDir(t)
	sentinel := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := NewAskPassSession(root, Credentials{Username: "user", Token: "teamkit-token-canary"})
	if err != nil {
		t.Fatal(err)
	}
	helper := session.Credentials().AskPassPath
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(helper); !os.IsNotExist(err) {
		t.Fatalf("helper remains after Close: %v", err)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("Close altered non-owned file: %q, %v", data, err)
	}
}

func TestNewAskPassSession_RejectsRelativeOrTraversalTempRoot(t *testing.T) {
	validRoot := testutil.TempDir(t)
	traversalRoot := validRoot + string(os.PathSeparator) + ".."
	for _, root := range []string{"", ".", "relative", traversalRoot} {
		session, err := NewAskPassSession(root, Credentials{Username: "user", Token: "teamkit-token-canary"})
		if err == nil || session != nil {
			t.Fatalf("NewAskPassSession(%q) = %#v, %v; want error", root, session, err)
		}
	}
}
