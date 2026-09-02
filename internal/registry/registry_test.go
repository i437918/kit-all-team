package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func protectedRegistryPath(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(testutil.TempDir(t), "registry")
	if err := ensureRegistryDirectory(directory); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, "environments.json")
}

func protectedRegistryFixture(t *testing.T, document []byte) string {
	t.Helper()
	path := protectedRegistryPath(t)
	if err := writeRegistryAtomic(path, document); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStoreLoad_RejectsUnknownFieldsDuplicatesAndBounds(t *testing.T) {
	root := filepath.Join(testutil.TempDir(t), "kit")
	other := filepath.Join(testutil.TempDir(t), "other")
	cleanRoot, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	duplicateHome := fmt.Sprintf(`{"schema_version":1,"homes":[%s,%s]}`, cleanRoot, cleanRoot)
	nonCanonical := other + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(other)
	nonCanonicalHome := fmt.Sprintf(`{"schema_version":1,"homes":[%q]}`, nonCanonical)
	cases := []string{
		`{}`,
		`{"schema_version":1}`,
		`{"homes":[]}`,
		`{"schema_version":null,"homes":[]}`,
		`{"schema_version":1,"homes":null}`,
		`{"schema_version":1,"schema_version":1,"homes":[]}`,
		`{"schema_version":1,"homes":[],"homes":[]}`,
		`{"schema_version":1,"homes":[],"project":"apa"}`,
		`{"schema_version":2,"homes":[]}`,
		duplicateHome,
		`{"schema_version":1,"homes":["relative"]}`,
		nonCanonicalHome,
	}
	for _, document := range cases {
		path := protectedRegistryFixture(t, []byte(document))
		_, state, err := New(path).Load(context.Background())
		if state != LoadCorrupt || err == nil {
			t.Fatalf("state=%v err=%v document=%s", state, err, document)
		}
	}
}

func TestStoreLoad_RejectsEntryByteAndUTF8Bounds(t *testing.T) {
	homes := make([]string, 65)
	for index := range homes {
		homes[index] = filepath.Join(testutil.TempDir(t), fmt.Sprintf("kit-%02d", index))
	}
	document, err := json.Marshal(Registry{SchemaVersion: 1, Homes: homes})
	if err != nil {
		t.Fatal(err)
	}
	invalidUTF8 := []byte{'{', '"', 's', 'c', 'h', 'e', 'm', 'a', '_', 'v', 'e', 'r', 's', 'i', 'o', 'n', '"', ':', '1', ',', '"', 'h', 'o', 'm', 'e', 's', '"', ':', '[', '"', 0xff, '"', ']', '}'}
	for _, data := range [][]byte{document, bytes.Repeat([]byte{' '}, int(MaxBytes+1)), invalidUTF8} {
		path := protectedRegistryFixture(t, data)
		_, state, loadErr := New(path).Load(context.Background())
		if state != LoadCorrupt || loadErr == nil {
			t.Fatalf("state=%v err=%v len=%d", state, loadErr, len(data))
		}
	}
}

func TestStorePromote_MovesCanonicalHomeToFrontAndCaps64(t *testing.T) {
	store := New(protectedRegistryPath(t))
	for i := 0; i < 65; i++ {
		home := filepath.Join(testutil.TempDir(t), fmt.Sprintf("kit-%02d", i))
		if err := store.Promote(context.Background(), home); err != nil {
			t.Fatal(err)
		}
	}
	got, state, err := store.Load(context.Background())
	if err != nil || state != LoadValid || len(got.Homes) != 64 {
		t.Fatalf("state=%v len=%d err=%v", state, len(got.Homes), err)
	}
	if !strings.HasSuffix(got.Homes[0], "kit-64") {
		t.Fatalf("MRU=%q", got.Homes[0])
	}
}

func TestStorePromote_DoesNotRewriteCorruptOrUnavailableSession(t *testing.T) {
	path := protectedRegistryFixture(t, []byte(`{broken`))
	before, _ := os.ReadFile(path)
	store := New(path)
	_, state, _ := store.Load(context.Background())
	if state != LoadCorrupt {
		t.Fatalf("state=%v", state)
	}
	if err := store.Promote(context.Background(), filepath.Join(testutil.TempDir(t), "kit")); !errors.Is(err, ErrReadOnlySession) {
		t.Fatalf("err=%v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("corrupt registry was rewritten")
	}
}

func TestStoreLoad_PathContractFailureIsCorruptButComparisonIOIsUnavailable(t *testing.T) {
	corrupt := protectedRegistryFixture(t, []byte(`{"schema_version":1,"homes":["relative"]}`))
	if _, state, err := New(corrupt).Load(context.Background()); state != LoadCorrupt || err == nil {
		t.Fatalf("state=%v err=%v", state, err)
	}
	home := filepath.Join(testutil.TempDir(t), "kit")
	document, err := json.Marshal(Registry{SchemaVersion: 1, Homes: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	unavailable := protectedRegistryFixture(t, document)
	original := comparisonKey
	comparisonKey = func(string) (string, error) { return "", fs.ErrPermission }
	defer func() { comparisonKey = original }()
	if _, state, err := New(unavailable).Load(context.Background()); state != LoadUnavailable || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("state=%v err=%v", state, err)
	}
}

func TestStoreLoad_ReadsFromTheValidatedHandleAfterPathReplacement(t *testing.T) {
	home := filepath.Join(testutil.TempDir(t), "kit")
	document, err := json.Marshal(Registry{SchemaVersion: SchemaVersion, Homes: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	path := protectedRegistryFixture(t, document)
	replacement := protectedRegistryFixture(t, []byte(`{broken`))
	original := openRegistryFile
	openRegistryFile = func(name string) (*os.File, error) {
		file, err := original(name)
		if err != nil {
			return nil, err
		}
		backup := name + ".opened"
		if err := os.Rename(name, backup); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := os.Rename(replacement, name); err != nil {
			_ = file.Close()
			return nil, err
		}
		return file, nil
	}
	defer func() { openRegistryFile = original }()

	got, state, err := New(path).Load(context.Background())
	if err != nil || state != LoadValid || len(got.Homes) != 1 || got.Homes[0] != home {
		t.Fatalf("registry=%+v state=%v err=%v", got, state, err)
	}
}

func TestStorePromote_DoesNotRewriteUnavailableSession(t *testing.T) {
	path := protectedRegistryFixture(t, []byte(`{"schema_version":1,"homes":[]}`))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original := openRegistryFile
	openRegistryFile = func(string) (*os.File, error) { return nil, fs.ErrPermission }
	defer func() { openRegistryFile = original }()
	store := New(path)
	if _, state, err := store.Load(context.Background()); state != LoadUnavailable || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("state=%v err=%v", state, err)
	}
	if err := store.Promote(context.Background(), filepath.Join(testutil.TempDir(t), "kit")); !errors.Is(err, ErrReadOnlySession) {
		t.Fatalf("Promote() error = %v, want ErrReadOnlySession", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("registry rewritten: before=%q after=%q err=%v", before, after, err)
	}
}
