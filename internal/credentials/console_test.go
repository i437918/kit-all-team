package credentials

import (
	"bytes"
	"context"
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
		"AI_TOKEN (ввод виден): ",
		"Токен Jira (ввод виден): ",
		"Токен Confluence (ввод виден): ",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output=%q does not contain %q", output.String(), want)
		}
	}
}

func TestConsoleReaderChooseStoredCredential_ShowsKeyAndNeverStoredValue(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	if _, err := writer.WriteString("1\n2\nreplacement-input-canary\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	reader := NewConsoleReader(input, &output)
	use, err := reader.ChooseStoredCredential(context.Background(), GitLabUsername)
	if err != nil || use != UseStoredCredential {
		t.Fatalf("use=%v err=%v", use, err)
	}
	replace, err := reader.ChooseStoredCredential(context.Background(), GitLabToken)
	if err != nil || replace != ReplaceStoredCredential {
		t.Fatalf("replace=%v err=%v", replace, err)
	}
	value, err := reader.ReadSecret(GitLabToken)
	if err != nil || value != "replacement-input-canary" {
		t.Fatalf("value received=%t err=%v", value == "replacement-input-canary", err)
	}
	for _, want := range []string{GitLabUsername + " уже сохранён", GitLabToken + " уже сохранён", "1. Использовать сохранённое значение", "2. Ввести новое значение", "Токен Gitlab (ввод виден):"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output=%q missing %q", output.String(), want)
		}
	}
	for _, forbidden := range []string{"saved-value-canary", "replacement-input-canary"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("program output repeated %q: %q", forbidden, output.String())
		}
	}
}

func TestConsoleReaderUsesAITokenDisplayNameWithoutChangingInternalKey(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	if _, err := writer.WriteString("2\nreplacement\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	reader := NewConsoleReader(input, &output)
	if _, err := reader.ChooseStoredCredential(context.Background(), PublicProviderAPIKey); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadSecret(PublicProviderAPIKey); err != nil {
		t.Fatal(err)
	}
	if PublicProviderAPIKey != "TEAMKIT_PUBLIC_PROVIDER_API_KEY" {
		t.Fatalf("internal key=%q", PublicProviderAPIKey)
	}
	if strings.Count(output.String(), "AI_TOKEN") != 2 || strings.Contains(output.String(), PublicProviderAPIKey) {
		t.Fatalf("output=%q", output.String())
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
