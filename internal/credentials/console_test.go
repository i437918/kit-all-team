package credentials

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestConsoleReaderWarnsThatCredentialInputIsVisible(t *testing.T) {
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
		"Логин Gitlab (например: ivan.ivanov) (ввод виден): ",
		"Токен Gitlab (ввод виден): ",
		"Токен LLM (ввод виден): ",
		"Токен Jira (ввод виден): ",
		"Токен Confluence (ввод виден): ",
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
	if output.String() != "CUSTOM_SECRET (ввод виден): " {
		t.Fatalf("output=%q", output.String())
	}
}
