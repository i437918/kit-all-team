package credentials

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ConsoleReader masks secret input on a terminal and supports piped test input.
type ConsoleReader struct {
	input  *os.File
	output io.Writer
	lines  *bufio.Reader
}

// NewConsoleReader binds the process terminal used for credential prompts.
func NewConsoleReader(input *os.File, output io.Writer) *ConsoleReader {
	return NewConsoleReaderWithBufferedInput(input, output, bufio.NewReader(input))
}

// NewConsoleReaderWithBufferedInput binds credential prompts to a buffered
// input shared with the interactive questionnaire.
func NewConsoleReaderWithBufferedInput(input *os.File, output io.Writer, lines *bufio.Reader) *ConsoleReader {
	return &ConsoleReader{input: input, output: output, lines: lines}
}

// ReadSecret reads one value without echo when the input is a terminal.
func (r *ConsoleReader) ReadSecret(label string) (string, error) {
	return r.readSecret(label)
}

// ReadSecretContext returns promptly on cancellation even while terminal or
// pipe input is blocked.
func (r *ConsoleReader) ReadSecretContext(ctx context.Context, label string) (string, error) {
	type result struct {
		value string
		err   error
	}
	completed := make(chan result, 1)
	go func() {
		value, err := r.readSecret(label)
		completed <- result{value: value, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case got := <-completed:
		return got.value, got.err
	}
}

func (r *ConsoleReader) readSecret(label string) (string, error) {
	if r == nil || r.input == nil || r.output == nil {
		return "", fmt.Errorf("credential console is unavailable")
	}
	if _, err := fmt.Fprintf(r.output, "%s (ввод скрыт): ", displayCredentialLabel(label)); err != nil {
		return "", err
	}
	if term.IsTerminal(int(r.input.Fd())) {
		value, err := term.ReadPassword(int(r.input.Fd()))
		fmt.Fprintln(r.output)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(value)), nil
	}
	value, err := r.lines.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func displayCredentialLabel(label string) string {
	switch label {
	case GitLabUsername:
		return "Логин GitLab (GITLAB_USERNAME)"
	case GitLabToken:
		return "Токен GitLab (TEAMKIT_SOURCE_TOKEN)"
	case PublicProviderAPIKey:
		return "Ключ PublicProvider (TEAMKIT_PUBLIC_PROVIDER_API_KEY)"
	case JiraToken:
		return "Jira personal token"
	case ConfluenceToken:
		return "Confluence personal token"
	default:
		return label
	}
}
