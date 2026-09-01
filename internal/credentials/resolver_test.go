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
				PublicProviderAPIKey: "provider", "TEAMKIT_PUBLIC_ISSUES_KEY": "jira-token", "TEAMKIT_PUBLIC_WIKI_KEY": "confluence-token",
			},
			want: []string{PublicProviderAPIKey, "TEAMKIT_PUBLIC_ISSUES_KEY", "TEAMKIT_PUBLIC_WIKI_KEY"},
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
	if err := os.WriteFile(sentinel, []byte("TEAMKIT_SOURCE_TOKEN=teamkit-secret-canary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(home, ".env")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	assertResolverRejectsHomeBeforeSecrets(t, hermesDesired(t), home)
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "TEAMKIT_SOURCE_TOKEN=teamkit-secret-canary\n" {
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
				GitLabUsername: "user", GitLabToken: "token", PublicProviderAPIKey: "provider",
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

type fakeChooser struct {
	actions map[string]StoredCredentialAction
	keys    []string
}

func (c *fakeChooser) ChooseStoredCredential(_ context.Context, key string) (StoredCredentialAction, error) {
	c.keys = append(c.keys, key)
	if action := c.actions[key]; action != 0 {
		return action, nil
	}
	return UseStoredCredential, nil
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
		GitLabUsername: "dmitry.pavlov", GitLabToken: "token", PublicProviderAPIKey: "llm-key", "TEAMKIT_PUBLIC_ISSUES_KEY": "jira-token", "TEAMKIT_PUBLIC_WIKI_KEY": "confluence-token",
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
	if !reflect.DeepEqual(store.loaded, [][]string{{GitLabUsername, GitLabToken, PublicProviderAPIKey, "TEAMKIT_PUBLIC_ISSUES_KEY", "TEAMKIT_PUBLIC_WIKI_KEY"}}) {
		t.Fatalf("loaded=%#v", store.loaded)
	}
}

func TestResolverInteractiveHermes_OffersExactlyFiveStoredKeysInStableOrder(t *testing.T) {
	wantKeys := []string{GitLabUsername, GitLabToken, PublicProviderAPIKey, JiraToken, ConfluenceToken}
	stored := map[string]string{
		GitLabUsername: "saved-user-canary", GitLabToken: "saved-token-canary",
		PublicProviderAPIKey: "saved-provider-canary", JiraToken: "saved-jira-canary",
		ConfluenceToken: "saved-confluence-canary", GitCAFile: "C:/optional/ca.pem",
	}
	store := &fakeStore{values: stored}
	reader := &fakeReader{}
	chooser := &fakeChooser{}
	resolver := Resolver{
		Home:  func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		Store: func(string) (SecretStore, error) { return store, nil }, Reader: reader, Chooser: chooser,
	}
	actions := []reconcile.Action{{Kind: reconcile.ActionSyncContent}, {Kind: reconcile.ActionConfigureApplication}}
	got, err := resolver.ResolveForPlan(context.Background(), hermesDesired(t), actions, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(chooser.keys, wantKeys) {
		t.Fatalf("chooser keys=%v want=%v", chooser.keys, wantKeys)
	}
	if len(reader.labels) != 0 || store.saved != nil {
		t.Fatalf("use-stored prompted or saved: labels=%v saved=%v", reader.labels, store.saved)
	}
	if !reflect.DeepEqual(got, stored) {
		t.Fatalf("values keys=%v want keys=%v", sortedKeys(got), sortedKeys(stored))
	}
	if !reflect.DeepEqual(store.loaded, [][]string{{GitLabUsername, GitLabToken, GitCAFile, PublicProviderAPIKey, JiraToken, ConfluenceToken}}) {
		t.Fatalf("loaded=%v", store.loaded)
	}
}

func TestResolverResolveForPlan_InteractiveHermesTraversesFiveForGitOnlyPlan(t *testing.T) {
	wantKeys := []string{GitLabUsername, GitLabToken, PublicProviderAPIKey, JiraToken, ConfluenceToken}
	stored := map[string]string{
		GitLabUsername: "saved-user", GitLabToken: "saved-token",
		PublicProviderAPIKey: "saved-provider", JiraToken: "saved-jira",
		ConfluenceToken: "saved-confluence", GitCAFile: "C:/optional/ca.pem",
	}
	store := &fakeStore{values: stored}
	chooser := &fakeChooser{}
	resolver := Resolver{
		Home:  func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		Store: func(string) (SecretStore, error) { return store, nil }, Chooser: chooser,
	}
	got, err := resolver.ResolveForPlan(context.Background(), hermesDesired(t), []reconcile.Action{{Kind: reconcile.ActionSyncContent}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(chooser.keys, wantKeys) {
		t.Fatalf("chooser keys=%v want=%v", chooser.keys, wantKeys)
	}
	if !reflect.DeepEqual(store.loaded, [][]string{{GitLabUsername, GitLabToken, GitCAFile, PublicProviderAPIKey, JiraToken, ConfluenceToken}}) {
		t.Fatalf("loaded=%v", store.loaded)
	}
	if !reflect.DeepEqual(got, stored) || store.saved != nil {
		t.Fatalf("values=%v saved=%v", sortedKeys(got), store.saved)
	}
}

func TestResolverInteractiveHermes_UseReplaceAndMissingPersistOnlyChanges(t *testing.T) {
	stored := map[string]string{
		GitLabUsername: "saved-user-canary", GitLabToken: "saved-token-canary",
		JiraToken: "saved-jira-canary", ConfluenceToken: "saved-confluence-canary",
	}
	store := &fakeStore{values: stored}
	reader := &fakeReader{values: []string{"replacement-token-canary", "missing-provider-canary", "replacement-confluence-canary"}}
	chooser := &fakeChooser{actions: map[string]StoredCredentialAction{
		GitLabUsername:  UseStoredCredential,
		GitLabToken:     ReplaceStoredCredential,
		JiraToken:       UseStoredCredential,
		ConfluenceToken: ReplaceStoredCredential,
	}}
	resolver := Resolver{
		Home:  func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		Store: func(string) (SecretStore, error) { return store, nil }, Reader: reader, Chooser: chooser,
	}
	actions := []reconcile.Action{{Kind: reconcile.ActionSyncDatabase}, {Kind: reconcile.ActionConfigureApplication}}
	got, err := resolver.ResolveForPlan(context.Background(), hermesDesired(t), actions, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(chooser.keys, []string{GitLabUsername, GitLabToken, JiraToken, ConfluenceToken}) {
		t.Fatalf("chooser keys=%v", chooser.keys)
	}
	if !reflect.DeepEqual(reader.labels, []string{GitLabToken, PublicProviderAPIKey, ConfluenceToken}) {
		t.Fatalf("reader labels=%v", reader.labels)
	}
	wantSaved := map[string]string{
		GitLabToken:      "replacement-token-canary",
		PublicProviderAPIKey: "missing-provider-canary",
		ConfluenceToken:  "replacement-confluence-canary",
	}
	if !reflect.DeepEqual(store.saved, wantSaved) {
		t.Fatalf("saved=%v want=%v", store.saved, wantSaved)
	}
	if got[GitLabUsername] != stored[GitLabUsername] || got[JiraToken] != stored[JiraToken] {
		t.Fatalf("use-stored values changed: keys=%v", sortedKeys(got))
	}
	if _, prompted := got[GitCAFile]; prompted || containsString(reader.labels, GitCAFile) || containsString(chooser.keys, GitCAFile) {
		t.Fatalf("optional CA entered credential flow: values=%v reader=%v chooser=%v", sortedKeys(got), reader.labels, chooser.keys)
	}
}

func TestResolverInteractiveHermes_NilChooserOrReplacementReaderFailsClosed(t *testing.T) {
	stored := map[string]string{
		GitLabUsername: "saved-user-canary", GitLabToken: "saved-token-canary",
		PublicProviderAPIKey: "saved-provider-canary", JiraToken: "saved-jira-canary",
		ConfluenceToken: "saved-confluence-canary",
	}
	for _, test := range []struct {
		name    string
		chooser StoredCredentialChooser
		want    string
	}{
		{"missing chooser", nil, "CREDENTIAL_CHOOSER_REQUIRED: " + GitLabUsername},
		{"replacement without reader", &fakeChooser{actions: map[string]StoredCredentialAction{GitLabUsername: ReplaceStoredCredential}}, "CREDENTIAL_READER_REQUIRED: " + GitLabUsername},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{values: stored}
			resolver := Resolver{
				Home:  func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
				Store: func(string) (SecretStore, error) { return store, nil }, Chooser: test.chooser,
			}
			_, err := resolver.Resolve(context.Background(), hermesDesired(t), true)
			if err == nil || err.Error() != test.want {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
			if store.saved != nil {
				t.Fatalf("nil dependency persisted values: %v", store.saved)
			}
			for _, canary := range stored {
				if strings.Contains(err.Error(), canary) {
					t.Fatalf("error leaked stored value: %v", err)
				}
			}
		})
	}
}

func TestResolverPromptsMaskedForMissingValuesAndPersistsThem(t *testing.T) {
	store := &fakeStore{values: map[string]string{GitLabUsername: "dmitry.pavlov"}}
	reader := &fakeReader{values: []string{"git-token", "llm-key", "jira-token", "confluence-token"}}
	resolver := Resolver{
		Home:  func(domain.DesiredState) (string, error) { return testutil.TempDir(t), nil },
		Store: func(string) (SecretStore, error) { return store, nil }, Reader: reader, Chooser: &fakeChooser{},
	}
	got, err := resolver.Resolve(context.Background(), hermesDesired(t), true)
	if err != nil {
		t.Fatal(err)
	}
	if got[GitLabToken] != "git-token" || got[PublicProviderAPIKey] != "llm-key" || got["TEAMKIT_PUBLIC_ISSUES_KEY"] != "jira-token" || got["TEAMKIT_PUBLIC_WIKI_KEY"] != "confluence-token" {
		t.Fatalf("resolved values missing: keys=%v", sortedKeys(got))
	}
	if !reflect.DeepEqual(reader.labels, []string{GitLabToken, PublicProviderAPIKey, "TEAMKIT_PUBLIC_ISSUES_KEY", "TEAMKIT_PUBLIC_WIKI_KEY"}) {
		t.Fatalf("prompt labels=%v", reader.labels)
	}
	if _, exists := store.saved[GitLabUsername]; exists || store.saved[GitLabToken] == "" {
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
	want := "CREDENTIALS_REQUIRED: TEAMKIT_PUBLIC_PROVIDER_API_KEY,TEAMKIT_PUBLIC_ISSUES_KEY,TEAMKIT_PUBLIC_WIKI_KEY"
	if err == nil || err.Error() != want {
		t.Fatalf("error=%v, want %q", err, want)
	}
	if strings.Contains(err.Error(), "CANARY_USER") {
		t.Fatalf("error leaked a value: %v", err)
	}
}

func TestResolverAlternativeAppDoesNotRequestPublicProviderKey(t *testing.T) {
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
	for _, key := range []string{PublicProviderAPIKey, "TEAMKIT_PUBLIC_ISSUES_KEY", "TEAMKIT_PUBLIC_WIKI_KEY"} {
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
