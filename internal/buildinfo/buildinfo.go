// Package buildinfo exposes immutable metadata injected by release builds.
package buildinfo

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// Info describes the exact source and build used for a teamkit binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

// Current returns the metadata embedded in the running binary.
func Current() Info {
	return Info{Version: version, Commit: commit, BuildDate: buildDate}
}
