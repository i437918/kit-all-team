package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"runtime"

	"github.com/mi1man-cmd/kit-all-team/internal/bootstrap"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
)

const legacyRC2HermesVersion = "0.20.1"

type operationContract struct {
	SchemaVersion int                          `json:"schema_version"`
	Project       operationProjectContract     `json:"project"`
	Toolchain     operationToolchainContract   `json:"toolchain"`
	Provider      *operationProviderContract   `json:"provider,omitempty"`
	MCP           *operationMCPContract        `json:"mcp,omitempty"`
	MCPServers    []operationMCPServerContract `json:"mcp_servers,omitempty"`
	Hermes        *operationHermesContract     `json:"hermes,omitempty"`
}

type operationProjectContract struct {
	ID                 domain.ProjectID `json:"id"`
	ContentRepository  string           `json:"content_repository"`
	ContentBranch      string           `json:"content_branch"`
	DatabaseRepository string           `json:"database_repository"`
	DatabaseBranch     string           `json:"database_branch"`
}

type operationToolchainContract struct {
	ID     domain.Toolchain `json:"id"`
	Origin string           `json:"origin"`
	Commit string           `json:"commit"`
}

type operationProviderContract struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	BaseURL           string `json:"base_url"`
	Model             string `json:"model"`
	APIMode           string `json:"api_mode"`
	APIKeyEnvironment string `json:"api_key_environment"`
}

type operationMCPContract struct {
	ID       string `json:"id"`
	Endpoint string `json:"endpoint"`
}

type operationMCPServerContract struct {
	ID                        string                           `json:"id"`
	Endpoint                  string                           `json:"endpoint,omitempty"`
	Headers                   map[string]string                `json:"headers,omitempty"`
	ConnectTimeout            int                              `json:"connect_timeout,omitempty"`
	Timeout                   int                              `json:"timeout,omitempty"`
	SamplingEnabled           *bool                            `json:"sampling_enabled,omitempty"`
	SupportsParallelToolCalls *bool                            `json:"supports_parallel_tool_calls,omitempty"`
	Command                   string                           `json:"command,omitempty"`
	Args                      []string                         `json:"args,omitempty"`
	Asset                     *operationOfficeCLIAssetContract `json:"asset,omitempty"`
}

type operationOfficeCLIAssetContract struct {
	Version            string          `json:"version"`
	Commit             string          `json:"commit"`
	OS                 domain.OSFamily `json:"os"`
	Architecture       string          `json:"architecture"`
	FileName           string          `json:"file_name"`
	URL                string          `json:"url"`
	Size               int64           `json:"size"`
	SHA256             string          `json:"sha256"`
	UpdatePolicy       string          `json:"update_policy"`
	SkillRefreshPolicy string          `json:"skill_refresh_policy"`
}

type operationHermesContract struct {
	Mode                    string                      `json:"mode"`
	MinimumVersion          string                      `json:"minimum_version,omitempty"`
	MaximumExclusiveVersion string                      `json:"maximum_exclusive_version,omitempty"`
	ObservedVersion         string                      `json:"observed_version,omitempty"`
	SourceCommit            string                      `json:"source_commit,omitempty"`
	Installer               *operationInstallerContract `json:"installer,omitempty"`
	CertificateSHA256       string                      `json:"certificate_sha256"`
}

// legacyRC2OperationContract is the closed operation identity emitted by
// v0.1.0-rc.2. Keep this shape private and immutable: it exists only to prove
// that one interrupted RC2 operation still binds the exact current catalog
// pins before it may be resumed.
type legacyRC2OperationContract struct {
	SchemaVersion int                        `json:"schema_version"`
	Project       operationProjectContract   `json:"project"`
	Toolchain     operationToolchainContract `json:"toolchain"`
	Provider      *operationProviderContract `json:"provider,omitempty"`
	MCP           operationMCPContract       `json:"mcp"`
	Hermes        *legacyRC2HermesContract   `json:"hermes,omitempty"`
}

type legacyRC2HermesContract struct {
	Mode              string `json:"mode"`
	CompatibleVersion string `json:"compatible_version,omitempty"`
	CertificateSHA256 string `json:"certificate_sha256"`
}

type operationInstallerContract struct {
	Kind   string `json:"kind"`
	URL    string `json:"url,omitempty"`
	SHA256 string `json:"sha256"`
	Commit string `json:"commit,omitempty"`
	Signer string `json:"signer,omitempty"`
}

func (s *Service) operationContract() OperationContractResolver {
	if s.options.OperationContract != nil {
		return s.options.OperationContract
	}
	return defaultOperationContract
}

func defaultOperationContract(desired domain.DesiredState) (string, error) {
	project, err := catalog.LookupProject(desired.Project())
	if err != nil {
		return "", err
	}
	toolchain, err := catalog.LookupToolchain(desired.Toolchain())
	if err != nil {
		return "", err
	}
	contract := operationContract{
		SchemaVersion: 1,
		Project: operationProjectContract{
			ID: project.ID, ContentRepository: project.ContentRepository, ContentBranch: project.ContentBranch,
			DatabaseRepository: project.DatabaseRepository, DatabaseBranch: project.DatabaseBranch,
		},
		Toolchain: operationToolchainContract{ID: toolchain.ID, Origin: toolchain.Origin, Commit: toolchain.Commit},
	}
	if desired.Application() == domain.AppHermes {
		v8std := catalog.V8StdMCP()
		contract.MCPServers = []operationMCPServerContract{{ID: v8std.ID, Endpoint: v8std.Endpoint}}
		disabled := false
		for _, mcp := range catalog.AtlassianMCPs() {
			headers := make(map[string]string, len(mcp.Headers))
			for key, value := range mcp.Headers {
				headers[key] = value
			}
			contract.MCPServers = append(contract.MCPServers, operationMCPServerContract{
				ID: mcp.ID, Endpoint: mcp.Endpoint, Headers: headers,
				ConnectTimeout: mcp.ConnectTimeout, Timeout: mcp.Timeout,
				SamplingEnabled: &disabled, SupportsParallelToolCalls: &disabled,
			})
		}
		officeCLIAsset, err := resolveOfficeCLIAsset(desired.OS(), runtime.GOARCH)
		if err != nil {
			return "", err
		}
		officeCLIPath, err := officeCLIManagedPath(desired.HermesHome(), officeCLIAsset.Version)
		if err != nil {
			return "", err
		}
		contract.MCPServers = append(contract.MCPServers, operationMCPServerContract{
			ID: "officecli", Command: officeCLIPath, Args: []string{"mcp"},
			Asset: &operationOfficeCLIAssetContract{
				Version: officeCLIAsset.Version, Commit: officeCLIAsset.Commit, OS: officeCLIAsset.OS,
				Architecture: officeCLIAsset.Architecture, FileName: officeCLIAsset.FileName, URL: officeCLIAsset.URL,
				Size: officeCLIAsset.Size, SHA256: officeCLIAsset.SHA256,
				UpdatePolicy: "auto_update_disabled_user_config", SkillRefreshPolicy: "existing_installed_only_best_effort",
			},
		})
		provider := catalog.DefaultProvider()
		contract.Provider = &operationProviderContract{
			ID: provider.ID, Name: provider.Name, BaseURL: provider.BaseURL, Model: provider.Model,
			APIMode: provider.APIMode, APIKeyEnvironment: provider.APIKeyEnvironment,
		}
		installer := &operationInstallerContract{
			Kind: "posix-script", URL: POSIXInstallerURL, SHA256: POSIXInstallerSHA256, Commit: POSIXInstallerCommit,
		}
		if desired.OS() == domain.OSWindows {
			installer = &operationInstallerContract{
				Kind: "windows-exe", SHA256: WindowsInstallerSHA256, Signer: WindowsInstallerSigner,
			}
		}
		contract.Hermes = &operationHermesContract{
			Mode: "managed-pinned-source", SourceCommit: hermes.HermesSourceCommit, Installer: installer,
			CertificateSHA256: bootstrap.DefaultCertificateSHA256,
		}
	} else {
		mcp := catalog.V8StdMCP()
		contract.MCP = &operationMCPContract{ID: mcp.ID, Endpoint: mcp.Endpoint}
	}
	if desired.Application() == domain.AppHermes && desired.AppInstalled() {
		contract.Hermes = &operationHermesContract{
			Mode: "external-compatible", MinimumVersion: hermes.HermesMinimumVersion,
			MaximumExclusiveVersion: hermes.HermesMaximumExclusiveVersion, ObservedVersion: desired.HermesVersion(),
			CertificateSHA256: bootstrap.DefaultCertificateSHA256,
		}
	}
	data, err := json.Marshal(contract)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func legacyRC2InstalledHermesContract(desired domain.DesiredState) (string, error) {
	if desired.Application() != domain.AppHermes || !desired.AppInstalled() || desired.HermesVersion() != "" {
		return "", nil
	}
	project, err := catalog.LookupProject(desired.Project())
	if err != nil {
		return "", err
	}
	toolchain, err := catalog.LookupToolchain(desired.Toolchain())
	if err != nil {
		return "", err
	}
	provider := catalog.DefaultProvider()
	mcp := catalog.V8StdMCP()
	contract := legacyRC2OperationContract{
		SchemaVersion: 1,
		Project: operationProjectContract{
			ID: project.ID, ContentRepository: project.ContentRepository, ContentBranch: project.ContentBranch,
			DatabaseRepository: project.DatabaseRepository, DatabaseBranch: project.DatabaseBranch,
		},
		Toolchain: operationToolchainContract{ID: toolchain.ID, Origin: toolchain.Origin, Commit: toolchain.Commit},
		Provider: &operationProviderContract{
			ID: provider.ID, Name: provider.Name, BaseURL: provider.BaseURL, Model: provider.Model,
			APIMode: provider.APIMode, APIKeyEnvironment: provider.APIKeyEnvironment,
		},
		MCP: operationMCPContract{ID: mcp.ID, Endpoint: mcp.Endpoint},
		Hermes: &legacyRC2HermesContract{
			Mode: "external-compatible", CompatibleVersion: legacyRC2HermesVersion,
			CertificateSHA256: bootstrap.DefaultCertificateSHA256,
		},
	}
	data, err := json.Marshal(contract)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
