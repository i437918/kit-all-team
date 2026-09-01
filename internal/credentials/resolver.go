// Package credentials resolves required secrets from the selected application's
// private environment without exposing values through command arguments or logs.
package credentials

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/secrets"
)

const (
	GitLabUsername   = "GITLAB_USERNAME"
	GitLabToken      = "TEAMKIT_SOURCE_TOKEN"
	GitCAFile        = "GIT_SSL_CAINFO"
	PublicProviderAPIKey = "TEAMKIT_PUBLIC_PROVIDER_API_KEY"
	JiraToken        = "TEAMKIT_PUBLIC_ISSUES_KEY"
	ConfluenceToken  = "TEAMKIT_PUBLIC_WIKI_KEY"
)

// SecretStore is the minimal application-local persistence boundary.
type SecretStore interface {
	Load(keys ...string) (map[string]string, error)
	Save(map[string]string) (string, error)
}

// StoreFactory opens the private secret store for one application home.
type StoreFactory func(applicationHome string) (SecretStore, error)

// HomeResolver returns the selected application's absolute private directory.
type HomeResolver func(domain.DesiredState) (string, error)

// SecretReader obtains one masked value from an interactive input.
type SecretReader interface {
	ReadSecret(label string) (string, error)
}

// ContextSecretReader permits cancellation while a terminal or pipe is
// blocked waiting for a secret.
type ContextSecretReader interface {
	ReadSecretContext(context.Context, string) (string, error)
}

// Resolver loads existing values and prompts only for required missing values.
var hermesCredentials = []string{GitLabUsername, GitLabToken, PublicProviderAPIKey, JiraToken, ConfluenceToken}

// StoredCredentialAction selects how an existing stored credential is handled.
type StoredCredentialAction uint8

const (
	// UseStoredCredential keeps the existing stored credential.
	UseStoredCredential StoredCredentialAction = iota + 1
	// ReplaceStoredCredential prompts for and stores a replacement credential.
	ReplaceStoredCredential
)

// StoredCredentialChooser obtains the user's choice for an existing credential.
type StoredCredentialChooser interface {
	ChooseStoredCredential(context.Context, string) (StoredCredentialAction, error)
}
type Resolver struct {
	Home    HomeResolver
	Store   StoreFactory
	Reader  SecretReader
	Chooser StoredCredentialChooser
}

// Resolve implements cli.CredentialSource.
func (r Resolver) Resolve(ctx context.Context, desired domain.DesiredState, interactive bool) (map[string]string, error) {
	required := []string{GitLabUsername, GitLabToken}
	if desired.Application() == domain.AppHermes {
		required = append(required, PublicProviderAPIKey, JiraToken, ConfluenceToken)
	}
	return r.resolve(ctx, desired, required, required, interactive)
}

// ResolveForPlan loads and prompts only for credentials needed by actions. A
// configured CA file is optional and therefore never prompts when absent.
func (r Resolver) ResolveForPlan(ctx context.Context, desired domain.DesiredState, actions []reconcile.Action, interactive bool) (map[string]string, error) {
	gitNeeded, providerNeeded := false, false
	for _, action := range actions {
		switch action.Kind {
		case reconcile.ActionSyncContent, reconcile.ActionSyncDatabase:
			gitNeeded = true
		case reconcile.ActionConfigureApplication:
			providerNeeded = desired.Application() == domain.AppHermes
		}
	}
	if !gitNeeded && !providerNeeded {
		return map[string]string{}, nil
	}
	if interactive && desired.Application() == domain.AppHermes {
		required := append([]string(nil), hermesCredentials...)
		load := []string{GitLabUsername, GitLabToken, GitCAFile, PublicProviderAPIKey, JiraToken, ConfluenceToken}
		return r.resolve(ctx, desired, required, load, true)
	}
	required := make([]string, 0, 5)
	load := make([]string, 0, 6)
	if gitNeeded {
		required = append(required, GitLabUsername, GitLabToken)
		load = append(load, GitLabUsername, GitLabToken, GitCAFile)
	}
	if providerNeeded {
		required = append(required, PublicProviderAPIKey, JiraToken, ConfluenceToken)
		load = append(load, PublicProviderAPIKey, JiraToken, ConfluenceToken)
	}
	return r.resolve(ctx, desired, required, load, interactive)
}

func (r Resolver) resolve(ctx context.Context, desired domain.DesiredState, required, load []string, interactive bool) (map[string]string, error) {
	homeResolver := r.Home
	if homeResolver == nil {
		homeResolver = DefaultApplicationHome
	}
	home, err := homeResolver(desired)
	if err != nil {
		return nil, err
	}
	if err := validateApplicationHome(home, desired.KitHome()); err != nil {
		return nil, err
	}
	storeFactory := r.Store
	if storeFactory == nil {
		storeFactory = func(path string) (SecretStore, error) { return secrets.NewStore(path) }
	}
	store, err := storeFactory(home)
	if err != nil {
		return nil, err
	}
	values, err := store.Load(load...)
	if err != nil {
		return nil, err
	}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		if strings.TrimSpace(values[key]) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 && !interactive {
		return nil, fmt.Errorf("CREDENTIALS_REQUIRED: %s", strings.Join(missing, ","))
	}
	if len(missing) > 0 && r.Reader == nil {
		return nil, fmt.Errorf("CREDENTIAL_READER_REQUIRED: %s", strings.Join(missing, ","))
	}
	changes := map[string]string{}
	for _, key := range required {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		existing := strings.TrimSpace(values[key]) != ""
		replace := !existing
		if existing && interactive && desired.Application() == domain.AppHermes {
			if r.Chooser == nil {
				return nil, fmt.Errorf("CREDENTIAL_CHOOSER_REQUIRED: %s", key)
			}
			action, err := r.Chooser.ChooseStoredCredential(ctx, key)
			if err != nil {
				return nil, err
			}
			if action != UseStoredCredential && action != ReplaceStoredCredential {
				return nil, fmt.Errorf("CREDENTIAL_ACTION_INVALID: %s", key)
			}
			replace = action == ReplaceStoredCredential
		}
		if !replace {
			continue
		}
		if r.Reader == nil {
			return nil, fmt.Errorf("CREDENTIAL_READER_REQUIRED: %s", key)
		}
		var value string
		var err error
		if contextual, ok := r.Reader.(ContextSecretReader); ok {
			value, err = contextual.ReadSecretContext(ctx, key)
		} else {
			value, err = r.Reader.ReadSecret(key)
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			return nil, fmt.Errorf("CREDENTIAL_INPUT_FAILED: %s", key)
		}
		if value == "" || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("CREDENTIAL_VALUE_INVALID: %s", key)
		}
		values[key], changes[key] = value, value
	}
	if len(changes) > 0 {
		if _, err := store.Save(changes); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func validateApplicationHome(home, kitHome string) error {
	if strings.TrimSpace(home) == "" || !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return fmt.Errorf("APPLICATION_HOME_INVALID")
	}
	if err := pathsafe.ValidateDirectory(home); err != nil {
		return fmt.Errorf("APPLICATION_HOME_UNSAFE: %w", err)
	}
	if err := pathsafe.ValidateRegular(filepath.Join(home, ".env")); err != nil {
		return fmt.Errorf("APPLICATION_HOME_UNSAFE: %w", err)
	}
	overlaps, err := pathsafe.Overlaps(home, kitHome)
	if err != nil {
		return fmt.Errorf("APPLICATION_HOME_UNSAFE: %w", err)
	}
	if overlaps {
		return fmt.Errorf("APPLICATION_HOME_OVERLAPS_KIT: %w", pathsafe.ErrUnsafe)
	}
	return nil
}

// DefaultApplicationHome maps each closed application to its private config directory.
func DefaultApplicationHome(desired domain.DesiredState) (string, error) {
	if desired.Application() == domain.AppHermes {
		if !filepath.IsAbs(desired.HermesHome()) {
			return "", fmt.Errorf("HERMES_HOME_INVALID")
		}
		return filepath.Clean(desired.HermesHome()), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", fmt.Errorf("APPLICATION_HOME_UNAVAILABLE")
	}
	configHome, configErr := os.UserConfigDir()
	if configErr != nil || !filepath.IsAbs(configHome) {
		configHome = filepath.Join(home, ".config")
	}
	var path string
	switch desired.Application() {
	case domain.AppCursor:
		path = filepath.Join(configHome, "Cursor")
	case domain.AppOpenCode:
		path = filepath.Join(configHome, "opencode")
	case domain.AppKiloCode:
		path = filepath.Join(configHome, "kilo-code")
	case domain.AppClaudeCode:
		path = filepath.Join(home, ".claude")
	case domain.AppCodex:
		path = filepath.Join(home, ".codex")
	case domain.AppKimi:
		path = filepath.Join(home, ".kimi")
	case domain.AppQwen:
		path = filepath.Join(home, ".qwen")
	case domain.AppCommandCode:
		path = filepath.Join(home, ".command-code")
	case domain.AppCline:
		path = filepath.Join(home, ".cline")
	case domain.AppPi:
		path = filepath.Join(home, ".pi")
	default:
		return "", domain.NewValidationError(domain.ApplicationUnknown, "application", "")
	}
	return filepath.Clean(path), nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
