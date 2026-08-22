package credentials

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestConsoleReaderUsesRussianCredentialLabels(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	if _, err := writer.WriteString("user\ntoken\nprovider-key\njira-token\nconfluence-token\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	reader := NewConsoleReader(input, &output)
	for _, key := range []string{GitLabUsername, GitLabToken, CustomLLMAPIKey, "HERMES_CUSTOM_ISSUE_TRACKER_TOKEN", "HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN"} {
		if _, err := reader.ReadSecret(key); err != nil {
			t.Fatal(err)
		}
	}

	for _, want := range []string{
		"Логин GitLab (GITLAB_USERNAME) (ввод скрыт): ",
		"Токен GitLab (GITLAB_TOKEN) (ввод скрыт): ",
		"Ключ CustomLLM (HERMES_CUSTOM_LLM_API_KEY) (ввод скрыт): ",
		"Jira personal token (ввод скрыт): ",
		"Confluence personal token (ввод скрыт): ",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output=%q does not contain %q", output.String(), want)
		}
	}
}

func TestConsoleReaderPreservesUnknownCredentialLabel(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	if _, err := writer.WriteString("value\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	reader := NewConsoleReader(input, &output)
	if _, err := reader.ReadSecret("CUSTOM_SECRET"); err != nil {
		t.Fatal(err)
	}
	if output.String() != "CUSTOM_SECRET (ввод скрыт): " {
		t.Fatalf("output=%q", output.String())
	}
}
