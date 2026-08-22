package credentials

import (
	"context"
	"errors"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

type fakeStore struct {
	values map[string]string
	saved  map[string]string
	loaded [][]string
}

func (s *fakeStore) Load(keys ...string) (map[string]string, error) {
	s.loaded = append(s.loaded, append([]string(nil), keys...))
	result := make(map[string]string)
	for _, key := range keys {
		if value := s.values[key]; value != "" {
			result[key] = value
		}
	}
	return result, nil
}

func TestResolverResolveForPlanLoadsOnlyRequiredActionKeys(t *testing.T) {
	desired := hermesDesired(t)
	for _, test := range []struct {
		name    string
		actions []reconcile.Action
		values  map[string]string
		want    []string
	}{
		{
			name: "Hermes provider configuration", actions: []reconcile.Action{{Kind: reconcile.ActionConfigureApplication}},
			values: map[string]string{
				CustomLLMAPIKey: "provider", "HERMES_CUSTOM_ISSUE_TRACKER_TOKEN": "jira-token", "HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN": "confluence-token",
			},
			want: []string{CustomLLMAPIKey, "HERMES_CUSTOM_ISSUE_TRACKER_TOKEN", "HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN"},
		},
		{
			name: "Git sync with optional CA", actions: []reconcile.Action{{Kind: reconcile.ActionSyncContent}},
			values: map[string]string{GitLabUsername: "user", GitLabToken: "token"},
			want:   []string{GitLabUsername, GitLabToken, GitCAFile},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{values: test.values}
			resolver := Resolver{
				Home:  func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
				Store: func(string) (SecretStore, error) { return store, nil },
			}
			values, err := resolver.ResolveForPlan(context.Background(), desired, test.actions, false)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(store.loaded, [][]string{test.want}) || !reflect.DeepEqual(values, test.values) {
				t.Fatalf("loaded=%#v values=%#v", store.loaded, values)
			}
		})
	}
}

func TestResolverResolveForPlanNoActionsDoesNotOpenStore(t *testing.T) {
	opened := 0
	resolver := Resolver{
		Home:  func(domain.DesiredState) (string, error) { opened++; return testutil.TempDir(t), nil },
		Store: func(string) (SecretStore, error) { opened++; return &fakeStore{}, nil },
	}
	values, err := resolver.ResolveForPlan(context.Background(), hermesDesired(t), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 || opened != 0 {
		t.Fatalf("values=%#v opened=%d", values, opened)
	}
}

func TestResolverRejectsRedirectedApplicationHomeBeforeOpeningSecretStore(t *testing.T) {
	external := testutil.TempDir(t)
	root := testutil.TempDir(t)
	home := filepath.Join(root, "application")
	if err := os.Symlink(external, home); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	assertResolverRejectsHomeBeforeSecrets(t, hermesDesired(t), home)
}

func TestResolverRejectsRedirectedSecretFileBeforeOpeningSecretStore(t *testing.T) {
	home := testutil.TempDir(t)
	sentinel := filepath.Join(testutil.TempDir(t), "outside.env")
	if err := os.WriteFile(sentinel, []byte("GITLAB_TOKEN=teamkit-secret-canary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(home, ".env")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	assertResolverRejectsHomeBeforeSecrets(t, hermesDesired(t), home)
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "GITLAB_TOKEN=teamkit-secret-canary\n" {
		t.Fatalf("external secret changed to %q: %v", data, err)
	}
}

func TestResolverRejectsApplicationHomeOverlappingKitBeforeOpeningSecretStore(t *testing.T) {
	desired := hermesDesired(t)
	home := filepath.Join(desired.KitHome(), "private-app")
	assertResolverRejectsHomeBeforeSecrets(t, desired, home)
}

func TestResolverRejectsNonCleanApplicationHomeBeforeOpeningSecretStore(t *testing.T) {
	root := testutil.TempDir(t)
	home := filepath.Join(root, "application") + string(filepath.Separator) + ".." + string(filepath.Separator) + "application"
	assertResolverRejectsHomeBeforeSecrets(t, hermesDesired(t), home)
}

func assertResolverRejectsHomeBeforeSecrets(t *testing.T, desired domain.DesiredState, home string) {
	t.Helper()
	opened := 0
	reader := &fakeReader{values: []string{"must-not-be-read"}}
	resolver := Resolver{
		Home: func(domain.DesiredState) (string, error) { return home, nil },
		Store: func(string) (SecretStore, error) {
			opened++
			return &fakeStore{values: map[string]string{
				GitLabUsername: "user", GitLabToken: "token", CustomLLMAPIKey: "provider",
			}}, nil
		},
		Reader: reader,
	}

	_, err := resolver.Resolve(context.Background(), desired, true)
	if err == nil {
		t.Fatal("unsafe application home was accepted")
	}
	if !errors.Is(err, pathsafe.ErrUnsafe) && !strings.Contains(err.Error(), "APPLICATION_HOME") {
		t.Fatalf("Resolve error = %v; want path-safety rejection", err)
	}
	if opened != 0 || len(reader.labels) != 0 {
		t.Fatalf("unsafe home reached secrets: store opens=%d prompts=%v", opened, reader.labels)
	}
}

func (s *fakeStore) Save(values map[string]string) (string, error) {
	s.saved = values
	return "/app/.env", nil
}

type fakeReader struct {
	values []string
	labels []string
}

type blockingContextReader struct{ entered chan struct{} }

func (blockingContextReader) ReadSecret(string) (string, error) {
	return "", errors.New("context-aware method was not used")
}

func (r blockingContextReader) ReadSecretContext(ctx context.Context, _ string) (string, error) {
	close(r.entered)
	<-ctx.Done()
	return "", ctx.Err()
}

func (r *fakeReader) ReadSecret(label string) (string, error) {
	r.labels = append(r.labels, label)
	if len(r.values) == 0 {
		return "", errors.New("no input")
	}
	value := r.values[0]
	r.values = r.values[1:]
	return value, nil
}

func TestResolverUsesExistingAppLocalSecretsWithoutPrompt(t *testing.T) {
	store := &fakeStore{values: map[string]string{
		GitLabUsername: "dmitry.pavlov", GitLabToken: "token", CustomLLMAPIKey: "llm-key", "HERMES_CUSTOM_ISSUE_TRACKER_TOKEN": "jira-token", "HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN": "confluence-token",
	}}
	reader := &fakeReader{}
	resolver := Resolver{
		Home:  func(domain.DesiredState) (string, error) { return filepath.Join(testutil.TempDir(t), "hermes"), nil },
		Store: func(string) (SecretStore, error) { return store, nil }, Reader: reader,
	}
	got, err := resolver.Resolve(context.Background(), hermesDesired(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, store.values) {
		t.Fatalf("Resolve()=%v want=%v", got, store.values)
	}
	if len(reader.labels) != 0 || store.saved != nil {
		t.Fatalf("unexpected prompt/save: labels=%v saved=%v", reader.labels, store.saved)
	}
	if !reflect.DeepEqual(store.loaded, [][]string{{GitLabUsername, GitLabToken, CustomLLMAPIKey, "HERMES_CUSTOM_ISSUE_TRACKER_TOKEN", "HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN"}}) {
		t.Fatalf("loaded=%#v", store.loaded)
	}
}

func TestResolverPromptsMaskedForMissingValuesAndPersistsThem(t *testing.T) {
	store := &fakeStore{values: map[string]string{GitLabUsername: "dmitry.pavlov"}}
	reader := &fakeReader{values: []string{"git-token", "llm-key", "jira-token", "confluence-token"}}
	resolver := Resolver{
		Home:  func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		Store: func(string) (SecretStore, error) { return store, nil }, Reader: reader,
	}
	got, err := resolver.Resolve(context.Background(), hermesDesired(t), true)
	if err != nil {
		t.Fatal(err)
	}
	if got[GitLabToken] != "git-token" || got[CustomLLMAPIKey] != "llm-key" || got["HERMES_CUSTOM_ISSUE_TRACKER_TOKEN"] != "jira-token" || got["HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN"] != "confluence-token" {
		t.Fatalf("resolved values missing: keys=%v", sortedKeys(got))
	}
	if !reflect.DeepEqual(reader.labels, []string{GitLabToken, CustomLLMAPIKey, "HERMES_CUSTOM_ISSUE_TRACKER_TOKEN", "HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN"}) {
		t.Fatalf("prompt labels=%v", reader.labels)
	}
	if store.saved[GitLabUsername] != "dmitry.pavlov" || store.saved[GitLabToken] == "" {
		t.Fatalf("saved keys=%v", sortedKeys(store.saved))
	}
}

func TestResolverNonInteractiveMissingErrorContainsNamesNotValues(t *testing.T) {
	store := &fakeStore{values: map[string]string{GitLabUsername: "CANARY_USER", GitLabToken: "git-token"}}
	resolver := Resolver{
		Home:  func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		Store: func(string) (SecretStore, error) { return store, nil },
	}
	_, err := resolver.Resolve(context.Background(), hermesDesired(t), false)
	want := "CREDENTIALS_REQUIRED: HERMES_CUSTOM_LLM_API_KEY,HERMES_CUSTOM_ISSUE_TRACKER_TOKEN,HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN"
	if err == nil || err.Error() != want {
		t.Fatalf("error=%v, want %q", err, want)
	}
	if strings.Contains(err.Error(), "CANARY_USER") {
		t.Fatalf("error leaked a value: %v", err)
	}
}

func TestResolverAlternativeAppDoesNotRequestCustomLLMKey(t *testing.T) {
	root := testutil.TempDir(t)
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppCodex, AppInstalled: true,
		KitHome: filepath.Join(root, "kit"), Project: domain.ProjectWMS, Role: domain.RoleDeveloper,
		Toolchain: domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{}
	reader := &fakeReader{values: []string{"dmitry.pavlov", "git-token"}}
	resolver := Resolver{
		Home:  func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		Store: func(string) (SecretStore, error) { return store, nil }, Reader: reader,
	}
	got, err := resolver.Resolve(context.Background(), desired, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{CustomLLMAPIKey, "HERMES_CUSTOM_ISSUE_TRACKER_TOKEN", "HERMES_CUSTOM_KNOWLEDGE_BASE_TOKEN"} {
		if _, exists := got[key]; exists {
			t.Fatalf("alternative app unexpectedly requested %s", key)
		}
	}
	if !reflect.DeepEqual(store.loaded, [][]string{{GitLabUsername, GitLabToken}}) {
		t.Fatalf("alternative app loaded=%#v", store.loaded)
	}
}

func TestResolverCancellationInterruptsBlockedSecretPrompt(t *testing.T) {
	store := &fakeStore{}
	entered := make(chan struct{})
	home := testutil.TempDir(t)
	resolver := Resolver{
		Home:   func(domain.DesiredState) (string, error) { return home, nil },
		Store:  func(string) (SecretStore, error) { return store, nil },
		Reader: blockingContextReader{entered: entered},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := resolver.Resolve(ctx, hermesDesired(t), true)
		done <- err
	}()
	<-entered
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}
	if store.saved != nil {
		t.Fatalf("canceled credentials were persisted: %#v", store.saved)
	}
}

func hermesDesired(t *testing.T) domain.DesiredState {
	t.Helper()
	root := testutil.TempDir(t)
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: filepath.Join(root, "kit"), HermesHome: filepath.Join(root, "hermes"), Project: domain.ProjectWMS,
		Role: domain.RoleDeveloper, Toolchain: domain.ToolchainAIRules1C,
	})
	if err != nil {
		t.Fatal(err)
	}
	return desired
}
