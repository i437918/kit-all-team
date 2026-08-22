// Command handoff copies a verified immutable GitHub candidate artifact into
// a kept GitLab artifact using only the Go standard library.
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultGitHubAPI  = "https://api.github.com"
	defaultRepository = "i437918/kit-all-team"
	version           = "v0.1.5"
)

var requiredFiles = []string{
	"teamkit-v0.1.5-windows-amd64.exe",
	"teamkit-v0.1.5-linux-amd64",
	"teamkit-v0.1.5-darwin-amd64",
	"teamkit-v0.1.5-darwin-arm64",
	"SHA256SUMS",
	"SECURITY-AUDIT.json",
}

type artifactMetadata struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Digest      string `json:"digest"`
	WorkflowRun struct {
		ID      int64  `json:"id"`
		HeadSHA string `json:"head_sha"`
	} `json:"workflow_run"`
}

type manifest struct {
	Commit               string `json:"commit"`
	Version              string `json:"version"`
	GitHubRunID          int64  `json:"github_run_id"`
	GitHubArtifactID     int64  `json:"github_artifact_id"`
	GitHubArtifactDigest string `json:"github_artifact_digest"`
	SHA256File           string `json:"sha256_file"`
}

type config struct {
	commit     string
	runID      int64
	artifactID int64
	digest     string
	token      string
	githubAPI  string
	repository string
	outputDir  string
}

func main() {
	if err := run(context.Background(), loadConfig()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadConfig() config {
	return config{
		commit:     os.Getenv("CI_COMMIT_SHA"),
		runID:      parseID("GITHUB_RUN_ID"),
		artifactID: parseID("GITHUB_ARTIFACT_ID"),
		digest:     os.Getenv("GITHUB_ARTIFACT_DIGEST"),
		token:      os.Getenv("GITHUB_HANDOFF_TOKEN"),
		githubAPI:  envOr("GITHUB_API_BASE_URL", defaultGitHubAPI),
		repository: envOr("GITHUB_REPOSITORY", defaultRepository),
		outputDir:  envOr("GITLAB_HANDOFF_DIR", "handoff"),
	}
}

func parseID(key string) int64 {
	var value int64
	_, _ = fmt.Sscan(os.Getenv(key), &value)
	return value
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func run(ctx context.Context, cfg config) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	client := &http.Client{}
	metadataURL := fmt.Sprintf("%s/repos/%s/actions/artifacts/%d", strings.TrimRight(cfg.githubAPI, "/"), cfg.repository, cfg.artifactID)
	var metadata artifactMetadata
	if err := getJSON(ctx, client, metadataURL, cfg.token, &metadata); err != nil {
		return fmt.Errorf("HANDOFF_METADATA_FETCH_FAILED: %w", err)
	}
	if err := validateMetadata(cfg, metadata); err != nil {
		return err
	}

	archive, err := os.CreateTemp("", "teamkit-candidate-*.zip")
	if err != nil {
		return fmt.Errorf("HANDOFF_TEMPFILE_FAILED: %w", err)
	}
	archiveName := archive.Name()
	defer os.Remove(archiveName)
	defer archive.Close()
	zipURL := fmt.Sprintf("%s/repos/%s/actions/artifacts/%d/zip", strings.TrimRight(cfg.githubAPI, "/"), cfg.repository, cfg.artifactID)
	if err := download(ctx, client, zipURL, cfg.token, archive); err != nil {
		return fmt.Errorf("HANDOFF_ARCHIVE_FETCH_FAILED: %w", err)
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("HANDOFF_ARCHIVE_CLOSE_FAILED: %w", err)
	}
	actualDigest, err := fileDigest(archiveName)
	if err != nil {
		return fmt.Errorf("HANDOFF_ARCHIVE_DIGEST_FAILED: %w", err)
	}
	if actualDigest != cfg.digest {
		return fmt.Errorf("HANDOFF_ARCHIVE_DIGEST_MISMATCH: got %s", actualDigest)
	}
	if err := extractExactCandidate(archiveName, cfg.outputDir); err != nil {
		return err
	}
	return writeEvidence(cfg)
}

func (cfg config) validate() error {
	if cfg.commit == "" || cfg.runID <= 0 || cfg.artifactID <= 0 || cfg.token == "" {
		return fmt.Errorf("HANDOFF_INPUT_INVALID")
	}
	if len(cfg.commit) != 40 || strings.Trim(cfg.commit, "0123456789abcdef") != "" {
		return fmt.Errorf("HANDOFF_COMMIT_INVALID")
	}
	if len(cfg.digest) != len("sha256:")+64 || !strings.HasPrefix(cfg.digest, "sha256:") || strings.TrimPrefix(cfg.digest, "sha256:") != strings.ToLower(strings.TrimPrefix(cfg.digest, "sha256:")) {
		return fmt.Errorf("HANDOFF_DIGEST_INVALID")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(cfg.digest, "sha256:")); err != nil {
		return fmt.Errorf("HANDOFF_DIGEST_INVALID")
	}
	return nil
}

func getJSON(ctx context.Context, client *http.Client, url, token string, target any) error {
	response, err := request(ctx, client, url, token)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func download(ctx context.Context, client *http.Client, url, token string, target io.Writer) error {
	response, err := request(ctx, client, url, token)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	_, err = io.Copy(target, response.Body)
	return err
}

func request(ctx context.Context, client *http.Client, url, token string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "teamkit-release-handoff")
	return client.Do(request)
}

func validateMetadata(cfg config, metadata artifactMetadata) error {
	if metadata.ID != cfg.artifactID || metadata.Name != "candidate-binaries" || metadata.Digest != cfg.digest || metadata.WorkflowRun.ID != cfg.runID || metadata.WorkflowRun.HeadSHA != cfg.commit {
		return fmt.Errorf("HANDOFF_METADATA_MISMATCH")
	}
	return nil
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func extractExactCandidate(archiveName, outputDir string) error {
	archive, err := zip.OpenReader(archiveName)
	if err != nil {
		return fmt.Errorf("HANDOFF_ARCHIVE_INVALID: %w", err)
	}
	defer archive.Close()
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return fmt.Errorf("HANDOFF_OUTPUT_CREATE_FAILED: %w", err)
	}
	expected := make(map[string]bool, len(requiredFiles))
	for _, name := range requiredFiles {
		expected[name] = true
	}
	seen := make(map[string]bool, len(requiredFiles))
	for _, file := range archive.File {
		if !expected[file.Name] || seen[file.Name] || file.FileInfo().IsDir() || file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("HANDOFF_ARCHIVE_LAYOUT_INVALID")
		}
		reader, err := file.Open()
		if err != nil {
			return fmt.Errorf("HANDOFF_ARCHIVE_ENTRY_OPEN_FAILED: %w", err)
		}
		mode := file.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		output, err := os.OpenFile(filepath.Join(outputDir, file.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err == nil {
			_, err = io.Copy(output, reader)
		}
		closeOutput := outputClose(output)
		closeReader := reader.Close()
		if err != nil {
			return fmt.Errorf("HANDOFF_ARCHIVE_ENTRY_COPY_FAILED: %w", err)
		}
		if closeOutput != nil || closeReader != nil {
			return fmt.Errorf("HANDOFF_ARCHIVE_ENTRY_CLOSE_FAILED")
		}
		info, err := os.Stat(filepath.Join(outputDir, file.Name))
		if err != nil || info.Size() == 0 {
			return fmt.Errorf("HANDOFF_ARCHIVE_ENTRY_EMPTY")
		}
		seen[file.Name] = true
	}
	if len(seen) != len(requiredFiles) {
		return fmt.Errorf("HANDOFF_ARCHIVE_LAYOUT_INVALID")
	}
	return nil
}

func outputClose(output *os.File) error {
	if output == nil {
		return nil
	}
	return output.Close()
}

func writeEvidence(cfg config) error {
	sums, err := os.Create(filepath.Join(cfg.outputDir, "SHA256SUMS.handoff"))
	if err != nil {
		return fmt.Errorf("HANDOFF_SUMS_CREATE_FAILED: %w", err)
	}
	for _, name := range requiredFiles {
		digest, err := fileDigest(filepath.Join(cfg.outputDir, name))
		if err != nil {
			sums.Close()
			return fmt.Errorf("HANDOFF_SUMS_DIGEST_FAILED: %w", err)
		}
		if _, err := fmt.Fprintf(sums, "%s  %s\n", strings.TrimPrefix(digest, "sha256:"), name); err != nil {
			sums.Close()
			return fmt.Errorf("HANDOFF_SUMS_WRITE_FAILED: %w", err)
		}
	}
	if err := sums.Close(); err != nil {
		return fmt.Errorf("HANDOFF_SUMS_CLOSE_FAILED: %w", err)
	}
	encoded, err := json.Marshal(manifest{cfg.commit, version, cfg.runID, cfg.artifactID, cfg.digest, "SHA256SUMS.handoff"})
	if err != nil {
		return fmt.Errorf("HANDOFF_MANIFEST_ENCODE_FAILED: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.outputDir, "MANIFEST.json"), append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("HANDOFF_MANIFEST_WRITE_FAILED: %w", err)
	}
	return nil
}
