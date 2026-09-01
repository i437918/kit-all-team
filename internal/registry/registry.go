// Package registry stores a bounded, paths-only most-recently-used list of
// Team Kit environment homes.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"github.com/mi1man-cmd/kit-all-team/internal/pathsafe"
	"github.com/mi1man-cmd/kit-all-team/internal/privatefile"
)

const SchemaVersion = 1
const MaxEntries = 64
const MaxBytes int64 = 65536

// Registry is the complete on-disk paths-only registry contract.
type Registry struct {
	SchemaVersion int      `json:"schema_version"`
	Homes         []string `json:"homes"`
}

// LoadState classifies registry loading without conflating corrupt data with
// operational filesystem failures.
type LoadState uint8

const (
	LoadMissing LoadState = iota
	LoadValid
	LoadCorrupt
	LoadUnavailable
)

var errRegistryContract = errors.New("REGISTRY_CONTRACT_INVALID")

// ErrReadOnlySession prevents a corrupt or unavailable registry from being
// replaced during the current Store session.
var ErrReadOnlySession = errors.New("REGISTRY_READ_ONLY_SESSION")

var canonicalPath = pathsafe.CanonicalPath
var comparisonKey = pathsafe.ComparisonKey
var openRegistryFile = privatefile.OpenValidated

// Store serializes access to one registry and caches its validated session
// state so an unsafe file is never silently replaced.
type Store struct {
	path        string
	locationErr error

	mu       sync.Mutex
	loaded   bool
	state    LoadState
	snapshot Registry
	loadErr  error
}

// New returns a Store for path without touching the filesystem.
func New(path string) *Store { return &Store{path: path} }

// NewDefault resolves the current platform location without touching the
// filesystem. Any resolution failure is reported by Load or Promote.
func NewDefault() *Store {
	path, err := DefaultPath(defaultLocationOptions())
	return &Store{path: path, locationErr: err}
}

// Load reads and validates the registry, caching the resulting session state.
func (store *Store) Load(ctx context.Context) (Registry, LoadState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.loadLocked(ctx)
}

func (store *Store) loadLocked(ctx context.Context) (Registry, LoadState, error) {
	if store.loaded {
		return cloneRegistry(store.snapshot), store.state, store.loadErr
	}
	if err := ctx.Err(); err != nil {
		return store.cacheLoad(Registry{}, LoadUnavailable, err)
	}
	if store.locationErr != nil {
		return store.cacheLoad(Registry{}, LoadUnavailable, store.locationErr)
	}

	directory := filepath.Dir(store.path)
	if _, err := os.Lstat(directory); errors.Is(err, fs.ErrNotExist) {
		return store.cacheLoad(Registry{SchemaVersion: SchemaVersion, Homes: []string{}}, LoadMissing, nil)
	} else if err != nil {
		return store.cacheLoad(Registry{}, LoadUnavailable, err)
	}
	if err := validateRegistryDirectory(directory); err != nil {
		return store.cacheLoad(Registry{}, LoadUnavailable, err)
	}
	file, err := openRegistryFile(store.path)
	if errors.Is(err, fs.ErrNotExist) {
		return store.cacheLoad(Registry{SchemaVersion: SchemaVersion, Homes: []string{}}, LoadMissing, nil)
	}
	if err != nil {
		return store.cacheLoad(Registry{}, LoadUnavailable, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return store.cacheLoad(Registry{}, LoadUnavailable, readErr)
	}
	if closeErr != nil {
		return store.cacheLoad(Registry{}, LoadUnavailable, closeErr)
	}
	registry, err := decodeRegistry(data)
	if err != nil {
		if errors.Is(err, errRegistryContract) {
			return store.cacheLoad(Registry{}, LoadCorrupt, err)
		}
		return store.cacheLoad(Registry{}, LoadUnavailable, err)
	}
	return store.cacheLoad(registry, LoadValid, nil)
}

func (store *Store) cacheLoad(registry Registry, state LoadState, err error) (Registry, LoadState, error) {
	store.loaded = true
	store.state = state
	store.snapshot = cloneRegistry(registry)
	store.loadErr = err
	return cloneRegistry(registry), state, err
}

func cloneRegistry(registry Registry) Registry {
	result := registry
	result.Homes = append([]string(nil), registry.Homes...)
	return result
}

// Promote moves home to the front of the bounded MRU registry.
func (store *Store) Promote(ctx context.Context, home string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	canonical, key, err := canonicalRegistryHome(home)
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.loaded {
		_, _, _ = store.loadLocked(ctx)
	}
	if store.state == LoadCorrupt || store.state == LoadUnavailable {
		return fmt.Errorf("%w: %v", ErrReadOnlySession, store.loadErr)
	}

	homes := make([]string, 0, min(MaxEntries, len(store.snapshot.Homes)+1))
	homes = append(homes, canonical)
	for _, existing := range store.snapshot.Homes {
		existingKey, err := comparisonKey(existing)
		if err != nil {
			return err
		}
		if existingKey != key && len(homes) < MaxEntries {
			homes = append(homes, existing)
		}
	}
	updated := Registry{SchemaVersion: SchemaVersion, Homes: homes}
	data, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if int64(len(data)) > MaxBytes {
		return contractError("document exceeds %d bytes", MaxBytes)
	}
	if err := writeRegistryAtomic(store.path, data); err != nil {
		return err
	}
	store.snapshot = cloneRegistry(updated)
	store.state = LoadValid
	store.loadErr = nil
	return nil
}

func contractError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errRegistryContract, fmt.Sprintf(format, args...))
}

func requireDelim(decoder *json.Decoder, want json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return contractError("JSON token: %v", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != want {
		return contractError("want delimiter %q", want)
	}
	return nil
}

func decodeRegistry(data []byte) (Registry, error) {
	if int64(len(data)) > MaxBytes {
		return Registry{}, contractError("document exceeds %d bytes", MaxBytes)
	}
	if !utf8.Valid(data) {
		return Registry{}, contractError("document is not UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := requireDelim(decoder, '{'); err != nil {
		return Registry{}, err
	}
	seenFields, seenHomes := map[string]struct{}{}, map[string]struct{}{}
	result := Registry{}
	haveSchema, haveHomes := false, false
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return Registry{}, contractError("field token: %v", err)
		}
		field, ok := token.(string)
		if !ok {
			return Registry{}, contractError("field name is not a string")
		}
		if _, duplicate := seenFields[field]; duplicate {
			return Registry{}, contractError("duplicate field %q", field)
		}
		seenFields[field] = struct{}{}
		switch field {
		case "schema_version":
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil || !bytes.Equal(bytes.TrimSpace(raw), []byte("1")) {
				return Registry{}, contractError("schema_version must equal integer 1")
			}
			result.SchemaVersion, haveSchema = SchemaVersion, true
		case "homes":
			if err := requireDelim(decoder, '['); err != nil {
				return Registry{}, err
			}
			for decoder.More() {
				if len(result.Homes) == MaxEntries {
					return Registry{}, contractError("homes exceeds %d entries", MaxEntries)
				}
				var raw json.RawMessage
				if err := decoder.Decode(&raw); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					return Registry{}, contractError("home must be a string")
				}
				var home string
				if err := json.Unmarshal(raw, &home); err != nil {
					return Registry{}, contractError("home must be a string")
				}
				canonical, key, err := canonicalRegistryHome(home)
				if err != nil {
					return Registry{}, err
				}
				if _, duplicate := seenHomes[key]; duplicate {
					return Registry{}, contractError("duplicate home")
				}
				seenHomes[key] = struct{}{}
				result.Homes = append(result.Homes, canonical)
			}
			if err := requireDelim(decoder, ']'); err != nil {
				return Registry{}, err
			}
			haveHomes = true
		default:
			return Registry{}, contractError("unknown field %q", field)
		}
	}
	if err := requireDelim(decoder, '}'); err != nil {
		return Registry{}, err
	}
	if !haveSchema || !haveHomes {
		return Registry{}, contractError("required fields are missing")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Registry{}, contractError("trailing JSON")
	}
	return result, nil
}

func canonicalRegistryHome(home string) (string, string, error) {
	if home == "" || !filepath.IsAbs(home) || home != filepath.Clean(home) {
		return "", "", contractError("home must be a nonempty canonical absolute path")
	}
	canonical, err := canonicalPath(home)
	if err != nil {
		if errors.Is(err, pathsafe.ErrUnsafe) {
			return "", "", fmt.Errorf("%w: %w", errRegistryContract, err)
		}
		return "", "", err
	}
	key, err := comparisonKey(canonical)
	if err != nil {
		if errors.Is(err, pathsafe.ErrUnsafe) {
			return "", "", fmt.Errorf("%w: %w", errRegistryContract, err)
		}
		return "", "", err
	}
	return canonical, key, nil
}
