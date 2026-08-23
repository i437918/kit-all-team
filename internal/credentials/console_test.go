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
	for _, key := range []string{GitLabUsername, GitLabToken, PublicProviderAPIKey, "TEAMKIT_PUBLIC_ISSUES_KEY", "TEAMKIT_PUBLIC_WIKI_KEY"} {
		if _, err := reader.ReadSecret(key); err != nil {
			t.Fatal(err)
		}
	}

	for _, want := range []string{
		"Логин GitLab (GITLAB_USERNAME) (ввод скрыт): ",
		"Токен GitLab (TEAMKIT_SOURCE_TOKEN) (ввод скрыт): ",
		"Ключ PublicProvider (TEAMKIT_PUBLIC_PROVIDER_API_KEY) (ввод скрыт): ",
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
