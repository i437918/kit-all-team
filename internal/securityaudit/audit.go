// Package securityaudit produces secret-free release evidence by scanning the
// Git index, reachable Git history, candidate files, and supported archives.
package securityaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxInputBytes       = int64(64 << 20)
	maxGitCommandOutput = int64(64 << 20)
	maxGitObjects       = 1_000_000
	maxGitPaths         = 1_000_000
	maxArchiveItems     = 10_000
	maxArchiveDepth     = 3
	reportSchema        = 1
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
	SchemaVersion int           `json:"schema_version"`
	Passed        bool          `json:"passed"`
	Commit        string        `json:"commit,omitempty"`
	HistoryScope  string        `json:"history_scope,omitempty"`
	Scopes        []ScopeReport `json:"scopes"`
	Findings      []Finding     `json:"findings"`
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

type auditor struct {
	scopes   map[string]*ScopeReport
	findings map[string]Finding
}

// Audit scans all selected inputs and returns a deterministic report. Secret
// detections are represented by Report.Passed=false, not by an error.
func Audit(ctx context.Context, options Options) (Report, error) {
	audit := &auditor{scopes: map[string]*ScopeReport{}, findings: map[string]Finding{}}
	commit := strings.TrimSpace(options.Commit)
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
		if commit != "" && commit != detectedCommit {
			return Report{}, fmt.Errorf("security audit commit does not match repository")
		}
		commit = detectedCommit
	}

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
	if strings.TrimSpace(options.Repository) == "" && len(paths) == 0 {
		return Report{}, fmt.Errorf("security audit input is required")
	}

	report := Report{SchemaVersion: reportSchema, Commit: commit}
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
	a.scanContent(scope, location, data)
	return a.scanArchive("artifact_archive", location, filepath.Base(path), data, 0)
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
