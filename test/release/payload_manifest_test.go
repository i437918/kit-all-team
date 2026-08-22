package release_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

type payloadManifest struct {
	OfficeCLI officeCLIPayload `json:"officeCLI"`
}

type officeCLIPayload struct {
	Version string                  `json:"version"`
	Commit  string                  `json:"commit"`
	Assets  []officeCLIPayloadAsset `json:"assets"`
}

type officeCLIPayloadAsset struct {
	OS           domain.OSFamily `json:"os"`
	Architecture string          `json:"architecture"`
	FileName     string          `json:"fileName"`
	URL          string          `json:"url"`
	Size         int64           `json:"size"`
	SHA256       string          `json:"sha256"`
}

type officeCLIPayloadKey struct {
	OS           domain.OSFamily
	Architecture string
}

var qualifiedOfficeCLIPayloadTargets = map[officeCLIPayloadKey]struct{}{
	{OS: domain.OSWindows, Architecture: "amd64"}: {},
	{OS: domain.OSLinux, Architecture: "amd64"}:   {},
	{OS: domain.OSMacOS, Architecture: "amd64"}:   {},
	{OS: domain.OSMacOS, Architecture: "arm64"}:   {},
}

func TestPayloadManifest_OfficeCLIAssetsMatchCatalog(t *testing.T) {
	manifest := readPayloadManifest(t)
	assets, err := validateOfficeCLIAssets(manifest)
	if err != nil {
		t.Fatalf("validate OfficeCLI payload assets: %v", err)
	}

	for _, target := range []struct {
		family       domain.OSFamily
		architecture string
	}{
		{family: domain.OSWindows, architecture: "amd64"},
		{family: domain.OSLinux, architecture: "amd64"},
		{family: domain.OSMacOS, architecture: "amd64"},
		{family: domain.OSMacOS, architecture: "arm64"},
	} {
		catalogAsset, err := catalog.LookupOfficeCLIAsset(target.family, target.architecture)
		if err != nil {
			t.Fatalf("LookupOfficeCLIAsset(%q, %q): %v", target.family, target.architecture, err)
		}
		manifestAsset, ok := assets[target.family][target.architecture]
		if !ok {
			t.Fatalf("payloads.json lacks OfficeCLI asset for %q/%q", target.family, target.architecture)
		}
		if manifest.OfficeCLI.Version != catalogAsset.Version || manifest.OfficeCLI.Commit != catalogAsset.Commit ||
			manifestAsset.FileName != catalogAsset.FileName || manifestAsset.URL != catalogAsset.URL ||
			manifestAsset.Size != catalogAsset.Size || manifestAsset.SHA256 != catalogAsset.SHA256 {
			t.Fatalf("payloads.json OfficeCLI asset %q/%q = %#v with version %q commit %q; want %#v", target.family, target.architecture, manifestAsset, manifest.OfficeCLI.Version, manifest.OfficeCLI.Commit, catalogAsset)
		}
	}
}

func TestPayloadManifest_OfficeCLIAssetsRejectClosedSetViolations(t *testing.T) {
	manifest := readPayloadManifest(t)
	tests := []struct {
		name     string
		mutate   func(*payloadManifest)
		contains string
	}{
		{
			name: "extra unsupported target",
			mutate: func(manifest *payloadManifest) {
				manifest.OfficeCLI.Assets = append(manifest.OfficeCLI.Assets, officeCLIPayloadAsset{OS: domain.OSWindows, Architecture: "arm64"})
			},
			contains: "exactly four",
		},
		{
			name: "unsupported target among four",
			mutate: func(manifest *payloadManifest) {
				manifest.OfficeCLI.Assets[3] = officeCLIPayloadAsset{OS: domain.OSWindows, Architecture: "arm64"}
			},
			contains: "unsupported",
		},
		{
			name: "duplicate target key",
			mutate: func(manifest *payloadManifest) {
				manifest.OfficeCLI.Assets[3] = manifest.OfficeCLI.Assets[0]
			},
			contains: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalid := clonePayloadManifest(manifest)
			tt.mutate(&invalid)
			_, err := validateOfficeCLIAssets(invalid)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("validateOfficeCLIAssets() error = %v, want error containing %q", err, tt.contains)
			}
		})
	}
}

func TestParsePayloadManifest_RejectsInvalidJSON(t *testing.T) {
	if _, err := parsePayloadManifest([]byte(`{"officeCLI":`)); err == nil {
		t.Fatal("parsePayloadManifest() accepted invalid JSON")
	}
}

func TestPayloadManifest_OfficeCLIALTMatchesLinuxAMD64(t *testing.T) {
	linux, err := catalog.LookupOfficeCLIAsset(domain.OSLinux, "amd64")
	if err != nil {
		t.Fatalf("LookupOfficeCLIAsset(linux, amd64): %v", err)
	}
	alt, err := catalog.LookupOfficeCLIAsset(domain.OSALTLinux, "amd64")
	if err != nil {
		t.Fatalf("LookupOfficeCLIAsset(altlinux, amd64): %v", err)
	}
	if alt != linux {
		t.Fatalf("LookupOfficeCLIAsset(altlinux, amd64) = %#v, want Linux asset %#v", alt, linux)
	}
}

func readPayloadManifest(t *testing.T) payloadManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "assets", "payloads.json"))
	if err != nil {
		t.Fatalf("read assets/payloads.json: %v", err)
	}
	manifest, err := parsePayloadManifest(data)
	if err != nil {
		t.Fatalf("parse assets/payloads.json: %v", err)
	}
	return manifest
}

func clonePayloadManifest(manifest payloadManifest) payloadManifest {
	clone := manifest
	clone.OfficeCLI.Assets = append([]officeCLIPayloadAsset(nil), manifest.OfficeCLI.Assets...)
	return clone
}

func parsePayloadManifest(data []byte) (payloadManifest, error) {
	var manifest payloadManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return payloadManifest{}, err
	}
	return manifest, nil
}

func validateOfficeCLIAssets(manifest payloadManifest) (map[domain.OSFamily]map[string]officeCLIPayloadAsset, error) {
	if len(manifest.OfficeCLI.Assets) != len(qualifiedOfficeCLIPayloadTargets) {
		return nil, fmt.Errorf("OfficeCLI assets must contain exactly four qualified targets, got %d", len(manifest.OfficeCLI.Assets))
	}
	assets := make(map[domain.OSFamily]map[string]officeCLIPayloadAsset)
	seen := make(map[officeCLIPayloadKey]struct{}, len(manifest.OfficeCLI.Assets))
	for _, asset := range manifest.OfficeCLI.Assets {
		key := officeCLIPayloadKey{OS: asset.OS, Architecture: asset.Architecture}
		if _, ok := qualifiedOfficeCLIPayloadTargets[key]; !ok {
			return nil, fmt.Errorf("unsupported OfficeCLI target %q/%q", asset.OS, asset.Architecture)
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate OfficeCLI target %q/%q", asset.OS, asset.Architecture)
		}
		seen[key] = struct{}{}
		if assets[asset.OS] == nil {
			assets[asset.OS] = make(map[string]officeCLIPayloadAsset)
		}
		assets[asset.OS][asset.Architecture] = asset
	}
	return assets, nil
}
