// Package hermes materializes the private Hermes configuration and installation
// prerequisites without placing credentials in commands or shared workspace files.
package hermes

import "github.com/mi1man-cmd/kit-all-team/internal/catalog"

// Provider describes an LLM provider configured for a Hermes profile.
type Provider struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Endpoint          string `json:"endpoint"`
	Model             string `json:"model"`
	APIMode           string `json:"api_mode"`
	APIKeyEnvironment string `json:"api_key_environment"`
}

// CustomLLMProvider returns the approved default Hermes provider contract.
func CustomLLMProvider() Provider {
	pinned := catalog.DefaultProvider()
	return Provider{
		ID:                pinned.ID,
		Name:              pinned.Name,
		Endpoint:          pinned.BaseURL,
		Model:             pinned.Model,
		APIMode:           pinned.APIMode,
		APIKeyEnvironment: pinned.APIKeyEnvironment,
	}
}
