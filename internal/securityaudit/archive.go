package securityaudit

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (a *auditor) scanArtifactPath(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("security audit artifact path is unavailable")
	}
	if forbiddenTrackedPath(filepath.Base(root)) {
		a.addFinding("artifact_files", "forbidden_path", root)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		a.addFinding("artifact_files", "unsafe_symlink", root)
		return nil
	}
	if info.Mode().IsRegular() {
		return a.readAndScan("artifact_files", root, root)
	}
	if !info.IsDir() {
		a.addFinding("artifact_files", "unsafe_file_type", root)
		return nil
	}
	return filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("security audit cannot walk artifacts")
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return fmt.Errorf("security audit cannot resolve artifact path")
		}
		if forbiddenTrackedPath(relative) {
			a.addFinding("artifact_files", "forbidden_path", current)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			a.addFinding("artifact_files", "unsafe_symlink", current)
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		return a.readAndScan("artifact_files", current, current)
	})
}

func (a *auditor) scanArchive(scope, location, name string, data []byte, depth int) error {
	format := detectArchive(name, data)
	if depth >= maxArchiveDepth {
		if format != archiveNone {
			a.addFinding(scope, "archive_depth_exceeded", location)
		}
		return nil
	}
	switch format {
	case archiveZIP:
		return a.scanZIP(scope, location, data, depth+1)
	case archiveTarGzip:
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("security audit archive is invalid")
		}
		defer reader.Close()
		return a.scanTar(scope, location, tar.NewReader(reader), depth+1)
	case archiveTar:
		return a.scanTar(scope, location, tar.NewReader(bytes.NewReader(data)), depth+1)
	default:
		return nil
	}
}

type archiveFormat uint8

const (
	archiveNone archiveFormat = iota
	archiveZIP
	archiveTar
	archiveTarGzip
)

func detectArchive(name string, data []byte) archiveFormat {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".zip") || bytes.HasPrefix(data, []byte("PK\x03\x04")) ||
		bytes.HasPrefix(data, []byte("PK\x05\x06")) || bytes.HasPrefix(data, []byte("PK\x07\x08")) {
		return archiveZIP
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") ||
		bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		return archiveTarGzip
	}
	if strings.HasSuffix(lower, ".tar") || (len(data) >= 262 && bytes.Equal(data[257:262], []byte("ustar"))) {
		return archiveTar
	}
	return archiveNone
}

func (a *auditor) scanZIP(scope, location string, data []byte, depth int) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("security audit archive is invalid")
	}
	if len(archive.File) > maxArchiveItems {
		a.addFinding(scope, "oversized_archive", location)
		return nil
	}
	for index, entry := range archive.File {
		entryLocation := fmt.Sprintf("%s#%d", location, index)
		if !safeArchiveName(entry.Name) {
			a.addFinding(scope, "unsafe_archive_path", entryLocation)
			continue
		}
		if forbiddenTrackedPath(entry.Name) {
			a.addFinding(scope, "forbidden_path", entryLocation)
		}
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			a.addFinding(scope, "unsafe_archive_entry", entryLocation)
			continue
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.UncompressedSize64 > uint64(maxInputBytes) {
			a.addFinding(scope, "oversized_input", entryLocation)
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return fmt.Errorf("security audit archive cannot be read")
		}
		contents, readErr := io.ReadAll(io.LimitReader(reader, maxInputBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return fmt.Errorf("security audit archive cannot be read")
		}
		if int64(len(contents)) > maxInputBytes {
			a.addFinding(scope, "oversized_input", entryLocation)
			continue
		}
		a.scanContent(scope, entryLocation, contents)
		if err := a.scanArchive(scope, entryLocation, entry.Name, contents, depth); err != nil {
			return err
		}
	}
	return nil
}

func (a *auditor) scanTar(scope, location string, archive *tar.Reader, depth int) error {
	for index := 0; ; index++ {
		if index >= maxArchiveItems {
			a.addFinding(scope, "oversized_archive", location)
			return nil
		}
		header, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("security audit archive is invalid")
		}
		entryLocation := fmt.Sprintf("%s#%d", location, index)
		if !safeArchiveName(header.Name) {
			a.addFinding(scope, "unsafe_archive_path", entryLocation)
			continue
		}
		if forbiddenTrackedPath(header.Name) {
			a.addFinding(scope, "forbidden_path", entryLocation)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			a.addFinding(scope, "unsafe_archive_entry", entryLocation)
			continue
		}
		if header.Size < 0 || header.Size > maxInputBytes {
			a.addFinding(scope, "oversized_input", entryLocation)
			continue
		}
		contents, err := io.ReadAll(io.LimitReader(archive, maxInputBytes+1))
		if err != nil {
			return fmt.Errorf("security audit archive cannot be read")
		}
		if int64(len(contents)) > maxInputBytes {
			a.addFinding(scope, "oversized_input", entryLocation)
			continue
		}
		a.scanContent(scope, entryLocation, contents)
		if err := a.scanArchive(scope, entryLocation, header.Name, contents, depth); err != nil {
			return err
		}
	}
}

func safeArchiveName(name string) bool {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") {
		return false
	}
	clean := path.Clean(name)
	return clean != "." && !path.IsAbs(clean) && clean != ".." && !strings.HasPrefix(clean, "../")
}
