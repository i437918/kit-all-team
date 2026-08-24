package credentials

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// ConsoleReader reads visible credential input from a terminal or pipe.
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

// ReadSecret reads one visible credential value from the shared console input.
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
	if _, err := fmt.Fprintf(r.output, "%s (ввод виден): ", displayCredentialLabel(label)); err != nil {
		return "", err
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
		return "Логин Gitlab (например: ivan.ivanov)"
	case GitLabToken:
		return "Токен Gitlab"
	case PublicProviderAPIKey:
		return "Токен LLM"
	case JiraToken:
		return "Токен Jira"
	case ConfluenceToken:
		return "Токен Confluence"
	default:
		return label
	}
}
