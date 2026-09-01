package gitx

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

// AskPassSession owns one temporary token-free Git askpass helper.
type AskPassSession struct {
	helperPath  string
	credentials Credentials
}

const gitLabAskPassHost = "source.example.invalid"

// NewAskPassSession creates a helper directly below tempRoot. It reads only the
// username and token supplied by Git's child-process environment.
func NewAskPassSession(tempRoot string, credentials Credentials) (*AskPassSession, error) {
	return newAskPassSessionWithExecutable(tempRoot, credentials, "")
}

func newAskPassSessionWithExecutable(tempRoot string, credentials Credentials, executable string) (*AskPassSession, error) {
	if strings.TrimSpace(tempRoot) == "" || !filepath.IsAbs(tempRoot) || filepath.Clean(tempRoot) != tempRoot {
		return nil, fmt.Errorf("askpass temp root must be a clean absolute path")
	}
	info, err := os.Stat(tempRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("askpass temp root is not a directory")
	}
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	helper, err := privatefile.CreateTemp(tempRoot, "teamkit-askpass-", extension, 0o700)
	if err != nil {
		return nil, err
	}
	helperPath := helper.Name()
	cleanup := func(cause error) (*AskPassSession, error) {
		helper.Close()
		_ = os.Remove(helperPath)
		return nil, cause
	}
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return cleanup(err)
		}
	}
	source, err := os.Open(executable)
	if err != nil {
		return cleanup(err)
	}
	sourceInfo, statErr := source.Stat()
	if statErr != nil || !sourceInfo.Mode().IsRegular() {
		_ = source.Close()
		if statErr != nil {
			return cleanup(statErr)
		}
		return cleanup(fmt.Errorf("askpass executable is not a regular file"))
	}
	_, copyErr := io.Copy(helper, source)
	closeErr := source.Close()
	if copyErr != nil {
		return cleanup(copyErr)
	}
	if closeErr != nil {
		return cleanup(closeErr)
	}
	if err := helper.Sync(); err != nil {
		return cleanup(err)
	}
	if err := helper.Close(); err != nil {
		_ = os.Remove(helperPath)
		return nil, err
	}
	credentials.AskPassPath = helperPath
	return &AskPassSession{helperPath: helperPath, credentials: credentials}, nil
}

// IsAskPassInvocation reports whether the current Windows process was started
// by Git as the private askpass executable owned by this process.
func IsAskPassInvocation(getenv func(string) string) bool {
	if getenv == nil || getenv("GIT_TERMINAL_PROMPT") != "0" {
		return false
	}
	helper := getenv("GIT_ASKPASS")
	if helper == "" || !filepath.IsAbs(helper) {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(helper), filepath.Clean(executable))
}

// RunAskPass answers one validated Git credential prompt without logging or
// placing credential values in command-line arguments or files.
func RunAskPass(args []string, getenv func(string) string, output io.Writer) int {
	if len(args) != 1 || getenv == nil || output == nil {
		return 2
	}
	username := getenv("TEAMKIT_GIT_USERNAME")
	token := getenv("TEAMKIT_GIT_TOKEN")
	answer, ok := askPassAnswer(args[0], username, token)
	if !ok {
		return 2
	}
	if _, err := io.WriteString(output, answer+"\n"); err != nil {
		return 3
	}
	return 0
}

// Credentials returns the environment-only credentials associated with this helper.
func (s *AskPassSession) Credentials() Credentials { return s.credentials }

// Close removes only the helper file owned by this session. It is idempotent.
func (s *AskPassSession) Close() error {
	if s == nil || s.helperPath == "" {
		return nil
	}
	path := s.helperPath
	s.helperPath = ""
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func askPassAnswer(prompt, username, token string) (string, bool) {
	const suffix = "': "
	typePrompt := ""
	target := ""
	for kind, prefix := range map[string]string{
		"username": "Username for '",
		"password": "Password for '",
	} {
		if strings.HasPrefix(prompt, prefix) && strings.HasSuffix(prompt, suffix) {
			typePrompt = kind
			target = strings.TrimSuffix(strings.TrimPrefix(prompt, prefix), suffix)
			break
		}
	}
	parsed, err := url.Parse(target)
	if err != nil || strings.ContainsAny(target, "?#") || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), gitLabAskPassHost) ||
		parsed.Port() != "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	switch typePrompt {
	case "username":
		if username == "" || parsed.User != nil || target != "https://"+gitLabAskPassHost {
			return "", false
		}
		return username, true
	case "password":
		if token == "" || parsed.User == nil || parsed.User.Username() != username {
			return "", false
		}
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return "", false
		}
		return token, true
	default:
		return "", false
	}
}
