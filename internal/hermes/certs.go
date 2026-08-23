package hermes

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

const maxManagedCertificateArchiveBytes = int64(64 << 20)

type certificateArchiveFile struct {
	file     *zip.File
	relative string
}

// ErrArchivePath reports an archive entry that would escape HERMES_HOME/certs.
var ErrArchivePath = errors.New("certificate archive path is unsafe")

// ErrCABundleMissing reports an archive that lacks the required CA bundle.
var ErrCABundleMissing = errors.New("certificate archive does not contain ca-bundle.pem")

// ExtractCertificates extracts a certificate bundle beneath HERMES_HOME/certs.
// It refuses absolute, traversal, and symbolic-link archive entries.
func ExtractCertificates(source io.ReaderAt, size int64, hermesHome string) (string, error) {
	if hermesHome == "" {
		return "", fmt.Errorf("%w: empty HERMES_HOME", ErrArchivePath)
	}
	if err := pathsafe.EnsureDirectory(hermesHome, 0o700); err != nil {
		return "", fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	if size <= 0 || size > maxManagedCertificateArchiveBytes {
		return "", fmt.Errorf("%w: certificate archive size is invalid", ErrArchivePath)
	}
	archive, err := io.ReadAll(io.NewSectionReader(source, 0, size))
	if err != nil {
		return "", err
	}
	if int64(len(archive)) != size {
		return "", fmt.Errorf("%w: certificate archive is truncated", ErrArchivePath)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", err
	}
	files, bundleRelative, err := normalizedCertificateArchive(reader)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(hermesHome, "certs")
	if err := pathsafe.ValidateDirectory(destination); err != nil {
		return "", fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	staging, err := os.MkdirTemp(hermesHome, ".teamkit-certs-")
	if err != nil {
		return "", err
	}
	removeStaging := true
	defer func() {
		if removeStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := pathsafe.ValidateDirectory(staging); err != nil {
		return "", fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	for _, archived := range files {
		file := archived.file
		relative := archived.relative
		target := filepath.Join(staging, relative)
		if !isWithin(staging, target) {
			return "", fmt.Errorf("%w: %q", ErrArchivePath, file.Name)
		}
		if err := pathsafe.EnsureDirectory(filepath.Dir(target), 0o700); err != nil {
			return "", fmt.Errorf("%w: %v", ErrArchivePath, err)
		}
		input, err := file.Open()
		if err != nil {
			return "", err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = input.Close()
			return "", err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		inputErr := input.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if inputErr != nil {
			return "", inputErr
		}
	}
	renamed, err := publishCertificateStaging(staging, destination, bundleRelative)
	if err != nil {
		return "", err
	}
	if renamed {
		removeStaging = false
	}
	managedArchive := filepath.Join(hermesHome, ".teamkit", "certificates.zip")
	if err := writeCertificateAtomic(managedArchive, archive); err != nil {
		return "", err
	}
	return filepath.Join(destination, bundleRelative), nil
}

func publishCertificateStaging(staging, destination, bundleRelative string) (bool, error) {
	if err := pathsafe.ValidateDirectory(filepath.Dir(destination)); err != nil {
		return false, fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	if err := pathsafe.ValidateDirectory(staging); err != nil {
		return false, fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	if info, err := os.Lstat(destination); errors.Is(err, fs.ErrNotExist) {
		if err := os.Rename(staging, destination); err != nil {
			return false, err
		}
		return true, nil
	} else if err != nil {
		return false, err
	} else if !info.IsDir() {
		return false, fmt.Errorf("%w: certificate destination is not a directory", ErrArchivePath)
	}
	if err := pathsafe.ValidateDirectory(destination); err != nil {
		return false, fmt.Errorf("%w: %v", ErrArchivePath, err)
	}

	var files []string
	err := filepath.WalkDir(staging, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == staging || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: staged certificate is not regular", ErrArchivePath)
		}
		relative, err := filepath.Rel(staging, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if !isWithin(destination, target) {
			return fmt.Errorf("%w: staged path escapes destination", ErrArchivePath)
		}
		if err := pathsafe.ValidateDirectory(filepath.Dir(target)); err != nil {
			return fmt.Errorf("%w: %v", ErrArchivePath, err)
		}
		if err := pathsafe.ValidateRegular(target); err != nil {
			return fmt.Errorf("%w: %v", ErrArchivePath, err)
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		return false, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i] == bundleRelative {
			return false
		}
		if files[j] == bundleRelative {
			return true
		}
		return files[i] < files[j]
	})
	for _, relative := range files {
		data, err := os.ReadFile(filepath.Join(staging, relative))
		if err != nil {
			return false, err
		}
		target := filepath.Join(destination, relative)
		if err := writeCertificateAtomic(target, data); err != nil {
			return false, err
		}
	}
	return false, nil
}

func writeCertificateAtomic(path string, data []byte) error {
	if err := pathsafe.ValidateRegular(path); err != nil {
		return fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	directory := filepath.Dir(path)
	if err := pathsafe.EnsureDirectory(directory, 0o700); err != nil {
		return fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	temporary, err := os.CreateTemp(directory, ".teamkit-certificate-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := pathsafe.ValidateDirectory(directory); err != nil {
		return fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	if err := pathsafe.ValidateRegular(path); err != nil {
		return fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	return os.Rename(temporaryPath, path)
}

func safeArchivePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", fmt.Errorf("%w: %q", ErrArchivePath, name)
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("%w: %q", ErrArchivePath, name)
	}
	return cleaned, nil
}

func isWithin(base, target string) bool {
	relative, err := filepath.Rel(base, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// ApplicationCAEnvironment returns the application-local CA variables for the
// supplied certificate bundle. It never alters the operating-system trust store.
func ApplicationCAEnvironment(certificatePath string) map[string]string {
	return map[string]string{
		"HERMES_CA_BUNDLE":    certificatePath,
		"SSL_CERT_FILE":       certificatePath,
		"CURL_CA_BUNDLE":      certificatePath,
		"REQUESTS_CA_BUNDLE":  certificatePath,
		"GIT_SSL_CAINFO":      certificatePath,
		"NODE_EXTRA_CA_CERTS": certificatePath,
	}
}

// ManagedCertificateBundleReady verifies that the retained archive has the
// expected digest and that the published CA bundle exactly matches its unique
// ca-bundle.pem entry.
func ManagedCertificateBundleReady(hermesHome, expectedArchiveSHA256 string) (bool, error) {
	_, ready, err := ManagedCertificateBundle(hermesHome, expectedArchiveSHA256)
	return ready, err
}

// ManagedCertificateBundle returns the safely verified managed bundle path.
func ManagedCertificateBundle(hermesHome, expectedArchiveSHA256 string) (string, bool, error) {
	if !filepath.IsAbs(hermesHome) {
		return "", false, fmt.Errorf("%w: HERMES_HOME must be absolute", ErrArchivePath)
	}
	archivePath := filepath.Join(filepath.Clean(hermesHome), ".teamkit", "certificates.zip")
	if err := pathsafe.ValidateRegular(archivePath); err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	archiveInfo, err := os.Lstat(archivePath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !archiveInfo.Mode().IsRegular() || archiveInfo.Mode()&os.ModeSymlink != 0 || archiveInfo.Size() <= 0 || archiveInfo.Size() > maxManagedCertificateArchiveBytes {
		return "", false, nil
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		return "", false, err
	}
	digest := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), expectedArchiveSHA256) {
		return "", false, nil
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", false, nil
	}
	files, bundleRelative, err := normalizedCertificateArchive(reader)
	if err != nil {
		return "", false, nil
	}
	var expectedBundle []byte
	for _, archived := range files {
		if archived.relative != bundleRelative {
			continue
		}
		input, err := archived.file.Open()
		if err != nil {
			return "", false, err
		}
		expectedBundle, err = io.ReadAll(io.LimitReader(input, maxManagedCertificateArchiveBytes+1))
		closeErr := input.Close()
		if err != nil {
			return "", false, err
		}
		if closeErr != nil {
			return "", false, closeErr
		}
		if int64(len(expectedBundle)) > maxManagedCertificateArchiveBytes {
			return "", false, nil
		}
		break
	}
	bundlePath := filepath.Join(filepath.Clean(hermesHome), "certs", bundleRelative)
	if err := pathsafe.ValidateRegular(bundlePath); err != nil {
		return "", false, fmt.Errorf("%w: %v", ErrArchivePath, err)
	}
	bundleInfo, err := os.Lstat(bundlePath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !bundleInfo.Mode().IsRegular() || bundleInfo.Mode()&os.ModeSymlink != 0 || bundleInfo.Size() != int64(len(expectedBundle)) {
		return "", false, nil
	}
	actualBundle, err := os.ReadFile(bundlePath)
	if err != nil {
		return "", false, err
	}
	if !bytes.Equal(actualBundle, expectedBundle) {
		return "", false, nil
	}
	return bundlePath, true, nil
}

// CertificateEnvironmentReady checks that the provider key is nonblank and
// all six application-local CA variables point at the exact managed bundle.
func CertificateEnvironmentReady(path, bundle string) (bool, error) {
	if err := pathsafe.ValidateRegular(path); err != nil {
		return false, err
	}
	if err := privatefile.Validate(path); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 4<<20 {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	values := make(map[string]string)
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if raw == "" || strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			return false, nil
		}
		if _, duplicate := values[key]; duplicate {
			return false, nil
		}
		values[key] = value
	}
	for key, expected := range ApplicationCAEnvironment(bundle) {
		if values[key] != expected {
			return false, nil
		}
	}
	if strings.TrimSpace(values[PublicProviderProvider().APIKeyEnvironment]) == "" {
		return false, nil
	}
	return true, nil
}

func normalizedCertificateArchive(reader *zip.Reader) ([]certificateArchiveFile, string, error) {
	const (
		layoutRoot  = "root"
		layoutCerts = "certs"
	)
	files := make([]certificateArchiveFile, 0, len(reader.File))
	seen := make(map[string]struct{}, len(reader.File))
	layout := ""
	bundleRelative := ""
	for _, file := range reader.File {
		if archivePathHasTraversal(file.Name) {
			return nil, "", fmt.Errorf("%w: %q", ErrArchivePath, file.Name)
		}
		relative, err := safeArchivePath(file.Name)
		if err != nil {
			return nil, "", err
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return nil, "", fmt.Errorf("%w: non-regular entry %q", ErrArchivePath, file.Name)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			return nil, "", fmt.Errorf("%w: non-regular entry %q", ErrArchivePath, file.Name)
		}

		entryLayout := layoutRoot
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) > 1 && parts[0] == "certs" {
			entryLayout = layoutCerts
			relative = filepath.FromSlash(strings.Join(parts[1:], "/"))
		}
		if layout == "" {
			layout = entryLayout
		} else if layout != entryLayout {
			return nil, "", fmt.Errorf("%w: mixed certificate archive layouts", ErrArchivePath)
		}
		key := strings.ToLower(filepath.ToSlash(relative))
		if _, duplicate := seen[key]; duplicate {
			return nil, "", fmt.Errorf("%w: duplicate entry %q", ErrArchivePath, file.Name)
		}
		seen[key] = struct{}{}
		if filepath.Base(relative) == "ca-bundle.pem" {
			if bundleRelative != "" {
				return nil, "", fmt.Errorf("%w: multiple ca-bundle.pem entries", ErrArchivePath)
			}
			bundleRelative = relative
		}
		files = append(files, certificateArchiveFile{file: file, relative: relative})
	}
	if bundleRelative == "" {
		return nil, "", ErrCABundleMissing
	}
	return files, bundleRelative, nil
}

func archivePathHasTraversal(name string) bool {
	for _, component := range strings.Split(strings.ReplaceAll(name, "\\", "/"), "/") {
		if component == ".." {
			return true
		}
	}
	return false
}
