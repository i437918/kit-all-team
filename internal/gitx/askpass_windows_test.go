//go:build windows

package gitx

import (
	"fmt"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestNewAskPassSession_WindowsHelperHasProtectedOwnerOnlyDACL(t *testing.T) {
	session, err := NewAskPassSession(testutil.TempDir(t), Credentials{Username: "user", Token: "teamkit-token-canary"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	assertAskPassOwnerOnlyDACL(t, session.Credentials().AskPassPath)
}

func TestNewAskPassSession_WindowsExecutableReturnsMetacharacterCredentialsExactly(t *testing.T) {
	root := testutil.TempDir(t)
	sideEffect := filepath.Join(root, "askpass-command-injection-canary")
	username := "user&|><^%USERNAME%"
	token := "token&echo injected>" + sideEffect + "|<^%PATH%"
	executable := buildTeamKitAskPassTestBinary(t)
	session, err := newAskPassSessionWithExecutable(root, Credentials{Username: username, Token: token}, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	helper := session.Credentials().AskPassPath
	if filepath.Ext(helper) != ".exe" {
		t.Fatalf("helper extension = %q; want .exe", filepath.Ext(helper))
	}

	command := exec.Command("git", "-c", "credential.helper=", "credential", "fill")
	command.Env = askPassTestEnvironment(helper, username, token)
	command.Stdin = strings.NewReader("protocol=https\nhost=" + gitLabAskPassHost + "\n\n")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("Git credential flow failed: %v", err)
	}
	actual := make(map[string]string)
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			actual[key] = value
		}
	}
	if actual["username"] != username || actual["password"] != token {
		t.Fatalf("Git did not receive exact metacharacter credentials")
	}
	if _, err := os.Stat(sideEffect); !os.IsNotExist(err) {
		t.Fatalf("metacharacters caused side effect at %q: %v", sideEffect, err)
	}
}

func TestNewAskPassSession_WindowsExecutableRejectsUnexpectedPromptOrHost(t *testing.T) {
	root := testutil.TempDir(t)
	executable := buildTeamKitAskPassTestBinary(t)
	session, err := newAskPassSessionWithExecutable(root, Credentials{Username: "expected-user", Token: "secret-token"}, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	helper := session.Credentials().AskPassPath

	for _, prompt := range []string{
		"Password for 'https://expected-user@" + gitLabAskPassHost + ".attacker.invalid': ",
		"Password for 'https://another-user@" + gitLabAskPassHost + "': ",
		"Username for 'https://" + gitLabAskPassHost + "?': ",
		"Password for 'https://expected-user@" + gitLabAskPassHost + "#': ",
		"Token please",
	} {
		command := exec.Command(helper, prompt)
		command.Env = askPassTestEnvironment(helper, "expected-user", "secret-token")
		output, err := command.CombinedOutput()
		if err == nil {
			t.Fatalf("prompt %q succeeded with output %q", prompt, output)
		}
		if len(output) != 0 {
			t.Fatalf("rejected prompt produced output %q", output)
		}
	}
}

func buildTeamKitAskPassTestBinary(t *testing.T) string {
	t.Helper()
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(testutil.TempDir(t), "teamkit-source.exe")
	goExecutable := filepath.Join(runtime.GOROOT(), "bin", "go.exe")
	command := exec.Command(goExecutable, "build", "-o", target, "./cmd/teamkit")
	command.Dir = repositoryRoot
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build Team Kit askpass executable: %v\n%s", err, output)
	}
	return target
}

func askPassTestEnvironment(helper, username, token string) []string {
	result := make([]string, 0, len(os.Environ())+6)
	for _, value := range os.Environ() {
		upper := strings.ToUpper(value)
		if strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "TEAMKIT_GIT_") {
			continue
		}
		result = append(result, value)
	}
	return append(result,
		"GIT_ASKPASS="+helper,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=NUL",
		fmt.Sprintf("TEAMKIT_GIT_USERNAME=%s", username),
		fmt.Sprintf("TEAMKIT_GIT_TOKEN=%s", token),
	)
}

func assertAskPassOwnerOnlyDACL(t *testing.T, path string) {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("DACL for %q inherits access entries", path)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount != 1 {
		if dacl == nil {
			t.Fatalf("DACL for %q is nil", path)
		}
		t.Fatalf("DACL for %q has %d entries; want one owner entry", path, dacl.AceCount)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	entrySID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !entrySID.Equals(user.User.Sid) {
		t.Fatalf("DACL entry is not an owner allow entry")
	}
}
