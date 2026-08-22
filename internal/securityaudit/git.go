package securityaudit

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type gitObjectReferences struct {
	trackedPaths []string
	history      bool
}

type gitObjectInfo struct {
	kind string
	size int64
}

func (a *auditor) scanRepository(ctx context.Context, root, historyRef string) (string, error) {
	commitBytes, err := runGit(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(string(commitBytes))
	if !commitPattern.MatchString(commit) {
		return "", fmt.Errorf("security audit repository commit is invalid")
	}
	historyArgs := []string{"--all"}
	if historyRef != "" {
		if !commitPattern.MatchString(historyRef) || historyRef != commit {
			return "", fmt.Errorf("security audit history ref must match repository HEAD")
		}
		historyArgs = []string{historyRef}
	}

	references := map[string]*gitObjectReferences{}
	index, err := runGit(ctx, root, "ls-files", "--stage", "-z")
	if err != nil {
		return "", err
	}
	for _, record := range bytes.Split(index, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		tab := bytes.IndexByte(record, '\t')
		if tab < 0 {
			return "", fmt.Errorf("security audit cannot parse Git index")
		}
		fields := bytes.Fields(record[:tab])
		if len(fields) != 3 || len(fields[1]) != 40 {
			return "", fmt.Errorf("security audit cannot parse Git index")
		}
		path := string(record[tab+1:])
		a.addCoverage("tracked_paths", 1, int64(len(path)))
		if forbiddenTrackedPath(path) {
			a.addFinding("tracked_paths", "forbidden_path", path)
		}
		object := string(fields[1])
		entry := references[object]
		if entry == nil {
			entry = &gitObjectReferences{}
			references[object] = entry
		}
		entry.trackedPaths = append(entry.trackedPaths, path)
	}

	objectArgs := append([]string{"rev-list", "--objects"}, historyArgs...)
	objectArgs = append(objectArgs, "--no-object-names")
	objectOutput, err := runGit(ctx, root, objectArgs...)
	if err != nil {
		return "", err
	}
	reachableObjectIDs, err := parseReachableGitObjectIDs(objectOutput)
	if err != nil {
		return "", err
	}
	for _, object := range reachableObjectIDs {
		entry := references[object]
		if entry == nil {
			entry = &gitObjectReferences{}
			references[object] = entry
		}
		entry.history = true
		if len(references) > maxGitObjects {
			return "", fmt.Errorf("security audit Git object limit exceeded")
		}
	}

	logArgs := append([]string{"log", "-z"}, historyArgs...)
	logArgs = append(logArgs, "--format=", "--name-only", "--no-renames")
	historicalPaths, err := runGit(ctx, root, logArgs...)
	if err != nil {
		return "", err
	}
	seenHistoricalPaths := map[string]struct{}{}
	for _, record := range bytes.Split(historicalPaths, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		path := string(record)
		if _, seen := seenHistoricalPaths[path]; seen {
			continue
		}
		seenHistoricalPaths[path] = struct{}{}
		if len(seenHistoricalPaths) > maxGitPaths {
			return "", fmt.Errorf("security audit Git path limit exceeded")
		}
		a.addCoverage("git_history", 1, int64(len(path)))
		if forbiddenTrackedPath(path) {
			a.addFinding("git_history", "forbidden_path", path)
		}
	}

	objectIDs := make([]string, 0, len(references))
	for object := range references {
		objectIDs = append(objectIDs, object)
	}
	infos, err := batchCheckGitObjects(ctx, root, objectIDs)
	if err != nil {
		return "", err
	}
	readable := make([]string, 0, len(infos))
	for object, info := range infos {
		entry := references[object]
		if info.kind != "blob" && info.kind != "commit" && info.kind != "tag" {
			continue
		}
		if info.size <= maxInputBytes {
			readable = append(readable, object)
			continue
		}
		if info.kind == "blob" {
			for _, trackedPath := range entry.trackedPaths {
				a.addFinding("tracked_content", "oversized_input", trackedPath)
			}
		}
		if entry.history {
			a.addFinding("git_history", "oversized_input", object)
		}
	}
	if err := readGitObjects(ctx, root, readable, infos, func(object, kind string, contents []byte) error {
		entry := references[object]
		if kind == "blob" {
			for _, trackedPath := range entry.trackedPaths {
				a.scanContent("tracked_content", trackedPath, contents)
			}
		}
		if entry.history {
			a.scanContent("git_history", object, contents)
			if kind == "blob" {
				if err := a.scanArchive("git_history", object, object, contents, 0); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return "", err
	}
	return commit, nil
}

func parseReachableGitObjectIDs(output []byte) ([]string, error) {
	if len(output) == 0 || output[len(output)-1] != '\n' {
		return nil, fmt.Errorf("security audit cannot parse Git objects")
	}
	records := bytes.Split(output[:len(output)-1], []byte{'\n'})
	objects := make([]string, 0, len(records))
	for _, record := range records {
		if !commitPattern.Match(record) {
			return nil, fmt.Errorf("security audit cannot parse Git objects")
		}
		objects = append(objects, string(record))
	}
	return objects, nil
}

func batchCheckGitObjects(ctx context.Context, root string, objects []string) (map[string]gitObjectInfo, error) {
	sort.Strings(objects)
	if len(objects) == 0 {
		return map[string]gitObjectInfo{}, nil
	}
	command := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	command.Stdin = strings.NewReader(strings.Join(objects, "\n") + "\n")
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("security audit Git command failed")
	}
	lines := bytes.FieldsFunc(output, func(r rune) bool { return r == '\n' || r == '\r' })
	if len(lines) != len(objects) {
		return nil, fmt.Errorf("security audit cannot parse Git objects")
	}
	result := make(map[string]gitObjectInfo, len(objects))
	for index, line := range lines {
		fields := bytes.Fields(line)
		if len(fields) != 3 || string(fields[0]) != objects[index] {
			return nil, fmt.Errorf("security audit cannot parse Git objects")
		}
		size, err := strconv.ParseInt(string(fields[2]), 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("security audit cannot parse Git object size")
		}
		result[objects[index]] = gitObjectInfo{kind: string(fields[1]), size: size}
	}
	return result, nil
}

func readGitObjects(ctx context.Context, root string, objects []string, infos map[string]gitObjectInfo, consume func(string, string, []byte) error) error {
	sort.Strings(objects)
	if len(objects) == 0 {
		return nil
	}
	command := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "--batch")
	command.Stdin = strings.NewReader(strings.Join(objects, "\n") + "\n")
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("security audit cannot start Git object scan")
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("security audit cannot start Git object scan")
	}
	fail := func() error {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("security audit cannot parse Git objects")
	}
	reader := bufio.NewReader(stdout)
	for _, expected := range objects {
		header, err := reader.ReadString('\n')
		if err != nil {
			return fail()
		}
		fields := strings.Fields(header)
		info, exists := infos[expected]
		if !exists || len(fields) != 3 || fields[0] != expected || fields[1] != info.kind {
			return fail()
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 || size > maxInputBytes {
			return fail()
		}
		contents := make([]byte, size)
		if _, err := io.ReadFull(reader, contents); err != nil {
			return fail()
		}
		separator, err := reader.ReadByte()
		if err != nil || separator != '\n' {
			return fail()
		}
		if err := consume(expected, info.kind, contents); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return err
		}
	}
	if err := command.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("security audit Git command failed")
	}
	return nil
}

func runGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("security audit cannot start Git command")
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("security audit cannot start Git command")
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maxGitCommandOutput+1))
	if readErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("security audit cannot read Git command")
	}
	if int64(len(output)) > maxGitCommandOutput {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("security audit Git output limit exceeded")
	}
	if err := command.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("security audit Git command failed")
	}
	return output, nil
}

func forbiddenTrackedPath(value string) bool {
	normalized := strings.ReplaceAll(filepath.ToSlash(value), "//", "/")
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		lower := strings.ToLower(part)
		if lower == ".env" || strings.HasPrefix(lower, ".env.") || lower == "db" || lower == ".teamkit" || lower == "certs" {
			return true
		}
	}
	base := strings.ToLower(filepath.Base(normalized))
	if base == "certs.zip" || base == "hermes-setup.exe" {
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".key", ".pem", ".p12", ".pfx", ".ppk", ".crt", ".cer", ".der":
		return true
	default:
		return false
	}
}
