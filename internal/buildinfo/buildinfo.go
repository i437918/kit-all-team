// Package buildinfo exposes immutable metadata injected by release builds.
package buildinfo

// EmbeddedIdentityPrefix identifies the immutable version-and-commit record
// injected into every release binary for offline cross-platform inspection.
const EmbeddedIdentityPrefix = "teamkit-build-identity-v1:"

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
	identity  = EmbeddedIdentityPrefix + "dev:unknown"
)

// Info describes the exact source and build used for a teamkit binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	identity  string
}

// Current returns the metadata embedded in the running binary.
func Current() Info {
	return Info{Version: version, Commit: commit, BuildDate: buildDate, identity: identity}
}
