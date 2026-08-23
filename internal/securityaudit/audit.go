// Package securityaudit produces secret-free release evidence by scanning the
// Git index, reachable Git history, candidate files, and supported archives.
package securityaudit

import (
	"bytes"
	"context"
	"crypto/sha256"
	standardbuildinfo "debug/buildinfo"
	"encoding/hex"
	"fmt"
	teamkitbuildinfo "github.com/mi1man-cmd/kit-all-team/internal/buildinfo"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxInputBytes           = int64(64 << 20)
	maxGitCommandOutput     = int64(64 << 20)
	maxGitObjects           = 1_000_000
	maxGitPaths             = 1_000_000
	maxArchiveItems         = 10_000
	maxArchiveDepth         = 3
	reportSchema            = 1
	releaseCandidateVersion = "v0.1.5"
)

// Options selects the repository and release artifacts to scan. At least one
// repository or artifact path is required.
type Options struct {
	Repository string
	Paths      []string
	Commit     string
	// HistoryRef limits Git history scanning to the exact HEAD commit and its
	// ancestors. Leave empty to retain the repository-wide --all audit.
	HistoryRef string
}

// Report is deterministic, machine-readable evidence. It intentionally omits
// matched values and raw locations; LocationDigest is a one-way identifier.
type Report struct {
	SchemaVersion   int              `json:"schema_version"`
	Passed          bool             `json:"passed"`
	Commit          string           `json:"commit,omitempty"`
	HistoryScope    string           `json:"history_scope,omitempty"`
	HistoryRevision string           `json:"history_revision,omitempty"`
	Scopes          []ScopeReport    `json:"scopes"`
	Findings        []Finding        `json:"findings"`
	Binaries        []BinaryIdentity `json:"binaries,omitempty"`
}

// ScopeReport summarizes coverage without disclosing scanned content.
type ScopeReport struct {
	Name  string `json:"name"`
	Items int64  `json:"items"`
	Bytes int64  `json:"bytes"`
}

// Finding identifies only a detection class and a digest of its location.
type Finding struct {
	Scope          string `json:"scope"`
	Rule           string `json:"rule"`
	LocationDigest string `json:"location_digest"`
}

// BinaryIdentity is the embedded identity and digest of one release candidate binary.
type BinaryIdentity struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Version  string `json:"version"`
	Commit   string `json:"commit"`
}
type auditor struct {
	scopes         map[string]*ScopeReport
	findings       map[string]Finding
	expectedCommit string
	binaries       []BinaryIdentity
}

// Audit scans all selected inputs and returns a deterministic report. Secret
// detections are represented by Report.Passed=false, not by an error.
func Audit(ctx context.Context, options Options) (Report, error) {
	audit := &auditor{scopes: map[string]*ScopeReport{}, findings: map[string]Finding{}}
	commit := strings.TrimSpace(options.Commit)
	repositoryRevision := ""
	if commit != "" && !commitPattern.MatchString(commit) {
		return Report{}, fmt.Errorf("security audit commit is invalid")
	}
	historyRef := strings.TrimSpace(options.HistoryRef)
	if historyRef != "" && strings.TrimSpace(options.Repository) == "" {
		return Report{}, fmt.Errorf("security audit history ref requires a repository")
	}
	if historyRef != "" && !commitPattern.MatchString(historyRef) {
		return Report{}, fmt.Errorf("security audit history ref is invalid")
	}

	if strings.TrimSpace(options.Repository) != "" {
		repository, err := filepath.Abs(options.Repository)
		if err != nil {
			return Report{}, fmt.Errorf("resolve repository: %w", err)
		}
		detectedCommit, err := audit.scanRepository(ctx, filepath.Clean(repository), historyRef)
		if err != nil {
			return Report{}, err
		}
		repositoryRevision = detectedCommit
		if commit != "" && historyRef == "" && commit != detectedCommit {
			return Report{}, fmt.Errorf("security audit commit does not match repository")
		}
		if commit == "" {
			commit = detectedCommit
		}
	}

	audit.expectedCommit = commit
	paths := append([]string(nil), options.Paths...)
	sort.Strings(paths)
	for _, input := range paths {
		if strings.TrimSpace(input) == "" {
			return Report{}, fmt.Errorf("security audit artifact path is empty")
		}
		path, err := filepath.Abs(input)
		if err != nil {
			return Report{}, fmt.Errorf("resolve artifact path: %w", err)
		}
		if err := audit.scanArtifactPath(filepath.Clean(path)); err != nil {
			return Report{}, err
		}
	}
	if len(audit.binaries) > 0 {
		audit.verifyCandidateBinaries()
	}
	if strings.TrimSpace(options.Repository) == "" && len(paths) == 0 {
		return Report{}, fmt.Errorf("security audit input is required")
	}

	report := Report{SchemaVersion: reportSchema, Commit: commit, HistoryRevision: repositoryRevision}
	if strings.TrimSpace(options.Repository) != "" {
		report.HistoryScope = "all_refs"
		if historyRef != "" {
			report.HistoryScope = "exact_commit_ancestry"
		}
	}
	for _, scope := range audit.scopes {
		report.Scopes = append(report.Scopes, *scope)
	}
	sort.Slice(report.Scopes, func(i, j int) bool { return report.Scopes[i].Name < report.Scopes[j].Name })
	for _, finding := range audit.findings {
		report.Findings = append(report.Findings, finding)
	}
	report.Binaries = append(report.Binaries, audit.binaries...)
	sort.Slice(report.Binaries, func(i, j int) bool { return report.Binaries[i].Filename < report.Binaries[j].Filename })
	sort.Slice(report.Findings, func(i, j int) bool {
		left, right := report.Findings[i], report.Findings[j]
		if left.Scope != right.Scope {
			return left.Scope < right.Scope
		}
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		return left.LocationDigest < right.LocationDigest
	})
	report.Passed = len(report.Findings) == 0
	return report, nil
}

func (a *auditor) addCoverage(scope string, items, bytes int64) {
	entry := a.scopes[scope]
	if entry == nil {
		entry = &ScopeReport{Name: scope}
		a.scopes[scope] = entry
	}
	entry.Items += items
	entry.Bytes += bytes
}

func (a *auditor) addFinding(scope, rule, location string) {
	digest := sha256.Sum256([]byte(location))
	finding := Finding{Scope: scope, Rule: rule, LocationDigest: hex.EncodeToString(digest[:8])}
	a.findings[scope+"\x00"+rule+"\x00"+finding.LocationDigest] = finding
}

func (a *auditor) scanContent(scope, location string, data []byte) {
	a.addCoverage(scope, 1, int64(len(data)))
	for _, detector := range secretDetectors {
		if detector.matches(data) {
			a.addFinding(scope, detector.name, location)
		}
	}
}

func (a *auditor) readAndScan(scope, location, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("security audit cannot inspect artifact")
	}
	if !info.Mode().IsRegular() {
		a.addFinding(scope, "unsafe_file_type", location)
		return nil
	}
	if info.Size() > maxInputBytes {
		a.addFinding(scope, "oversized_input", location)
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("security audit cannot read artifact")
	}
	a.inspectCandidateBinary(location, path, data)
	a.scanContent(scope, location, data)
	return a.scanArchive("artifact_archive", location, filepath.Base(path), data, 0)
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

var releaseCandidateBinaryNames = []string{
	"teamkit-v0.1.5-windows-amd64.exe",
	"teamkit-v0.1.5-linux-amd64",
	"teamkit-v0.1.5-darwin-amd64",
	"teamkit-v0.1.5-darwin-arm64",
}

func (a *auditor) inspectCandidateBinary(location, path string, data []byte) {
	filename := filepath.Base(path)
	if !isReleaseCandidateBinary(filename) {
		return
	}
	identity := BinaryIdentity{Filename: filename}
	digest := sha256.Sum256(data)
	identity.SHA256 = hex.EncodeToString(digest[:])
	_, err := standardbuildinfo.ReadFile(path)
	if err != nil {
		a.addFinding("candidate_identity", "embedded_buildinfo", location)
		a.binaries = append(a.binaries, identity)
		return
	}
	identity.Version, identity.Commit = embeddedCandidateIdentity(data)
	if identity.Version != releaseCandidateVersion {
		a.addFinding("candidate_identity", "embedded_version", location)
	}
	if a.expectedCommit == "" || identity.Commit != a.expectedCommit {
		a.addFinding("candidate_identity", "embedded_commit", location)
	}
	a.binaries = append(a.binaries, identity)
}

func (a *auditor) verifyCandidateBinaries() {
	seen := make(map[string]bool, len(a.binaries))
	for _, identity := range a.binaries {
		seen[identity.Filename] = true
	}
	for _, filename := range releaseCandidateBinaryNames {
		if !seen[filename] {
			a.addFinding("candidate_identity", "missing_binary", filename)
		}
	}
}

func isReleaseCandidateBinary(filename string) bool {
	for _, candidate := range releaseCandidateBinaryNames {
		if filename == candidate {
			return true
		}
	}
	return false
}

var embeddedVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

func embeddedCandidateIdentity(data []byte) (string, string) {
	marker := []byte(teamkitbuildinfo.EmbeddedIdentityPrefix)
	const defaultIdentity = "dev:unknown"

	var version, commit string
	for offset := 0; offset < len(data); {
		index := bytes.Index(data[offset:], marker)
		if index < 0 {
			break
		}
		start := offset + index + len(marker)
		value := data[start:]
		if bytes.HasPrefix(value, []byte(defaultIdentity)) {
			offset = start + len(defaultIdentity)
			continue
		}

		separator := bytes.IndexByte(value, ':')
		identityEnd := separator + 1 + 40
		if separator <= 0 || separator > 64 || len(value) <= identityEnd || value[identityEnd] != 0 {
			return "", ""
		}
		candidateVersion := value[:separator]
		candidateCommit := value[separator+1 : identityEnd]
		if !embeddedVersionPattern.Match(candidateVersion) || !commitPattern.Match(candidateCommit) || version != "" {
			return "", ""
		}
		version = string(candidateVersion)
		commit = string(candidateCommit)
		offset = start + identityEnd + 1
	}
	return version, commit
}
