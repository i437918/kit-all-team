# Environment Mode and Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Добавить в интерактивный `teamkit apply` явные режимы создания и обновления окружения, безопасно находить ранее созданные окружения через локальный MRU-реестр путей и сохранить прежние неинтерактивные контракты.

**Architecture:** Новый `internal/registry` хранит только bounded MRU-массив абсолютных путей в owner-only atomic JSON, а новый `internal/environment` выполняет одинаковую read-only инспекцию каждого кандидата, начиная с operation envelope и заканчивая согласованными `owner`/`.env`. `internal/cli` остаётся единственным владельцем интерактивного UI и orchestration: add использует текущий questionnaire и `Plan/Apply`, update выбирает verified environment и вызывает существующий `Service.Update`; отдельная invocation-session обеспечивает предупреждение ровно один раз и best-effort promotion только после actionful success.

**Tech Stack:** Go 1.26.6, стандартные `flag`, `encoding/json`, `io`, `os`, `path/filepath`; существующие `golang.org/x/sys/windows`, `internal/pathsafe`, `internal/privatefile`, `internal/config`, `internal/state`, `internal/reconcile`; table-driven unit tests, CLI fake/spies, black-box integration tests и cross-compilation с `CGO_ENABLED=0`.

## Global Constraints

- Источник требований: `docs/superpowers/specs/2026-08-16-environment-mode-and-registry-design.md` на commit `7a1849b04819674f10f2f2fb578a090c765d4452`.
- Интерактивный `apply` всегда первым показывает ровно `1. Добавить новое окружение` и `2. Обновить существующее окружение`; `plan`, `--non-interactive`, `status`, `retry` и direct `update` не показывают mode menu.
- `--update` остаётся только scope `none|content|database|both`; add разрешает unset/`none`, но `content|database|both` возвращает `UPDATE_CHOICE_NOT_APPLICABLE`.
- Registry discovery использует только `--kit-home` либо registry MRU и затем `KIT_ALL_TEAM_HOME`; disk/PATH/network scan запрещён.
- Registry JSON имеет ровно `schema_version: 1` и `homes`, максимум `64` записей и `65536` байт; project, OS, app, role, toolchain, credentials, secrets, receipt, status и timestamps запрещены.
- Registry расположен в `%LOCALAPPDATA%\TeamKit\environments.json`, `~/Library/Application Support/TeamKit/environments.json` или `${XDG_CONFIG_HOME:-~/.config}/teamkit/environments.json` и создаётся owner-only: POSIX `0700/0600`, Windows protected current-user-only DACL.
- Corrupt/Unavailable registry выдаёт ровно одно предупреждение, разрешает env/manual fallback и запрещает rewrite/Promote до конца запуска.
- Operation envelope/receipt инспектируется bounded и строго до `owner` и `.env`; незавершённая операция всегда возвращает `RETRY_REQUIRED` без credentials, network, Plan, Apply и записей.
- Пункт update `1. Ничего` после summary возвращает `0` без новых reads, credentials, network, Plan, Apply, workspace/registry writes и MRU promotion.
- Best-effort Promote разрешён только после actionful success apply/update, а также успешного retry; failure Promote печатает warning, не меняет exit `0` и не вызывает rollback/retry.
- В мастере показываются `cc_1c_skills от Широкова` и `ai_rules_1c от Филиппова`, но `.env`, flags, receipts и service contracts сохраняют ID `cc_1c_skills`/`ai_rules_1c`.
- Для Hermes устанавливается ровно выбранный набор; для десяти non-Hermes приложений создаётся только secret-free `.teamkit/handoff.txt` выбранного pinned repo/commit плюс MCP v8std; `app-installed=false` возвращает `AI_APP_REQUIRED` до toolchain prompt и любых workspace-файлов.
- Go-код сначала получает падающий тест, затем минимальную реализацию; каждый task завершается отдельным Conventional Commit и scoped review.

---

## File Structure Map

- Create `internal/registry/registry.go`: JSON types, `LoadState`, session-aware `Store.Load`/`Store.Promote`, bounded strict validation and MRU logic.
- Create `internal/registry/location.go`: pure platform-location resolver with injectable GOOS/env/home.
- Create `internal/registry/registry_test.go`: schema, size/count, duplicate, MRU, corrupt/unavailable and no-rewrite tests.
- Create `internal/registry/location_test.go`: Windows/macOS/Linux/ALT location matrix.
- Create `internal/registry/registry_other_test.go` and `internal/registry/registry_windows_test.go`: native permission, symlink/junction/reparse and atomic replacement evidence.
- Create `internal/registry/secure.go`, `secure_other.go`, `secure_windows.go`: registry-only owner-only directory, temporary file and same-directory atomic replacement; global workspace/private writers remain unchanged.
- Create `internal/registry/registry_other_test.go` and `registry_windows_test.go`: directory/temporary/final owner-only evidence including full-access ACE masks.
- Create `internal/environment/environment.go`: `Candidate`, `VerifiedEnvironment`, `InspectionState`, `AddState` and exported inspector interface.
- Create `internal/environment/inspect.go`: operation-first, bounded, no-reparse inspection of root, operation, owner and public `.env`.
- Create `internal/environment/discovery.go`: source precedence, platform-aware dedupe and displayable result collection without I/O beyond the inspector.
- Create `internal/environment/inspect_test.go`, `discovery_test.go`, `inspect_windows_test.go`: inspection order, partial receipt, invalid sources and Windows reparse matrix.
- Modify `internal/state/store.go` and `store_test.go`: bounded strict operation loading shared by retry/status and discovery.
- Modify `internal/pathsafe/pathsafe.go`, `compare_other.go`, `compare_windows.go` and tests: exported canonical comparison key for registry/discovery dedupe.
- Modify `internal/cli/flags.go`: retain whether `--kit-home`, `--update` and `--toolchain` were explicitly present, including `--flag=`.
- Modify `internal/cli/prompt.go` and `prompt_test.go`: exact mode, skills, environment-selection and update-scope menus.
- Create `internal/cli/environment_flow.go` and `environment_flow_test.go`: add/update selection, summary, manual fallback, warning-once and retry command rendering.
- Modify `internal/cli/run.go` and `run_test.go`: dispatch modes, add classification, update execution, no-op barrier and success-only best-effort promotion.
- Modify `cmd/teamkit/main.go` and create `cmd/teamkit/main_test.go`: wire production inspector/registry with no constructor I/O.
- Modify `internal/apps/apps_test.go`, `internal/bootstrap/effects_test.go`, `internal/cli/run_test.go`: 10×2 non-Hermes and real role×existing/new Hermes lifecycle single-toolchain regressions.
- Modify `test/integration/blackbox_test.go`: process-level add/update/no-op/retry scenarios.
- Modify `README.md`, `docs/INSTALL.md`, `docs/SECURITY.md`, `docs/TEST-MATRIX.md`, `CHANGELOG.md` and `test/release/docs_test.go`: user workflow, security contract and release-note evidence.

### Task 1: Safe canonical comparison keys

**Files:**
- Modify: `internal/pathsafe/pathsafe.go:42-75`
- Modify: `internal/pathsafe/compare_other.go:1-5`
- Modify: `internal/pathsafe/compare_windows.go:1-88`
- Modify: `internal/pathsafe/pathsafe_test.go`
- Modify: `internal/pathsafe/pathsafe_windows_test.go`

**Interfaces:**
- Produces: `pathsafe.CanonicalPath(path string) (string, error)` and `pathsafe.ComparisonKey(path string) (string, error)`; both validate every existing component before canonicalization. CanonicalPath returns exact cleaned input on POSIX and a DOS/UNC final-path-resolved absolute home on Windows; ComparisonKey returns the canonical path unchanged on POSIX and case-folded on Windows.
- Preserves unchanged: `workspace.WriteFileAtomic`, `privatefile.WriteAtomic`, all public workspace-file modes/DACL behavior and all secret-store behavior.

- [ ] **Step 1: Write the common RED test**

```go
func TestComparisonKey_CleansSafePathsAndSeparatesChildren(t *testing.T) {
    root := testutil.TempDir(t)
    first, err := ComparisonKey(filepath.Join(root, "."))
    if err != nil { t.Fatal(err) }
    second, err := ComparisonKey(root)
    if err != nil { t.Fatal(err) }
    child, err := ComparisonKey(filepath.Join(root, "child"))
    if err != nil { t.Fatal(err) }
    if first != second || child == first { t.Fatalf("first=%q second=%q child=%q", first, second, child) }
}

func TestComparisonKey_RejectsRedirectedExistingComponent(t *testing.T) {
    root := testutil.TempDir(t)
    external := testutil.TempDir(t)
    link := filepath.Join(root, "redirect")
    if err := os.Symlink(external, link); err != nil { t.Skipf("symlink unavailable: %v", err) }
    if _, err := ComparisonKey(filepath.Join(link, "kit")); !errors.Is(err, ErrUnsafe) { t.Fatalf("err=%v", err) }
}
```

- [ ] **Step 2: Write the Windows RED test**

```go
func TestComparisonKey_WindowsCaseAndAliasAreEqual(t *testing.T) {
    longPath := filepath.Join(testutil.TempDir(t), "A Long Directory Name For Teamkit")
    if err := os.Mkdir(longPath, 0o700); err != nil { t.Fatal(err) }
    longUTF16, err := windows.UTF16PtrFromString(longPath)
    if err != nil { t.Fatal(err) }
    buffer := make([]uint16, 32768)
    length, err := windows.GetShortPathName(longUTF16, &buffer[0], uint32(len(buffer)))
    if err != nil || length == 0 || int(length) >= len(buffer) { t.Skipf("8.3 alias unavailable: %v", err) }
    shortPath := windows.UTF16ToString(buffer[:length])
    if strings.EqualFold(filepath.Clean(shortPath), filepath.Clean(longPath)) || !strings.Contains(shortPath, "~") { t.Skip("8.3 aliases are disabled on this volume") }
    longKey, err := ComparisonKey(longPath)
    if err != nil { t.Fatal(err) }
    shortKey, err := ComparisonKey(strings.ToUpper(shortPath))
    if err != nil { t.Fatal(err) }
    if longKey != shortKey { t.Fatalf("long=%q short=%q", longKey, shortKey) }
    canonical, err := CanonicalPath(shortPath)
    if err != nil { t.Fatal(err) }
    if strings.Contains(canonical, "~") || !filepath.IsAbs(canonical) { t.Fatalf("canonical=%q", canonical) }
}

func TestComparisonKey_RejectsJunctionBeforeFinalPath(t *testing.T) {
    root, external := testutil.TempDir(t), testutil.TempDir(t)
    junction := filepath.Join(root, "junction")
    if output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput(); err != nil { t.Fatalf("create junction: %v: %s", err, output) }
    if _, err := ComparisonKey(filepath.Join(junction, "kit")); !errors.Is(err, ErrUnsafe) { t.Fatalf("err=%v", err) }
}
```

- [ ] **Step 3: Run the focused RED gate**

Run: `go test -mod=vendor -count=1 ./internal/pathsafe -run '^TestComparisonKey_'`

Expected: build failure `undefined: ComparisonKey`.

- [ ] **Step 4: Implement validation-before-canonicalization**

```go
func ComparisonKey(path string) (string, error) {
    canonical, err := CanonicalPath(path)
    if err != nil { return "", err }
    return comparisonKey(canonical), nil
}
func CanonicalPath(path string) (string, error) {
    if !filepath.IsAbs(path) { return "", unsafeError(path, "canonicalization requires an absolute path") }
    clean := filepath.Clean(path)
    if err := ValidateDirectory(clean); err != nil { return "", err }
    return canonicalPath(clean)
}
```

In `compare_other.go`, `canonicalPath` returns its input and `comparisonKey` returns its input. In `compare_windows.go`, rename the current final-prefix algorithm to `canonicalPath`: call `ValidateDirectory` before it is reached, resolve the existing prefix with `GetFinalPathNameByHandle(..., VOLUME_NAME_DOS)`, strip `\\?\` or convert `\\?\UNC\server\share` to `\\server\share`, then join the nonexisting suffix. `comparisonKey` returns `strings.ToLower(filepath.Clean(canonical))`. Do not canonicalize first and validate afterward.

Preserve the package-private function used by existing `Overlaps` without changing its call sites:

```go
func comparisonPath(path string) (string, error) { return ComparisonKey(path) }
```

Define this wrapper in both build variants (or once in `pathsafe.go`); it ensures `Overlaps` also receives validation-before-canonicalization rather than referring to a removed symbol.

Do not change `workspace.WriteFileAtomic`, `privatefile.WriteAtomic` or any file in `internal/privatefile`.

- [ ] **Step 5: Run the GREEN and unchanged-contract gates**

Run: `go test -mod=vendor -count=1 ./internal/pathsafe ./internal/workspace ./internal/privatefile ./internal/secrets`

Expected: PASS for all four packages; existing workspace public-metadata and private secret-file tests remain unchanged and green.

- [ ] **Step 6: Commit only pathsafe changes**

```powershell
git add internal/pathsafe
git commit -m "feat(pathsafe): add validated canonical comparison keys"
```

### Task 2: Paths-only MRU registry contract and platform locations

**Files:**
- Create: `internal/registry/registry.go`
- Create: `internal/registry/location.go`
- Create: `internal/registry/secure.go`
- Create: `internal/registry/secure_other.go`
- Create: `internal/registry/secure_windows.go`
- Create: `internal/registry/registry_test.go`
- Create: `internal/registry/location_test.go`
- Create: `internal/registry/registry_other_test.go`
- Create: `internal/registry/registry_windows_test.go`

**Interfaces:**
- Produces: `Registry` with fields `SchemaVersion int` mapped to JSON `schema_version` and `Homes []string` mapped to JSON `homes`.
- Produces: `type LoadState uint8` with `LoadMissing`, `LoadValid`, `LoadCorrupt`, `LoadUnavailable`.
- Produces: `type LocationOptions struct { GOOS string; Getenv func(string) string; UserHomeDir func() (string, error) }` and `DefaultPath(LocationOptions) (string, error)`.
- Produces: `type Store struct`; `New(path string) *Store`; `NewDefault() *Store`; `(*Store).Load(context.Context) (Registry, LoadState, error)`; `(*Store).Promote(context.Context, string) error`.
- Produces package-private registry-only functions `validateRegistryDirectory`, `ensureRegistryDirectory`, `writeRegistryAtomic`, `replaceRegistryFile`; no global atomic writer changes.
- Consumes unchanged `privatefile.CreateTemp`/`privatefile.Validate` only for protected registry temporary/final files and Task 1 `pathsafe.CanonicalPath`/`pathsafe.ComparisonKey` for safe Windows storage plus equality.

- [ ] **Step 1: Write failing location and strict-schema tests**

```go
func TestDefaultPath_PlatformMatrix(t *testing.T) {
    tests := []struct{ goos string; env map[string]string; home, want string }{
        {"windows", map[string]string{"LOCALAPPDATA": `C:\Users\D\AppData\Local`}, `C:\Users\D`, `C:\Users\D\AppData\Local\TeamKit\environments.json`},
        {"darwin", nil, "/Users/d", "/Users/d/Library/Application Support/TeamKit/environments.json"},
        {"linux", map[string]string{"XDG_CONFIG_HOME": "/cfg"}, "/home/d", "/cfg/teamkit/environments.json"},
        {"linux", nil, "/home/d", "/home/d/.config/teamkit/environments.json"},
    }
    for _, tt := range tests {
        t.Run(tt.goos, func(t *testing.T) {
            got, err := DefaultPath(LocationOptions{
                GOOS: tt.goos,
                Getenv: func(key string) string { return tt.env[key] },
                UserHomeDir: func() (string, error) { return tt.home, nil },
            })
            if err != nil || got != tt.want { t.Fatalf("got=%q want=%q err=%v", got, tt.want, err) }
        })
    }
}

func TestDefaultPath_RejectsUnavailableOrRelativeBases(t *testing.T) {
    tests := []LocationOptions{
        {GOOS: "windows", Getenv: func(string) string { return "relative" }, UserHomeDir: func() (string, error) { return "", nil }},
        {GOOS: "darwin", Getenv: func(string) string { return "" }, UserHomeDir: func() (string, error) { return "relative", nil }},
        {GOOS: "linux", Getenv: func(string) string { return "relative" }, UserHomeDir: func() (string, error) { return "/home/d", nil }},
        {GOOS: "plan9", Getenv: func(string) string { return "" }, UserHomeDir: func() (string, error) { return "/home/d", nil }},
    }
    for _, options := range tests {
        if got, err := DefaultPath(options); err == nil || got != "" { t.Fatalf("goos=%s got=%q err=%v", options.GOOS, got, err) }
    }
}

func TestStoreLoad_RejectsUnknownFieldsDuplicatesAndBounds(t *testing.T) {
    root := filepath.Join(testutil.TempDir(t), "kit")
    other := filepath.Join(testutil.TempDir(t), "other")
    cleanRoot, err := json.Marshal(root)
    if err != nil { t.Fatal(err) }
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
        if state != LoadCorrupt || err == nil { t.Fatalf("state=%v err=%v document=%s", state, err, document) }
    }
}
```

Add these complete bound/encoding tests:

```go
func TestStoreLoad_RejectsEntryByteAndUTF8Bounds(t *testing.T) {
    homes := make([]string, 65)
    for index := range homes { homes[index] = filepath.Join(testutil.TempDir(t), fmt.Sprintf("kit-%02d", index)) }
    document, err := json.Marshal(Registry{SchemaVersion: 1, Homes: homes})
    if err != nil { t.Fatal(err) }
    invalidUTF8 := []byte{'{', '"', 's', 'c', 'h', 'e', 'm', 'a', '_', 'v', 'e', 'r', 's', 'i', 'o', 'n', '"', ':', '1', ',', '"', 'h', 'o', 'm', 'e', 's', '"', ':', '[', '"', 0xff, '"', ']', '}'}
    for _, data := range [][]byte{document, bytes.Repeat([]byte{' '}, MaxBytes+1), invalidUTF8} {
        path := protectedRegistryFixture(t, data)
        _, state, loadErr := New(path).Load(context.Background())
        if state != LoadCorrupt || loadErr == nil { t.Fatalf("state=%v err=%v len=%d", state, loadErr, len(data)) }
    }
}
```

- [ ] **Step 2: Run registry tests and observe RED**

Run: `go test -mod=vendor -count=1 ./internal/registry`

Expected: build failure because package/types do not exist.

- [ ] **Step 3: Implement platform location resolution without filesystem I/O**

Implement the pure platform functions exactly; do not use host-dependent `filepath.IsAbs` or `filepath.Join` for a simulated `GOOS`:

```go
func isAbsoluteForOS(goos, value string) bool {
    if value == "" { return false }
    if goos != "windows" { return strings.HasPrefix(value, "/") }
    normalized := strings.ReplaceAll(value, "/", `\`)
    if len(normalized) >= 3 && ((normalized[0] >= 'A' && normalized[0] <= 'Z') || (normalized[0] >= 'a' && normalized[0] <= 'z')) && normalized[1] == ':' && normalized[2] == '\\' {
        return true
    }
    if !strings.HasPrefix(normalized, `\\`) { return false }
    fields := strings.FieldsFunc(strings.TrimPrefix(normalized, `\\`), func(r rune) bool { return r == '\\' })
    return len(fields) >= 2 && fields[0] != "" && fields[1] != ""
}

func joinForOS(goos string, parts ...string) string {
    if goos != "windows" { return path.Join(parts...) }
    if len(parts) == 0 { return "" }
    result := strings.TrimRight(strings.ReplaceAll(parts[0], "/", `\`), `\`)
    for _, part := range parts[1:] {
        part = strings.Trim(strings.ReplaceAll(part, "/", `\`), `\`)
        if part != "" { result += `\` + part }
    }
    return result
}

func DefaultPath(options LocationOptions) (string, error) {
    switch options.GOOS {
    case "windows":
        base := options.Getenv("LOCALAPPDATA")
        if !isAbsoluteForOS("windows", base) { return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE") }
        return joinForOS("windows", base, "TeamKit", "environments.json"), nil
    case "darwin":
        home, err := options.UserHomeDir()
        if err != nil { return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE: %w", err) }
        if !isAbsoluteForOS("darwin", home) { return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE") }
        return joinForOS("darwin", home, "Library", "Application Support", "TeamKit", "environments.json"), nil
    case "linux":
        base := options.Getenv("XDG_CONFIG_HOME")
        if base == "" {
            home, err := options.UserHomeDir()
            if err != nil { return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE: %w", err) }
            if !isAbsoluteForOS("linux", home) { return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE") }
            base = joinForOS("linux", home, ".config")
        }
        if !isAbsoluteForOS("linux", base) { return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE") }
        return joinForOS("linux", base, "teamkit", "environments.json"), nil
    default:
        return "", fmt.Errorf("REGISTRY_LOCATION_UNAVAILABLE: unsupported GOOS %q", options.GOOS)
    }
}
```

`NewDefault` calls `DefaultPath` only, stores any location error in `Store.locationErr`, and performs no `Lstat`, open, create, permission or DACL operation. `Load` maps `locationErr` to `(Registry{}, LoadUnavailable, err)`.

- [ ] **Step 4: Implement bounded strict Load and cached session state**

Use these constants and shape:

```go
const SchemaVersion = 1
const MaxEntries = 64
const MaxBytes int64 = 65536

type Registry struct {
    SchemaVersion int      `json:"schema_version"`
    Homes         []string `json:"homes"`
}

type LoadState uint8
const (
    LoadMissing LoadState = iota
    LoadValid
    LoadCorrupt
    LoadUnavailable
)
```

`Load` uses `os.Lstat` before creation: absent directory/file is `LoadMissing`; existing directory passes `validateRegistryDirectory`, existing file passes `privatefile.Validate`, and reading uses `io.LimitReader(file, MaxBytes+1)`. Define `var errRegistryContract = errors.New("REGISTRY_CONTRACT_INVALID")`. `decodeRegistry(data []byte) (Registry, error)` returns an error wrapping this sentinel for UTF-8/JSON/required-field/type/version/count/noncanonical/duplicate failures; path-security or filesystem failures not wrapping the sentinel remain operational. `Load` maps only `errRegistryContract` to `LoadCorrupt`; permission, owner, DACL, unsafe-component and other I/O failures map to `LoadUnavailable`.

Implement the strict decoder exactly; `contractError` always wraps `errRegistryContract`:

```go
func contractError(format string, args ...any) error {
    return fmt.Errorf("%w: %s", errRegistryContract, fmt.Sprintf(format, args...))
}

func requireDelim(decoder *json.Decoder, want json.Delim) error {
    token, err := decoder.Token()
    if err != nil { return contractError("JSON token: %v", err) }
    delimiter, ok := token.(json.Delim)
    if !ok || delimiter != want { return contractError("want delimiter %q", want) }
    return nil
}

func decodeRegistry(data []byte) (Registry, error) {
    if int64(len(data)) > MaxBytes { return Registry{}, contractError("document exceeds %d bytes", MaxBytes) }
    if !utf8.Valid(data) { return Registry{}, contractError("document is not UTF-8") }
    decoder := json.NewDecoder(bytes.NewReader(data))
    decoder.UseNumber()
    if err := requireDelim(decoder, '{'); err != nil { return Registry{}, err }
    seenFields, seenHomes := map[string]struct{}{}, map[string]struct{}{}
    result := Registry{}
    haveSchema, haveHomes := false, false
    for decoder.More() {
        token, err := decoder.Token()
        if err != nil { return Registry{}, contractError("field token: %v", err) }
        field, ok := token.(string)
        if !ok { return Registry{}, contractError("field name is not a string") }
        if _, duplicate := seenFields[field]; duplicate { return Registry{}, contractError("duplicate field %q", field) }
        seenFields[field] = struct{}{}
        switch field {
        case "schema_version":
            var raw json.RawMessage
            if err := decoder.Decode(&raw); err != nil || !bytes.Equal(bytes.TrimSpace(raw), []byte("1")) { return Registry{}, contractError("schema_version must equal integer 1") }
            result.SchemaVersion, haveSchema = SchemaVersion, true
        case "homes":
            if err := requireDelim(decoder, '['); err != nil { return Registry{}, err }
            for decoder.More() {
                if len(result.Homes) == MaxEntries { return Registry{}, contractError("homes exceeds %d entries", MaxEntries) }
                var raw json.RawMessage
                if err := decoder.Decode(&raw); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) { return Registry{}, contractError("home must be a string") }
                var home string
                if err := json.Unmarshal(raw, &home); err != nil { return Registry{}, contractError("home must be a string") }
                canonical, key, err := canonicalRegistryHome(home)
                if err != nil { return Registry{}, err }
                if _, duplicate := seenHomes[key]; duplicate { return Registry{}, contractError("duplicate home") }
                seenHomes[key] = struct{}{}
                result.Homes = append(result.Homes, canonical)
            }
            if err := requireDelim(decoder, ']'); err != nil { return Registry{}, err }
            haveHomes = true
        default:
            return Registry{}, contractError("unknown field %q", field)
        }
    }
    if err := requireDelim(decoder, '}'); err != nil { return Registry{}, err }
    if !haveSchema || !haveHomes { return Registry{}, contractError("required fields are missing") }
    var extra any
    if err := decoder.Decode(&extra); err != io.EOF { return Registry{}, contractError("trailing JSON") }
    return result, nil
}
```

Define package-private seams `var canonicalPath = pathsafe.CanonicalPath` and `var comparisonKey = pathsafe.ComparisonKey`. `canonicalRegistryHome(home string) (string, string, error)` requires nonempty absolute input and `home == filepath.Clean(home)` (therefore rejecting `..`, redundant separators and trailing separators); those validation failures wrap `errRegistryContract`. It calls `canonicalPath(home)` first, then `comparisonKey(canonical)`: `pathsafe.ErrUnsafe` wraps `errRegistryContract` because the stored home is invalid, while permission and other I/O failures are returned unchanged and therefore classify as `LoadUnavailable`. On success return `(canonical, key)`, so Windows registry entries are DOS/UNC final-path canonical rather than aliases/case variants; POSIX storage remains the exact cleaned string. Cache only a defensive copy after complete validation.

- [ ] **Step 5: Write failing MRU, no-rewrite and promotion-failure tests**

```go
func TestStorePromote_MovesCanonicalHomeToFrontAndCaps64(t *testing.T) {
    store := New(protectedRegistryPath(t))
    for i := 0; i < 65; i++ {
        home := filepath.Join(testutil.TempDir(t), fmt.Sprintf("kit-%02d", i))
        if err := store.Promote(context.Background(), home); err != nil { t.Fatal(err) }
    }
    got, state, err := store.Load(context.Background())
    if err != nil || state != LoadValid || len(got.Homes) != 64 { t.Fatalf("state=%v len=%d err=%v", state, len(got.Homes), err) }
    if !strings.HasSuffix(got.Homes[0], "kit-64") { t.Fatalf("MRU=%q", got.Homes[0]) }
}

func TestStorePromote_DoesNotRewriteCorruptOrUnavailableSession(t *testing.T) {
    path := protectedRegistryFixture(t, []byte(`{broken`))
    before, _ := os.ReadFile(path)
    store := New(path)
    _, state, _ := store.Load(context.Background())
    if state != LoadCorrupt { t.Fatalf("state=%v", state) }
    if err := store.Promote(context.Background(), filepath.Join(testutil.TempDir(t), "kit")); !errors.Is(err, ErrReadOnlySession) { t.Fatalf("err=%v", err) }
    after, _ := os.ReadFile(path)
    if !bytes.Equal(before, after) { t.Fatal("corrupt registry was rewritten") }
}

func TestStoreLoad_PathContractFailureIsCorruptButComparisonIOIsUnavailable(t *testing.T) {
    corrupt := protectedRegistryFixture(t, []byte(`{"schema_version":1,"homes":["relative"]}`))
    if _, state, err := New(corrupt).Load(context.Background()); state != LoadCorrupt || err == nil { t.Fatalf("state=%v err=%v", state, err) }
    home := filepath.Join(testutil.TempDir(t), "kit")
    document, err := json.Marshal(Registry{SchemaVersion: 1, Homes: []string{home}})
    if err != nil { t.Fatal(err) }
    unavailable := protectedRegistryFixture(t, document)
    original := comparisonKey
    comparisonKey = func(string) (string, error) { return "", fs.ErrPermission }
    defer func() { comparisonKey = original }()
    if _, state, err := New(unavailable).Load(context.Background()); state != LoadUnavailable || !errors.Is(err, fs.ErrPermission) { t.Fatalf("state=%v err=%v", state, err) }
}
```

Define the registry test paths exactly once in `registry_test.go`:

```go
func protectedRegistryPath(t *testing.T) string {
    t.Helper()
    directory := filepath.Join(testutil.TempDir(t), "registry")
    if err := ensureRegistryDirectory(directory); err != nil { t.Fatal(err) }
    return filepath.Join(directory, "environments.json")
}

func protectedRegistryFixture(t *testing.T, document []byte) string {
    t.Helper()
    path := protectedRegistryPath(t)
    if err := writeRegistryAtomic(path, document); err != nil { t.Fatal(err) }
    return path
}
```

Update string fixtures to call `protectedRegistryFixture(t, []byte(document))` and MRU tests to call `New(protectedRegistryPath(t))`.

- [ ] **Step 6: Implement Promote as an owner-only bounded atomic MRU update**

Define `var ErrReadOnlySession = errors.New("REGISTRY_READ_ONLY_SESSION")`. Give `Store` a `sync.Mutex`, cached `loaded/state/snapshot`, and private `loadLocked(context.Context)` so `Load` and `Promote` do not recurse while holding the mutex. `Promote` checks context, calls `canonicalRegistryHome`, loads once when necessary, rejects cached `LoadCorrupt`/`LoadUnavailable`, removes every equal comparison key, prepends the canonical storage string, truncates to 64, marshals with one trailing newline, rejects output over `MaxBytes`, and calls only `writeRegistryAtomic`. Update the cached registry only after a successful replace; `Load` and the cache always return defensive `Homes` copies.

- [ ] **Step 7: Implement and test the registry-only protected atomic primitive**

Use these exact registry-only signatures; do not edit `internal/privatefile`, `internal/workspace` or any global parent-directory policy:

```go
var createRegistryTemp = privatefile.CreateTemp

func writeRegistryAtomic(target string, data []byte) error {
    directory := filepath.Dir(target)
    if err := ensureRegistryDirectory(directory); err != nil { return err }
    if err := validateRegistryDirectory(directory); err != nil { return err }
    if err := privatefile.Validate(target); err != nil { return err }
    temporary, err := createRegistryTemp(directory, ".teamkit-registry-", ".tmp", 0o600)
    if err != nil { return err }
    temporaryPath := temporary.Name()
    defer os.Remove(temporaryPath)
    if _, err := temporary.Write(data); err != nil { _ = temporary.Close(); return err }
    if err := temporary.Sync(); err != nil { _ = temporary.Close(); return err }
    if err := temporary.Close(); err != nil { return err }
    if err := validateRegistryDirectory(directory); err != nil { return err }
    if err := privatefile.Validate(temporaryPath); err != nil { return err }
    if err := privatefile.Validate(target); err != nil { return err }
    if err := replaceRegistryFile(temporaryPath, target); err != nil { return err }
    return privatefile.Validate(target)
}
```

`secure_other.go` defines `ensureRegistryDirectory(string) error` with `pathsafe.EnsureDirectory(path, 0o700)` followed by `validateRegistryDirectory`; validation requires `Lstat` directory, no redirect, `info.Mode().Perm() == 0o700`, and `Stat_t.Uid == os.Geteuid()`. `replaceRegistryFile(source,target string) error` calls `os.Rename`, opens `filepath.Dir(target)`, calls `Sync`, and closes it. `secure_windows.go` defines the same signatures: obtain the current SID, build SDDL `"O:"+sid+"D:P(A;;FA;;;"+sid+")"`, create missing registry directories component-by-component with that descriptor, validate owner/current-user-only protected DACL on every existing registry directory, and replace with `windows.MoveFileEx(source,target,MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)`. Neither platform helper accepts a workspace path.

In `registry_windows_test.go`, keep `requireRegistryFullAccessDACL` below and add `TestRegistryAtomicWrite_WindowsProtectsDirectoryTemporaryAndFinal`: wrap `createRegistryTemp` to capture its returned name, defer restoration, call `writeRegistryAtomic` twice, and call `requireRegistryFullAccessDACL` on the directory, captured temporary before replacement, and final file; assert the second body. Add a junction test that points the registry directory at an external directory containing a sentinel, requires an error wrapping `pathsafe.ErrUnsafe`/`privatefile.ErrUnsafePermissions`, and asserts sentinel bytes unchanged.

In `registry_other_test.go`, add the complete native permission/replacement test:

```go
func TestRegistryAtomicWrite_POSIXProtectsDirectoryTemporaryAndFinal(t *testing.T) {
    path := protectedRegistryPath(t)
    var temporaryMode fs.FileMode
    original := createRegistryTemp
    createRegistryTemp = func(directory, prefix, suffix string, perm fs.FileMode) (*os.File, error) {
        file, err := original(directory, prefix, suffix, perm)
        if err == nil { info, statErr := file.Stat(); if statErr != nil { _ = file.Close(); return nil, statErr }; temporaryMode = info.Mode().Perm() }
        return file, err
    }
    defer func() { createRegistryTemp = original }()
    if err := writeRegistryAtomic(path, []byte("first\n")); err != nil { t.Fatal(err) }
    if err := writeRegistryAtomic(path, []byte("second\n")); err != nil { t.Fatal(err) }
    directoryInfo, err := os.Stat(filepath.Dir(path)); if err != nil { t.Fatal(err) }
    fileInfo, err := os.Stat(path); if err != nil { t.Fatal(err) }
    body, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
    stat, ok := directoryInfo.Sys().(*syscall.Stat_t)
    if !ok || int(stat.Uid) != os.Geteuid() || directoryInfo.Mode().Perm() != 0o700 || temporaryMode != 0o600 || fileInfo.Mode().Perm() != 0o600 || string(body) != "second\n" {
        t.Fatalf("uid=%v dir=%o temp=%o file=%o body=%q", stat, directoryInfo.Mode().Perm(), temporaryMode, fileInfo.Mode().Perm(), body)
    }
}

func TestRegistryAtomicWrite_POSIXRejectsSymlinkWithoutTouchingSentinel(t *testing.T) {
    directory := filepath.Join(testutil.TempDir(t), "registry")
    if err := ensureRegistryDirectory(directory); err != nil { t.Fatal(err) }
    sentinel := filepath.Join(testutil.TempDir(t), "sentinel")
    if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil { t.Fatal(err) }
    target := filepath.Join(directory, "environments.json")
    if err := os.Symlink(sentinel, target); err != nil { t.Skipf("symlink unavailable: %v", err) }
    if err := writeRegistryAtomic(target, []byte("changed")); err == nil { t.Fatal("symlink accepted") }
    body, err := os.ReadFile(sentinel)
    if err != nil || string(body) != "unchanged" { t.Fatalf("sentinel=%q err=%v", body, err) }
}
```

```go
func requireRegistryFullAccessDACL(t *testing.T, path string) {
    t.Helper()
    descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
    if err != nil { t.Fatal(err) }
    current, err := windows.GetCurrentProcessToken().GetTokenUser()
    if err != nil { t.Fatal(err) }
    owner, _, err := descriptor.Owner()
    if err != nil || owner == nil || !owner.Equals(current.User.Sid) { t.Fatalf("owner=%v err=%v", owner, err) }
    control, _, err := descriptor.Control()
    if err != nil || control&windows.SE_DACL_PROTECTED == 0 { t.Fatalf("control=%v err=%v", control, err) }
    dacl, _, err := descriptor.DACL()
    if err != nil || dacl == nil || dacl.AceCount != 1 { t.Fatalf("dacl=%v err=%v", dacl, err) }
    var ace *windows.ACCESS_ALLOWED_ACE
    if err := windows.GetAce(dacl, 0, &ace); err != nil { t.Fatal(err) }
    sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
    if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !sid.Equals(current.User.Sid) || ace.Mask != windows.FILE_ALL_ACCESS {
        t.Fatalf("type=%d sid=%v mask=%#x want=%#x", ace.Header.AceType, sid, ace.Mask, windows.FILE_ALL_ACCESS)
    }
}

func TestRegistryAtomicWrite_WindowsProtectsDirectoryTemporaryAndFinal(t *testing.T) {
    path := protectedRegistryPath(t)
    original := createRegistryTemp
    temporaryChecks := 0
    createRegistryTemp = func(directory, prefix, suffix string, perm fs.FileMode) (*os.File, error) {
        file, err := original(directory, prefix, suffix, perm)
        if err == nil { requireRegistryFullAccessDACL(t, file.Name()); temporaryChecks++ }
        return file, err
    }
    defer func() { createRegistryTemp = original }()
    if err := writeRegistryAtomic(path, []byte("first\n")); err != nil { t.Fatal(err) }
    if err := writeRegistryAtomic(path, []byte("second\n")); err != nil { t.Fatal(err) }
    requireRegistryFullAccessDACL(t, filepath.Dir(path))
    requireRegistryFullAccessDACL(t, path)
    body, err := os.ReadFile(path)
    if err != nil || temporaryChecks != 2 || string(body) != "second\n" { t.Fatalf("checks=%d body=%q err=%v", temporaryChecks, body, err) }
}

func TestRegistryAtomicWrite_WindowsRejectsJunctionDirectory(t *testing.T) {
    parent, external := testutil.TempDir(t), testutil.TempDir(t)
    sentinel := filepath.Join(external, "sentinel")
    if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil { t.Fatal(err) }
    junction := filepath.Join(parent, "registry")
    output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput()
    if err != nil { t.Fatalf("mklink: %v: %s", err, output) }
    writeErr := writeRegistryAtomic(filepath.Join(junction, "environments.json"), []byte("changed"))
    if writeErr == nil { t.Fatal("junction accepted") }
    body, readErr := os.ReadFile(sentinel)
    if readErr != nil || string(body) != "unchanged" { t.Fatalf("sentinel=%q err=%v writeErr=%v", body, readErr, writeErr) }
}
```

- [ ] **Step 8: Run registry and security packages to GREEN**

Run: `go test -mod=vendor -count=1 ./internal/registry ./internal/privatefile ./internal/workspace ./internal/pathsafe`

Expected: PASS for all four packages; unchanged `privatefile.WriteAtomic` and `workspace.WriteFileAtomic` regressions remain green.

- [ ] **Step 9: Commit the registry**

```powershell
git add internal/registry
git commit -m "feat(registry): add protected paths-only MRU store"
```

### Task 3: Bounded operation-first environment inspector

**Files:**
- Modify: `internal/state/store.go:23-147`
- Modify: `internal/state/store_test.go`
- Create: `internal/environment/environment.go`
- Create: `internal/environment/inspect.go`
- Create: `internal/environment/inspect_test.go`
- Create: `internal/environment/inspect_windows_test.go`

**Interfaces:**
- Produces: `state.MaxOperationBytes = 1 << 20` and bounded `Store.LoadOperation` without changing its signature.
- Produces: `environment.InspectionState` values `Ready`, `RetryRequired`, `Foreign`, `InspectionFailed`.
- Produces: typed `environment.Error { State InspectionState; Detail string; Cause error }`; `errors.As` is the only way CLI orchestration translates `Foreign`, `InspectionFailed` and `RetryRequired` into public operational codes.
- Produces: `environment.AddState` values `AddTargetReady`, `AddWorkspaceExists`.
- Produces: `environment.VerifiedEnvironment { Home string; Desired domain.DesiredState; Pending bool }`.
- Produces: `environment.Inspector` with `Inspect(context.Context, string) (VerifiedEnvironment, InspectionState, error)` and `ClassifyAdd(context.Context, string) (AddState, error)`.
- Produces: `environment.NewInspector() Inspector` with no filesystem I/O at construction.
- Consumes: `state.New(home).LoadOperation`, `reconcile.RetryActionsChecked`, `config.ParseDotenv`, `pathsafe.ValidateDirectory/ValidateRegular`.

- [ ] **Step 1: Write a failing oversized-operation test**

```go
func TestStoreLoadOperation_RejectsDocumentAboveBound(t *testing.T) {
    root := testutil.TempDir(t)
    if err := os.Mkdir(filepath.Join(root, ".teamkit"), 0o700); err != nil { t.Fatal(err) }
    path := filepath.Join(root, ".teamkit", "operation.json")
    if err := os.WriteFile(path, bytes.Repeat([]byte("x"), MaxOperationBytes+1), 0o600); err != nil { t.Fatal(err) }
    store, err := New(root)
    if err != nil { t.Fatal(err) }
    if _, _, err := store.LoadOperation(); err == nil || !strings.Contains(err.Error(), "OPERATION_TOO_LARGE") {
        t.Fatalf("err=%v", err)
    }
}
```

- [ ] **Step 2: Run state test to verify RED, then implement bounded reading**

Run: `go test -mod=vendor -count=1 ./internal/state -run TestStoreLoadOperation_RejectsDocumentAboveBound`

Expected RED: `undefined: MaxOperationBytes`.

Implement and use this helper for `operation.json`, `plan.json` and `receipt.json`; preserve the existing strict decoders and wrap the caller-specific `*_TOO_LARGE` identity at each call site:

```go
func readBoundedRegular(path string, limit int64) ([]byte, error) {
    if err := pathsafe.ValidateRegular(path); err != nil { return nil, err }
    file, err := os.Open(path)
    if err != nil { return nil, err }
    defer file.Close()
    data, err := io.ReadAll(io.LimitReader(file, limit+1))
    if err != nil { return nil, err }
    if int64(len(data)) > limit { return nil, fmt.Errorf("DOCUMENT_TOO_LARGE") }
    return data, nil
}
```

- [ ] **Step 3: Write failing inspector tests proving operation-first behavior**

```go
func TestInspector_PendingFirstRunWinsBeforeMissingOwnerAndEnv(t *testing.T) {
    root, desired, plan := pendingOperationFixture(t)
    if _, err := os.Lstat(filepath.Join(root, ".teamkit", "owner")); !errors.Is(err, os.ErrNotExist) { t.Fatalf("owner exists: %v", err) }
    if _, err := os.Lstat(filepath.Join(root, ".env")); !errors.Is(err, os.ErrNotExist) { t.Fatalf("env exists: %v", err) }
    got, state, err := NewInspector().Inspect(context.Background(), root)
    var inspectionErr *Error
    if state != RetryRequired || !got.Pending || !errors.As(err, &inspectionErr) || inspectionErr.State != RetryRequired { t.Fatalf("got=%#v state=%v err=%T %v", got, state, err, err) }
    if got.Desired.Project() != desired.Project() || got.Desired.KitHome() != root || len(plan.Actions) == 0 {
        t.Fatalf("got=%#v desired=%#v plan=%#v", got, desired, plan)
    }
}

func TestInspector_PendingOperationDoesNotReadPoisonedOwnerOrEnv(t *testing.T) {
    root, _, _ := pendingOperationFixture(t)
    if err := os.WriteFile(filepath.Join(root, ".teamkit", "owner"), []byte("wrong\n"), 0o600); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=CANARY\n"), 0o600); err != nil { t.Fatal(err) }
    _, state, err := NewInspector().Inspect(context.Background(), root)
    var inspectionErr *Error
    if state != RetryRequired || !errors.As(err, &inspectionErr) || inspectionErr.State != RetryRequired { t.Fatalf("state=%v err=%T %v", state, err, err) }
}
```

Add this complete table test to the same file:

```go
func TestInspector_ClassifiesStructuralFailuresWithTypedErrors(t *testing.T) {
    tests := []struct {
        name      string
        mutate    func(t *testing.T, root string)
        wantState InspectionState
    }{
        {"owner mismatch", func(t *testing.T, root string) { writeFile(t, filepath.Join(root, ".teamkit", "owner"), "wms\n") }, Foreign},
        {"home mismatch", func(t *testing.T, root string) { rewriteEnvHome(t, root, filepath.Join(root, "other")) }, Foreign},
        {"malformed env", func(t *testing.T, root string) { writeFile(t, filepath.Join(root, ".env"), "TOKEN=CANARY\n") }, Foreign},
        {"oversized env", func(t *testing.T, root string) { writeBytes(t, filepath.Join(root, ".env"), bytes.Repeat([]byte("x"), maxPublicEnvBytes+1)) }, Foreign},
        {"missing root", func(t *testing.T, root string) { if err := os.RemoveAll(root); err != nil { t.Fatal(err) } }, Foreign},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            root, _ := readyEnvironmentFixture(t)
            test.mutate(t, root)
            _, state, err := NewInspector().Inspect(context.Background(), root)
            var inspectionErr *Error
            if state != test.wantState || !errors.As(err, &inspectionErr) || inspectionErr.State != test.wantState {
                t.Fatalf("state=%v err=%T %v", state, err, err)
            }
        })
    }
}
```

Define the referenced helpers in `inspect_test.go`:

```go
func writeBytes(t *testing.T, path string, data []byte) {
    t.Helper()
    if err := os.WriteFile(path, data, 0o600); err != nil { t.Fatal(err) }
}

func writeFile(t *testing.T, path, data string) { t.Helper(); writeBytes(t, path, []byte(data)) }

func rewriteEnvHome(t *testing.T, root, home string) {
    t.Helper()
    desired := readyDesiredState(t, root)
    values := config.Encode(desired)
    values["KIT_ALL_TEAM_HOME"] = home
    if err := workspace.WritePublicEnv(filepath.Join(root, ".env"), values); err != nil { t.Fatal(err) }
}

func pendingOperationFixture(t *testing.T) (string, domain.DesiredState, reconcile.OperationPlan) {
    t.Helper()
    root := filepath.Join(testutil.TempDir(t), "kit")
    desired := readyDesiredState(t, root)
    plan := reconcile.OperationPlan{ContractHash: "fixture-contract", Actions: []reconcile.Action{{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true}}}
    store, err := state.New(root)
    if err != nil { t.Fatal(err) }
    if err := store.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil { t.Fatal(err) }
    return root, desired, plan
}

func TestInspector_PendingReceiptMustMatchCandidateBeforeItIsDisplayable(t *testing.T) {
    candidate := filepath.Join(testutil.TempDir(t), "candidate")
    desiredHome := filepath.Join(testutil.TempDir(t), "receipt-home")
    desired := readyDesiredState(t, desiredHome)
    plan := reconcile.OperationPlan{ContractHash: "fixture-contract", Actions: []reconcile.Action{{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true}}}
    store, err := state.New(candidate)
    if err != nil { t.Fatal(err) }
    if err := store.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil { t.Fatal(err) }
    got, inspectionState, err := NewInspector().Inspect(context.Background(), candidate)
    var inspectionErr *Error
    if inspectionState != Foreign || !errors.As(err, &inspectionErr) || inspectionErr.State != Foreign || got.Pending {
        t.Fatalf("got=%#v state=%v err=%T %v", got, inspectionState, err, err)
    }
}
```

Define the remaining ready-state helpers exactly:

```go
func readyDesiredState(t *testing.T, root string) domain.DesiredState {
    t.Helper()
    desired, err := domain.NewDesiredState(domain.DesiredStateInput{
        OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
        KitHome: root, HermesHome: filepath.Join(filepath.Dir(root), "hermes"), HermesVersion: "0.20.2",
        Project: domain.ProjectAPA, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
    })
    if err != nil { t.Fatal(err) }
    return desired
}

func readyEnvironmentFixture(t *testing.T) (string, domain.DesiredState) {
    t.Helper()
    root := filepath.Join(testutil.TempDir(t), "kit")
    if err := os.MkdirAll(filepath.Join(root, ".teamkit"), 0o700); err != nil { t.Fatal(err) }
    desired := readyDesiredState(t, root)
    writeFile(t, filepath.Join(root, ".teamkit", "owner"), "apa\n")
    if err := workspace.WritePublicEnv(filepath.Join(root, ".env"), config.Encode(desired)); err != nil { t.Fatal(err) }
    return root, desired
}
```

Add these exact safety/I/O tests after the fixture helpers:

```go
func TestInspector_RejectsEmptyRelativeAndRedirectedMetadata(t *testing.T) {
    inspector := NewInspector()
    for _, home := range []string{"", "relative"} {
        _, state, err := inspector.Inspect(context.Background(), home)
        if state != Foreign || err == nil || !strings.Contains(err.Error(), "FOREIGN_WORKSPACE") { t.Fatalf("home=%q state=%v err=%v", home, state, err) }
    }
    for _, name := range []string{"owner", ".env"} {
        t.Run(name, func(t *testing.T) {
            root, _ := readyEnvironmentFixture(t)
            target := filepath.Join(root, ".teamkit", "owner")
            if name == ".env" { target = filepath.Join(root, ".env") }
            sentinel := filepath.Join(testutil.TempDir(t), "sentinel")
            writeFile(t, sentinel, "TEAMKIT_SECRET_CANARY\n")
            if err := os.Remove(target); err != nil { t.Fatal(err) }
            if err := os.Symlink(sentinel, target); err != nil { t.Skipf("symlink unavailable: %v", err) }
            _, state, err := inspector.Inspect(context.Background(), root)
            if state != Foreign || err == nil || !strings.Contains(err.Error(), "FOREIGN_WORKSPACE") { t.Fatalf("state=%v err=%v", state, err) }
            body, readErr := os.ReadFile(sentinel)
            if readErr != nil || string(body) != "TEAMKIT_SECRET_CANARY\n" { t.Fatalf("sentinel=%q err=%v", body, readErr) }
        })
    }
}

func TestInspector_UnreadablePublicEnvIsInspectionFailure(t *testing.T) {
    if runtime.GOOS == "windows" { t.Skip("mode-bit denial is covered by native Windows ACL tests") }
    root, _ := readyEnvironmentFixture(t)
    path := filepath.Join(root, ".env")
    if err := os.Chmod(path, 0); err != nil { t.Fatal(err) }
    t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
    _, state, err := NewInspector().Inspect(context.Background(), root)
    if err == nil { t.Skip("native executor can read mode-zero fixture") }
    if state != InspectionFailed || err == nil || !strings.Contains(err.Error(), "WORKSPACE_INSPECTION_FAILED") { t.Fatalf("state=%v err=%v", state, err) }
}
```

- [ ] **Step 4: Run inspector tests and observe RED**

Run: `go test -mod=vendor -count=1 ./internal/environment -run 'TestInspector'`

Expected: build failure because `internal/environment` does not exist.

- [ ] **Step 5: Implement exact environment types and typed error classification**

```go
type InspectionState uint8
const (
    Ready InspectionState = iota
    RetryRequired
    Foreign
    InspectionFailed
)
func (s InspectionState) String() string {
    switch s {
    case Ready: return "READY"
    case RetryRequired: return "RETRY_REQUIRED"
    case Foreign: return "FOREIGN_WORKSPACE"
    case InspectionFailed: return "WORKSPACE_INSPECTION_FAILED"
    default: return "WORKSPACE_INSPECTION_FAILED"
    }
}

type AddState uint8
const (
    AddTargetReady AddState = iota
    AddWorkspaceExists
)

type VerifiedEnvironment struct {
    Home    string
    Desired domain.DesiredState
    Pending bool
}

type Inspector interface {
    Inspect(context.Context, string) (VerifiedEnvironment, InspectionState, error)
    ClassifyAdd(context.Context, string) (AddState, error)
}

type Error struct {
    State  InspectionState
    Detail string
    Cause  error
}

func (e *Error) Error() string {
    if e.Detail == "" { return e.State.String() }
    return e.State.String() + ": " + e.Detail
}
func (e *Error) Unwrap() error { return e.Cause }
func inspectionError(state InspectionState, detail string, cause error) error {
    return &Error{State: state, Detail: detail, Cause: cause}
}
```

Implement `Inspect` in this exact order:

1. Reject empty/nonabsolute roots and `pathsafe.ErrUnsafe` as `inspectionError(Foreign, ...)`; classify other `Lstat`/open/read errors as `InspectionFailed`.
2. Require an existing readable root directory and regular `.teamkit` directory.
3. `Lstat(.teamkit/operation.json)`. If present, call bounded `state.New(root).LoadOperation()` before any owner/`.env` access. Convert its receipt with `DesiredState()`.
4. Before considering the operation pending, call `pathsafe.ComparisonKey(root)` and `pathsafe.ComparisonKey(receiptDesired.KitHome())`. Any unsafe key or unequal keys is typed `Foreign`; any operational key failure is typed `InspectionFailed`.
5. Call `reconcile.RetryActionsChecked(plan, receipt)`. A nonempty action slice returns `VerifiedEnvironment{Home: receiptDesired.KitHome(), Desired: receiptDesired, Pending: true}`, `RetryRequired`, and `inspectionError(RetryRequired, "operation receipt has incomplete actions", nil)`. A complete operation continues to public metadata inspection.
6. Bounded-read `.teamkit/owner` at 256 bytes and `.env` at 65536 bytes through `pathsafe.ValidateRegular`, require UTF-8, then parse `.env` with `config.ParseDotenv`.
7. Compute safe comparison keys for the candidate and parsed `desired.KitHome()` again, require equality, and require the trimmed owner equals `string(desired.Project())`.
8. Return `VerifiedEnvironment{Home: desired.KitHome(), Desired: desired}`, `Ready`, `nil`. Never return the registry/env/manual spelling when public desired state contains a safely equivalent canonical spelling.

- [ ] **Step 6: Implement add classification with no mutation**

Implement `ClassifyAdd` as a read-only state machine: validate an absolute root with `pathsafe.ValidateDirectory`; `Lstat` missing final root returns `AddTargetReady`; an existing empty directory (`os.ReadDir` length zero) returns `AddTargetReady`; a non-directory/unsafe/nonempty foreign root returns typed `*environment.Error{State: Foreign}`; a permission/I/O failure returns typed `InspectionFailed`. For a nonempty root, call `Inspect`; `Ready` maps to `AddWorkspaceExists`, while `RetryRequired`, `Foreign`, and `InspectionFailed` return the same typed `*environment.Error` unchanged. It never creates or removes a path.

- [ ] **Step 7: Add Windows junction/reparse inspector tests**

Keep these tests in `inspect_windows_test.go` under `//go:build windows`:

```go
func TestInspector_WindowsCandidateAliasMatchesDesiredAndReturnsDesiredHome(t *testing.T) {
    root, desired := readyEnvironmentFixture(t)
    candidate := strings.ToUpper(root)
    got, state, err := NewInspector().Inspect(context.Background(), candidate)
    if err != nil || state != Ready { t.Fatalf("state=%v err=%v", state, err) }
    if got.Home != desired.KitHome() { t.Fatalf("home=%q want desired home %q", got.Home, desired.KitHome()) }
}

func TestInspector_WindowsRejectsJunctionRoot(t *testing.T) {
    parent := testutil.TempDir(t)
    external, _ := readyEnvironmentFixture(t)
    sentinel := filepath.Join(external, "sentinel")
    writeFile(t, sentinel, "unchanged")
    junction := filepath.Join(parent, "junction")
    output, mkErr := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput()
    if mkErr != nil { t.Fatalf("mklink: %v: %s", mkErr, output) }
    _, state, err := NewInspector().Inspect(context.Background(), junction)
    if state != Foreign || err == nil { t.Fatalf("state=%v err=%v", state, err) }
    data, readErr := os.ReadFile(sentinel)
    if readErr != nil || string(data) != "unchanged" { t.Fatalf("sentinel=%q err=%v", data, readErr) }
}
```

- [ ] **Step 8: Run state/environment tests to GREEN**

Run: `go test -mod=vendor -count=1 ./internal/state ./internal/environment`

Expected: PASS, including pending first-run without owner or `.env`.

- [ ] **Step 9: Commit operation inspection**

```powershell
git add internal/state internal/environment
git commit -m "feat(environment): inspect Team Kit roots operation first"
```

### Task 4: Candidate precedence, dedupe and displayable discovery

**Files:**
- Create: `internal/environment/discovery.go`
- Create: `internal/environment/discovery_test.go`

**Interfaces:**
- Produces: `CandidateSource` constants `SourceExplicit`, `SourceRegistry`, `SourceEnvironment`, `SourceManual`.
- Produces: `Candidate { Home string; Source CandidateSource }`.
- Produces: `DiscoveryRequest { ExplicitHome string; Explicit bool; RegistryHomes []string; EnvironmentHome string }`.
- Produces: `DiscoveryResult { Environments []VerifiedEnvironment; Warnings []Warning; ManualRequired bool }`.
- Produces: `Warning { Source CandidateSource; Home string; State InspectionState }`; warning text never embeds the underlying error string.
- Produces: `Discover(context.Context, DiscoveryRequest, Inspector) (DiscoveryResult, error)`.
- Produces: `Warning.String() string` whose source/path is ASCII-escaped and bounded to 1536 bytes, preventing terminal-control injection.
- Consumes: Task 1 `pathsafe.ComparisonKey`; Task 3 `Inspector`.

- [ ] **Step 1: Write precedence/dedupe/fatality tests with a recording inspector**

```go
func TestDiscover_RegistryThenEnvironmentDedupesAndKeepsReceipt(t *testing.T) {
    base := testutil.TempDir(t)
    mru, pendingHome, bad := filepath.Join(base, "mru"), filepath.Join(base, "pending"), filepath.Join(base, "bad")
    inspector := &recordingInspector{results: map[string]inspectResult{
        mru: {verified: verified(t, mru, "apa"), state: Ready},
        pendingHome: {verified: pending(t, pendingHome), state: RetryRequired, err: inspectionError(RetryRequired, "pending", nil)},
        bad: {state: Foreign, err: inspectionError(Foreign, "foreign", nil)},
    }}
    got, err := Discover(context.Background(), DiscoveryRequest{
        RegistryHomes: []string{mru, bad, pendingHome}, EnvironmentHome: mru,
    }, inspector)
    if err != nil { t.Fatal(err) }
    if !reflect.DeepEqual(inspector.calls, []string{mru, bad, pendingHome}) { t.Fatalf("calls=%#v", inspector.calls) }
    if len(got.Environments) != 2 || !got.Environments[1].Pending || len(got.Warnings) != 1 { t.Fatalf("got=%#v", got) }
}

func TestDiscover_ExplicitFailureIsFatalWithoutFallback(t *testing.T) {
    base := testutil.TempDir(t)
    explicit, valid, environmentHome := filepath.Join(base, "explicit"), filepath.Join(base, "valid"), filepath.Join(base, "env")
    inspector := &recordingInspector{results: map[string]inspectResult{explicit: {state: Foreign, err: inspectionError(Foreign, "foreign", nil)}}}
    _, err := Discover(context.Background(), DiscoveryRequest{Explicit: true, ExplicitHome: explicit, RegistryHomes: []string{valid}, EnvironmentHome: environmentHome}, inspector)
    var inspectionErr *Error
    if !errors.As(err, &inspectionErr) || inspectionErr.State != Foreign || !reflect.DeepEqual(inspector.calls, []string{explicit}) { t.Fatalf("calls=%#v err=%v", inspector.calls, err) }
}
```

Define the discovery fake in the same file:

```go
type inspectResult struct { verified VerifiedEnvironment; state InspectionState; err error }
type recordingInspector struct { results map[string]inspectResult; calls []string }
func (r *recordingInspector) Inspect(_ context.Context, home string) (VerifiedEnvironment, InspectionState, error) {
    r.calls = append(r.calls, home)
    result := r.results[home]
    return result.verified, result.state, result.err
}
func (r *recordingInspector) ClassifyAdd(context.Context, string) (AddState, error) {
    return AddTargetReady, nil
}
func verified(t *testing.T, home, project string) VerifiedEnvironment {
    t.Helper()
    desired, err := domain.NewDesiredState(domain.DesiredStateInput{OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true, KitHome: home, HermesHome: filepath.Join(filepath.Dir(home), "hermes"), HermesVersion: "0.20.2", Project: domain.ProjectID(project), Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills})
    if err != nil { t.Fatal(err) }
    return VerifiedEnvironment{Home: home, Desired: desired}
}
func pending(t *testing.T, home string) VerifiedEnvironment { t.Helper(); result := verified(t, home, "wms"); result.Pending = true; return result }
```

Add this complete behavior test after the fake:

```go
func TestDiscover_NoDisplayableRequiresManualAndWarningIsBoundedEscaped(t *testing.T) {
    unsafePath := filepath.Join(testutil.TempDir(t), strings.Repeat("д", 300)+"\n\x1b[31m")
    inspector := &recordingInspector{results: map[string]inspectResult{unsafePath: {state: Foreign, err: inspectionError(Foreign, "foreign", nil)}}}
    got, err := Discover(context.Background(), DiscoveryRequest{RegistryHomes: []string{unsafePath}}, inspector)
    if err != nil { t.Fatal(err) }
    if !got.ManualRequired || len(got.Environments) != 0 || len(got.Warnings) != 1 { t.Fatalf("got=%#v", got) }
    warning := got.Warnings[0].String()
    if len(warning) > 1536 || strings.Contains(warning, "\n") || strings.ContainsRune(warning, '\x1b') { t.Fatalf("unsafe warning len=%d %q", len(warning), warning) }
    if !strings.Contains(warning, `\n`) || !strings.Contains(warning, `\u001b`) { t.Fatalf("warning not escaped: %q", warning) }
}

func TestDiscover_ContextCancellationStopsBeforeSecondCandidate(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    inspector := &cancelingInspector{cancel: cancel}
    base := testutil.TempDir(t)
    first, second := filepath.Join(base, "first"), filepath.Join(base, "second")
    _, err := Discover(ctx, DiscoveryRequest{RegistryHomes: []string{first, second}}, inspector)
    if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(inspector.calls, []string{first}) { t.Fatalf("calls=%#v err=%v", inspector.calls, err) }
}

func TestDiscover_ComparisonKeyFailuresFollowSourceFatality(t *testing.T) {
    inspector := &recordingInspector{}
    got, err := Discover(context.Background(), DiscoveryRequest{RegistryHomes: []string{"relative-registry"}, EnvironmentHome: "relative-env"}, inspector)
    if err != nil || !got.ManualRequired || len(got.Warnings) != 2 || len(inspector.calls) != 0 { t.Fatalf("got=%#v calls=%#v err=%v", got, inspector.calls, err) }
    _, err = Discover(context.Background(), DiscoveryRequest{Explicit: true, ExplicitHome: "relative-explicit"}, inspector)
    var inspectionErr *Error
    if !errors.As(err, &inspectionErr) || inspectionErr.State != Foreign || len(inspector.calls) != 0 { t.Fatalf("calls=%#v err=%T %v", inspector.calls, err, err) }
}

func TestDiscover_SameUncomparableRegistryAndEnvironmentPathWarnsOnce(t *testing.T) {
    inspector := &recordingInspector{}
    got, err := Discover(context.Background(), DiscoveryRequest{RegistryHomes: []string{"same-relative"}, EnvironmentHome: "same-relative"}, inspector)
    if err != nil || !got.ManualRequired || len(got.Warnings) != 1 || len(inspector.calls) != 0 {
        t.Fatalf("got=%#v calls=%#v err=%v", got, inspector.calls, err)
    }
}

func TestDiscover_RetryRequiredRequiresMatchingTypedError(t *testing.T) {
    home := filepath.Join(testutil.TempDir(t), "pending")
    inspector := &recordingInspector{results: map[string]inspectResult{
        home: {verified: pending(t, home), state: RetryRequired, err: nil},
    }}
    _, err := Discover(context.Background(), DiscoveryRequest{RegistryHomes: []string{home}}, inspector)
    var inspectionErr *Error
    if !errors.As(err, &inspectionErr) || inspectionErr.State != InspectionFailed {
        t.Fatalf("err=%T %v", err, err)
    }
}
```

Define the cancellation adapter in `discovery_test.go`:

```go
type cancelingInspector struct {
    cancel context.CancelFunc
    calls  []string
}

func (i *cancelingInspector) Inspect(_ context.Context, home string) (VerifiedEnvironment, InspectionState, error) {
    i.calls = append(i.calls, home)
    i.cancel()
    return VerifiedEnvironment{}, Foreign, inspectionError(Foreign, "foreign", nil)
}

func (i *cancelingInspector) ClassifyAdd(context.Context, string) (AddState, error) {
    return AddTargetReady, nil
}
```

In `Discover`, execute `if err := ctx.Err(); err != nil { return DiscoveryResult{}, err }` before creating each comparison key and immediately before each `Inspect` call. This makes the second candidate unreachable after the first adapter call cancels the context.

- [ ] **Step 2: Run discovery tests and observe RED**

Run: `go test -mod=vendor -count=1 ./internal/environment -run 'TestDiscover'`

Expected: build failure `undefined: Discover`.

- [ ] **Step 3: Implement deterministic candidate collection and bounded warnings**

Implement `Discover` with these exact state transitions:

```go
func Discover(ctx context.Context, request DiscoveryRequest, inspector Inspector) (DiscoveryResult, error) {
    candidates := make([]Candidate, 0, len(request.RegistryHomes)+1)
    if request.Explicit {
        candidates = append(candidates, Candidate{Home: request.ExplicitHome, Source: SourceExplicit})
    } else {
        for _, home := range request.RegistryHomes { candidates = append(candidates, Candidate{Home: home, Source: SourceRegistry}) }
        if request.EnvironmentHome != "" { candidates = append(candidates, Candidate{Home: request.EnvironmentHome, Source: SourceEnvironment}) }
    }
    result := DiscoveryResult{}
    seen := map[string]struct{}{}
    rawFailures := map[string]struct{}{}
    for _, candidate := range candidates {
        if err := ctx.Err(); err != nil { return DiscoveryResult{}, err }
        key, keyErr := pathsafe.ComparisonKey(candidate.Home)
        if keyErr != nil {
            if _, duplicate := rawFailures[candidate.Home]; duplicate { continue }
            rawFailures[candidate.Home] = struct{}{}
            typed := inspectionError(classifyComparisonFailure(keyErr), "candidate path cannot be compared safely", keyErr)
            if candidate.Source == SourceExplicit || candidate.Source == SourceManual { return DiscoveryResult{}, typed }
            result.Warnings = append(result.Warnings, Warning{Source: candidate.Source, Home: candidate.Home, State: typed.(*Error).State})
            continue
        }
        if _, duplicate := seen[key]; duplicate { continue }
        seen[key] = struct{}{}
        if err := ctx.Err(); err != nil { return DiscoveryResult{}, err }
        verified, state, inspectErr := inspector.Inspect(ctx, candidate.Home)
        switch state {
        case Ready:
            if inspectErr != nil { return DiscoveryResult{}, inspectionError(InspectionFailed, "ready inspection returned an error", inspectErr) }
            result.Environments = append(result.Environments, verified)
        case RetryRequired, Foreign, InspectionFailed:
            var typed *Error
            if !errors.As(inspectErr, &typed) || typed.State != state {
                return DiscoveryResult{}, inspectionError(InspectionFailed, "inspection state and typed error disagree", inspectErr)
            }
            if state == RetryRequired {
                result.Environments = append(result.Environments, verified)
                continue
            }
            if candidate.Source == SourceExplicit || candidate.Source == SourceManual { return DiscoveryResult{}, typed }
            result.Warnings = append(result.Warnings, Warning{Source: candidate.Source, Home: candidate.Home, State: state})
        default:
            return DiscoveryResult{}, inspectionError(InspectionFailed, "inspector returned an unknown state", inspectErr)
        }
    }
    result.ManualRequired = len(result.Environments) == 0
    return result, nil
}

func classifyComparisonFailure(err error) InspectionState {
    if errors.Is(err, pathsafe.ErrUnsafe) { return Foreign }
    return InspectionFailed
}
```

`Warning.String` takes at most the first 96 input runes, appends `…` when truncated, passes the result through `strconv.QuoteToASCII`, prefixes the fixed source/state labels, and enforces `len(result) <= 1536`; it never includes `.env` contents, receipt data, `err.Error()` or a raw control rune.

- [ ] **Step 4: Run environment package to GREEN**

Run: `go test -mod=vendor -count=1 ./internal/environment`

Expected: PASS.

- [ ] **Step 5: Commit discovery**

```powershell
git add internal/environment
git commit -m "feat(environment): discover verified roots by strict precedence"
```

### Task 5: Exact Russian wizard menus and explicit-flag tracking

**Files:**
- Modify: `internal/cli/flags.go:13-78`
- Create: `internal/cli/errors.go`
- Modify: `internal/cli/prompt.go:13-157`
- Modify: `internal/cli/prompt_test.go`

**Interfaces:**
- Produces: `options.kitHomeSet`, `options.updateSet`, `options.toolchainSet bool`, populated through `flags.Visit` even for `--flag=`.
- Produces: `applyModeChoices()`, `environmentChoices([]environment.VerifiedEnvironment)`, exact `toolchainChoices()` labels and exact `updateChoices()` labels.
- Produces: `questionnaire.askApplyMode(context.Context) (string, error)` and reusable `askChoice` reprompt/EOF/cancellation behavior.
- Produces typed `operationalError` codes and an exact code-to-exit mapping; no control flow parses error strings.
- Consumes: stable values `add`, `update`, `cc_1c_skills`, `ai_rules_1c`, `none|content|database|both`.

- [ ] **Step 1: Write failing exact-output tests**

```go
func TestQuestionnaireApplyModeAndToolchainCopyIsExact(t *testing.T) {
    var output bytes.Buffer
    q := newQuestionnaire(strings.NewReader("2\n1\n"), &output)
    mode, err := q.askApplyMode(context.Background())
    if err != nil || mode != "update" { t.Fatalf("mode=%q err=%v", mode, err) }
    var toolchain string
    if err := q.askChoice(context.Background(), &toolchain, "Выберите набор skills", toolchainChoices()); err != nil { t.Fatal(err) }
    want := "Что вы хотите сделать:\n  1. Добавить новое окружение\n  2. Обновить существующее окружение\nВведите номер ответа: " +
        "Выберите набор skills:\n  1. cc_1c_skills от Широкова\n  2. ai_rules_1c от Филиппова\nВведите номер ответа: "
    if output.String() != want { t.Fatalf("output=%q want=%q", output.String(), want) }
}

func TestParseOptions_RemembersExplicitEmptySelectors(t *testing.T) {
    opts, err := parseOptions([]string{"apply", "--toolchain=", "--update=", "--kit-home="}, io.Discard)
    if err != nil || !opts.toolchainSet || !opts.updateSet || !opts.kitHomeSet { t.Fatalf("opts=%#v err=%v", opts, err) }
}
```

Add these complete typed-error tests:

```go
func TestQuestionnaireModeRepromptsAndEOFIsTyped(t *testing.T) {
    var out bytes.Buffer
    q := newQuestionnaire(strings.NewReader("\n9\n2\n"), &out)
    mode, err := q.askApplyMode(context.Background())
    if err != nil || mode != "update" { t.Fatalf("mode=%q err=%v", mode, err) }
    if got := strings.Count(out.String(), "Что вы хотите сделать:"); got != 3 { t.Fatalf("menu count=%d output=%q", got, out.String()) }
    q = newQuestionnaire(strings.NewReader(""), io.Discard)
    _, err = q.askApplyMode(context.Background())
    var operational *operationalError
    if !errors.As(err, &operational) || operational.Code != codeInputRequired { t.Fatalf("err=%T %v", err, err) }
    code, exit := errorIdentity(err)
    if code != "INPUT_REQUIRED" || exit != ExitUsage { t.Fatalf("code=%q exit=%d", code, exit) }
}

func TestOperationalErrorExitMappingIsExact(t *testing.T) {
    tests := []struct{ code operationalCode; wantExit int }{
        {codeInputRequired, ExitUsage}, {codeUpdateChoiceNotApplicable, ExitUsage},
        {codeWorkspaceExistsUseUpdate, ExitFailure}, {codeForeignWorkspace, ExitFailure},
        {codeRetryRequired, ExitFailure}, {codeWorkspaceInspectionFailed, ExitFailure},
    }
    for _, test := range tests {
        identity, exit := errorIdentity(newOperationalError(test.code, "detail", nil))
        if identity != string(test.code) || exit != test.wantExit { t.Fatalf("code=%q identity=%q exit=%d", test.code, identity, exit) }
    }
}

func TestErrorIdentity_DomainAndOperationalExitMappingIsExact(t *testing.T) {
    tests := []struct{ name string; err error; wantCode string; wantExit int }{
        {"toolchain", domain.NewValidationError(domain.ToolchainUnknown, "toolchain", "bad"), "TOOLCHAIN_UNKNOWN", ExitUsage},
        {"application", domain.NewValidationError(domain.AIAppRequired, "application", "codex"), "AI_APP_REQUIRED", ExitApplicationRequired},
        {"foreign", newOperationalError(codeForeignWorkspace, "foreign", nil), "FOREIGN_WORKSPACE", ExitFailure},
        {"inspection", newOperationalError(codeWorkspaceInspectionFailed, "io", nil), "WORKSPACE_INSPECTION_FAILED", ExitFailure},
        {"retry", newOperationalError(codeRetryRequired, "retry", nil), "RETRY_REQUIRED", ExitFailure},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            code, exit := errorIdentity(test.err)
            if code != test.wantCode || exit != test.wantExit { t.Fatalf("code=%q exit=%d", code, exit) }
        })
    }
}
```

- [ ] **Step 2: Run CLI focused tests and observe RED**

Run: `go test -mod=vendor -count=1 ./internal/cli -run 'TestQuestionnaireApplyMode|TestParseOptions_RemembersExplicit'`

Expected: compile failures for missing fields/method.

- [ ] **Step 3: Implement exact menu copy and stable values**

```go
func applyModeChoices() []choice {
    return []choice{{value: "add", label: "Добавить новое окружение"}, {value: "update", label: "Обновить существующее окружение"}}
}

func toolchainChoices() []choice {
    return []choice{
        {value: string(domain.ToolchainCC1CSkills), label: "cc_1c_skills от Широкова"},
        {value: string(domain.ToolchainAIRules1C), label: "ai_rules_1c от Филиппова"},
    }
}

func updateChoices() []choice {
    return []choice{
        {value: "none", label: "Ничего"},
        {value: "content", label: "Только файлы окружения"},
        {value: "database", label: "Только файлы базы данных"},
        {value: "both", label: "Файлы окружения и базы данных"},
    }
}

const manualEnvironmentChoice = "manual"
func environmentChoices(environments []environment.VerifiedEnvironment) []choice {
    result := make([]choice, 0, len(environments)+1)
    for index, item := range environments {
        label := fmt.Sprintf("%s — %s", item.Desired.Project(), item.Home)
        if item.Pending { label = item.Home + " — незавершённая операция" }
        result = append(result, choice{value: strconv.Itoa(index), label: label})
    }
    return append(result, choice{value: manualEnvironmentChoice, label: "Указать другой путь"})
}
```

Create `errors.go` with exact signatures:

```go
type operationalCode string
const (
    codeInputRequired operationalCode = "INPUT_REQUIRED"
    codeWorkspaceExistsUseUpdate operationalCode = "WORKSPACE_EXISTS_USE_UPDATE"
    codeForeignWorkspace operationalCode = "FOREIGN_WORKSPACE"
    codeRetryRequired operationalCode = "RETRY_REQUIRED"
    codeUpdateChoiceNotApplicable operationalCode = "UPDATE_CHOICE_NOT_APPLICABLE"
    codeWorkspaceInspectionFailed operationalCode = "WORKSPACE_INSPECTION_FAILED"
)
type operationalError struct { Code operationalCode; Detail string; Cause error }
func (e *operationalError) Error() string {
    if e.Detail == "" { return string(e.Code) }
    return string(e.Code) + ": " + e.Detail
}
func (e *operationalError) Unwrap() error { return e.Cause }
func newOperationalError(code operationalCode, detail string, cause error) error {
    return &operationalError{Code: code, Detail: detail, Cause: cause}
}
```

`errorIdentity` first preserves `context.Canceled -> (INTERRUPTED, 130)`, then uses `errors.As` for `*operationalError` and the exact table in the test, then keeps current apps/domain mappings. In the domain branch, `AI_APP_REQUIRED` maps only to `ExitApplicationRequired`, `LOCAL_CHANGES_DETECTED` maps to `ExitLocalChanges`, and every other domain validation code including `TOOLCHAIN_UNKNOWN` maps to `ExitUsage`. The operational branch maps only `INPUT_REQUIRED` and `UPDATE_CHOICE_NOT_APPLICABLE` to `ExitUsage`; `WORKSPACE_EXISTS_USE_UPDATE`, `FOREIGN_WORKSPACE`, `RETRY_REQUIRED`, and `WORKSPACE_INSPECTION_FAILED` map to `ExitFailure`. Remove substring matching for the six new codes.

- [ ] **Step 4: Implement explicit flag detection and early non-Hermes absence**

After `flags.Parse`, call `flags.Visit` and set the three booleans by exact flag name. At the start of `completeProject`, if `opts.toolchainSet && opts.toolchain == ""`, return `domain.NewValidationError(domain.ToolchainUnknown, "toolchain", "")`; a nonempty explicit valid ID makes `askChoice` skip the question, and any nonempty invalid ID is rejected by `desiredState` without fallback. In `askChoice`/`askText`, replace formatted `INPUT_REQUIRED` errors with `newOperationalError(codeInputRequired, question, io.EOF)`. In `completeApplication`, immediately after choosing `appInstalled=false` for non-Hermes, return `domain.NewValidationError(domain.AIAppRequired, "application", opts.application)` so project/toolchain/path and effects are never reached.

- [ ] **Step 5: Run prompt/flag tests to GREEN**

Run: `go test -mod=vendor -count=1 ./internal/cli -run 'TestQuestionnaire|TestParseOptions'`

Expected: PASS.

- [ ] **Step 6: Commit UI primitives**

```powershell
git add internal/cli/flags.go internal/cli/errors.go internal/cli/prompt.go internal/cli/prompt_test.go
git commit -m "feat(cli): add exact environment mode menus"
```

### Task 6: Complete interactive add/update dispatcher and environment flow

**Files:**
- Create: `internal/cli/environment_flow.go`
- Create: `internal/cli/environment_flow_test.go`
- Modify: `internal/cli/run.go:58-176`
- Modify: `internal/cli/run_test.go`

**Interfaces:**
- Produces: `Runner.Environments environment.Inspector`, defaulting to `environment.NewInspector()`.
- Produces: explicit `Runner.Registry EnvironmentRegistry`. `nil` means registry support is disabled: no load, warning, promotion or panic. `withDefaults` must not populate it, preserving all existing embedded/test `Runner{...}` values; only `cmd/teamkit.newRunner` wires `registry.NewDefault()` in Task 7.
- Produces: `EnvironmentRegistry` with `Load(context.Context) (registry.Registry, registry.LoadState, error)` and `Promote(context.Context, string) error`, plus per-invocation `registrySession { store EnvironmentRegistry; loaded bool; snapshot registry.Registry; state registry.LoadState; loadWarningShown bool; promoteWarningShown bool }`; Part B of this same task adds nil-safe discovery/promotion methods and the complete update branch before the task's GREEN gate.
- Consumes Task 5 typed operational codes for every add/update operational failure.
- Produces: `func (r Runner) runInteractiveAdd(ctx context.Context, opts *options, q *questionnaire, session *registrySession) int` with the exact input ownership shown.
- Consumes: Task 3 `Inspector.ClassifyAdd`; current `discoverHermes`, `options.desiredState`, `Service.Plan/Apply/Status`.

- [ ] **Step 1: Write failing add-dispatch tests with spies**

```go
func TestRunInteractiveApply_AddIsFirstAndRejectsExistingBeforePlan(t *testing.T) {
    service := &fakeService{}
    inspector := &fakeInspector{addState: environment.AddWorkspaceExists}
    var out, errOut bytes.Buffer
    runner := Runner{Service: service, Environments: inspector, In: strings.NewReader(interactiveAddAnswers(t)), Out: &out, Err: &errOut, HermesDiscovery: installedHermes(t)}
    code := runner.Run(context.Background(), []string{"apply"})
    if code == ExitOK || !strings.Contains(errOut.String(), "WORKSPACE_EXISTS_USE_UPDATE") { t.Fatalf("code=%d stderr=%q", code, errOut.String()) }
    if service.plans != 0 || service.applies != 0 { t.Fatalf("plans=%d applies=%d", service.plans, service.applies) }
    if !strings.HasPrefix(out.String(), "Что вы хотите сделать:") { t.Fatalf("out=%q", out.String()) }
}

func TestRunInteractiveApply_AddRejectsActionfulUpdateScopeAfterModeOnly(t *testing.T) {
    runner := Runner{Service: &fakeService{}, In: strings.NewReader("1\n"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
    code := runner.Run(context.Background(), []string{"apply", "--update", "both"})
    if code == ExitOK || !strings.Contains(runner.Err.(*bytes.Buffer).String(), "UPDATE_CHOICE_NOT_APPLICABLE") { t.Fatalf("code=%d", code) }
}
```

Add this complete table to `environment_flow_test.go`; `interactiveAddAnswers` supplies every answer after the mode selection and the explicit-flag rows intentionally stop at the earliest rejected input:

```go
func TestRunInteractiveApply_AddClassificationAndFlags(t *testing.T) {
    tests := []struct {
        name         string
        args         []string
        input        string
        addState     environment.AddState
        addErr       error
        cancel       bool
        wantExit     int
        wantIdentity string
        wantPlans    int
        wantApplies  int
        wantAddCalls int
    }{
        {"ready", []string{"apply"}, interactiveAddAnswers(t), environment.AddTargetReady, nil, false, ExitOK, "", 1, 1, 1},
        {"existing", []string{"apply"}, interactiveAddAnswers(t), environment.AddWorkspaceExists, nil, false, ExitFailure, "WORKSPACE_EXISTS_USE_UPDATE", 0, 0, 1},
        {"foreign", []string{"apply"}, interactiveAddAnswers(t), environment.AddTargetReady, &environment.Error{State: environment.Foreign, Detail: "foreign"}, false, ExitFailure, "FOREIGN_WORKSPACE", 0, 0, 1},
        {"inspection", []string{"apply"}, interactiveAddAnswers(t), environment.AddTargetReady, &environment.Error{State: environment.InspectionFailed, Detail: "io"}, false, ExitFailure, "WORKSPACE_INSPECTION_FAILED", 0, 0, 1},
        {"explicit none", []string{"apply", "--update", "none"}, interactiveAddAnswers(t), environment.AddTargetReady, nil, false, ExitOK, "", 1, 1, 1},
        {"explicit actionful", []string{"apply", "--update", "both"}, "1\n", environment.AddTargetReady, nil, false, ExitUsage, "UPDATE_CHOICE_NOT_APPLICABLE", 0, 0, 0},
        {"explicit empty toolchain", []string{"apply", "--toolchain="}, interactiveAddAnswers(t), environment.AddTargetReady, nil, false, ExitUsage, "TOOLCHAIN_UNKNOWN", 0, 0, 0},
        {"canceled", []string{"apply"}, interactiveAddAnswers(t), environment.AddTargetReady, nil, true, ExitInterrupted, "INTERRUPTED", 0, 0, 0},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            ctx := context.Background()
            if test.cancel {
                canceled, cancel := context.WithCancel(ctx)
                cancel()
                ctx = canceled
            }
            actionful := oneActionPlan()
            service := &fakeService{plan: actionful, hasPlan: true, applyResult: &actionful, status: reconcile.StatusReady}
            inspector := &fakeInspector{addState: test.addState, addErr: test.addErr}
            registrySpy := &fakeRegistry{state: registry.LoadMissing}
            var out, errOut bytes.Buffer
            runner := Runner{Service: service, Registry: registrySpy, Environments: inspector, In: strings.NewReader(test.input), Out: &out, Err: &errOut, HermesDiscovery: installedHermes(t)}
            code := runner.Run(ctx, test.args)
            if code != test.wantExit { t.Fatalf("exit=%d want=%d stderr=%q", code, test.wantExit, errOut.String()) }
            if test.wantIdentity != "" && !strings.HasPrefix(errOut.String(), test.wantIdentity+": ") { t.Fatalf("stderr=%q", errOut.String()) }
            if service.plans != test.wantPlans || service.applies != test.wantApplies || inspector.addCalls != test.wantAddCalls {
                t.Fatalf("service=%#v addCalls=%d registry=%#v", service, inspector.addCalls, registrySpy)
            }
            if test.wantIdentity != "" && (registrySpy.loads != 0 || registrySpy.promotes != 0) { t.Fatalf("failed add touched registry: %#v", registrySpy) }
        })
    }
}
```

Define the test adapters in `environment_flow_test.go`:

```go
func interactiveAddAnswers(t *testing.T) string {
    t.Helper()
    kit := filepath.Join(testutil.TempDir(t), "kit")
    return strings.Join([]string{"1", "3", "1", kit, "2", "2", "1", ""}, "\n")
}

func installedHermes(t *testing.T) func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
    t.Helper()
    home := filepath.Join(testutil.TempDir(t), "hermes")
    return func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
        return hermes.DiscoveryResult{Installed: true, Home: home, Executable: filepath.Join(home, "hermes"), Version: "0.20.2"}, nil
    }
}

type inspectResult struct { verified environment.VerifiedEnvironment; state environment.InspectionState; err error }
type fakeInspector struct { addState environment.AddState; addErr error; addCalls int; inspectCalls int; byHome map[string]inspectResult }
func (f *fakeInspector) ClassifyAdd(context.Context, string) (environment.AddState, error) { f.addCalls++; return f.addState, f.addErr }
func (f *fakeInspector) Inspect(_ context.Context, home string) (environment.VerifiedEnvironment, environment.InspectionState, error) {
    f.inspectCalls++
    result := f.byHome[home]
    return result.verified, result.state, result.err
}

type fakeRegistry struct {
    snapshot registry.Registry
    state registry.LoadState
    loadErr error
    promoteErr error
    loads int
    promotes int
    promotedHome string
}
func (f *fakeRegistry) Load(context.Context) (registry.Registry, registry.LoadState, error) {
    f.loads++
    copied := registry.Registry{SchemaVersion: f.snapshot.SchemaVersion, Homes: append([]string(nil), f.snapshot.Homes...)}
    return copied, f.state, f.loadErr
}
func (f *fakeRegistry) Promote(_ context.Context, home string) error {
    f.promotes++
    f.promotedHome = home
    return f.promoteErr
}

func TestRunExistingRunnerWithoutRegistryRemainsBackwardCompatible(t *testing.T) {
    actionful := oneActionPlan()
    service := &fakeService{plan: actionful, hasPlan: true, applyResult: &actionful, status: reconcile.StatusReady}
    var errOut bytes.Buffer
    runner := Runner{Service: service, In: strings.NewReader(""), Out: io.Discard, Err: &errOut}
    if code := runner.Run(context.Background(), linuxArgs("apply")); code != ExitOK { t.Fatalf("exit=%d stderr=%q", code, errOut.String()) }
    if strings.Contains(errOut.String(), "реестр") { t.Fatalf("nil registry emitted warning: %q", errOut.String()) }
}
```

In the same RED change, extend the existing `run_test.go` service fake completely so Task 6 compiles without depending on Task 7. Replace its struct and mutation methods with this shape while retaining its current `Plan`/`Status` recording:

```go
type fakeService struct {
    command string
    desired domain.DesiredState
    update reconcile.UpdateChoice
    secrets map[string]string
    err, applyErr error
    plan reconcile.OperationPlan
    hasPlan bool
    applyResult, updateResult *reconcile.OperationPlan
    plans, applies, updates, retries, statuses int
    status reconcile.PlanStatus
    statusPlan reconcile.OperationPlan
    statusErr error
}

func copyPlan(plan reconcile.OperationPlan) reconcile.OperationPlan {
    copied := plan
    copied.Actions = append([]reconcile.Action(nil), plan.Actions...)
    return copied
}

func (f *fakeService) Apply(_ context.Context, desired domain.DesiredState, update reconcile.UpdateChoice, inputs ApplyInputs) (reconcile.OperationPlan, error) {
    f.command, f.desired, f.update, f.secrets, f.applies = "apply", desired, update, inputs.Secrets, f.applies+1
    if f.applyErr != nil { return oneActionPlan(), f.applyErr }
    if f.applyResult != nil { return copyPlan(*f.applyResult), f.err }
    return oneActionPlan(), f.err
}
func (f *fakeService) Update(_ context.Context, _ string, update reconcile.UpdateChoice) (reconcile.OperationPlan, error) {
    f.command, f.update, f.updates = "update", update, f.updates+1
    if f.updateResult != nil { return copyPlan(*f.updateResult), f.err }
    return oneActionPlan(), f.err
}
func (f *fakeService) Retry(context.Context, string) error {
    f.command, f.retries = "retry", f.retries+1
    return f.err
}
```

- [ ] **Step 2: Run add tests and observe RED**

Run: `go test -mod=vendor -count=1 ./internal/cli -run 'TestRunInteractiveApply_Add'`

Expected: current questionnaire starts with OS, or missing `Runner.Environments` build failure.

- [ ] **Step 3: Refactor Run so only interactive apply enters the mode dispatcher**

Keep interactive/noninteractive `plan` on its current selector path and never load registry. Split `completeProject` into exact methods `completeProjectSelectors(ctx context.Context, opts *options) error` (project/role/toolchain only) and `completeLegacyPlanScope(ctx context.Context, opts *options) error` (the current nonempty-workspace scope prompt), then call the latter only from interactive `plan`.

Extract the current apply body without semantic changes into `runDesiredApply`; Task 7 adds promotion after the final ready observation:

```go
func (r Runner) runDesiredApply(ctx context.Context, opts options, desired domain.DesiredState, metadata *hermesResult, session *registrySession) int {
    update, err := parseUpdate(opts.update)
    if err != nil { return r.fail(opts, err, nil) }
    plan, err := r.Service.Plan(ctx, desired, update)
    if err != nil { return r.fail(opts, err, nil) }
    if len(plan.Actions) == 0 {
        handoff, handoffErr := handoffFor(desired)
        if handoffErr != nil { return r.fail(opts, handoffErr, nil) }
        return r.writeResult(opts, commandResult{Command: "apply", Status: reconcile.Status(plan), Plan: plan, Handoff: handoff, Hermes: metadata})
    }
    secrets := map[string]string{}
    if r.Credentials != nil {
        if planned, ok := r.Credentials.(PlanCredentialSource); ok {
            secrets, err = planned.ResolveForPlan(ctx, desired, plan.Actions, !opts.nonInteractive)
        } else {
            secrets, err = r.Credentials.Resolve(ctx, desired, !opts.nonInteractive)
        }
        if err != nil { return r.fail(opts, err, nil) }
    }
    _, err = r.Service.Apply(ctx, desired, update, ApplyInputs{Secrets: secrets, HermesInstaller: opts.installerPath, CertificateArchive: opts.certificates})
    if err != nil { return r.fail(opts, err, secretValues(secrets)) }
    status, finalPlan, err := r.Service.Status(ctx, desired.KitHome())
    if err != nil { return r.fail(opts, err, secretValues(secrets)) }
    handoff, err := handoffFor(desired)
    if err != nil { return r.fail(opts, err, secretValues(secrets)) }
    return r.writeResult(opts, commandResult{Command: "apply", Status: status, Plan: finalPlan, Handoff: handoff, Hermes: metadata})
}

func (r Runner) runInteractiveAdd(ctx context.Context, opts *options, q *questionnaire, session *registrySession) int {
    if opts.updateSet && opts.update != "" && opts.update != string(reconcile.UpdateNone) {
        return r.fail(*opts, newOperationalError(codeUpdateChoiceNotApplicable, opts.update, nil), nil)
    }
    opts.update = string(reconcile.UpdateNone)
    if err := q.completeApplication(ctx, opts); err != nil { return r.fail(*opts, err, nil) }
    if err := q.completeKitHome(ctx, opts); err != nil { return r.fail(*opts, err, nil) }
    if err := r.discoverHermes(ctx, opts); err != nil { return r.fail(*opts, err, nil) }
    if err := q.completeProjectSelectors(ctx, opts); err != nil { return r.fail(*opts, err, nil) }
    addState, err := r.Environments.ClassifyAdd(ctx, opts.kitHome)
    if err != nil { return r.fail(*opts, operationalInspectionError(err), nil) }
    if addState == environment.AddWorkspaceExists {
        return r.fail(*opts, newOperationalError(codeWorkspaceExistsUseUpdate, "выберите режим обновления", nil), nil)
    }
    desired, err := opts.desiredState()
    if err != nil { return r.fail(*opts, err, nil) }
    return r.runDesiredApply(ctx, *opts, desired, metadataFor(*opts), session)
}
```

In `Run`, for `opts.command == "apply" && !opts.nonInteractive`, call `q.askApplyMode` before every selector/discovery/service call and dispatch exactly `add -> runInteractiveAdd`, `update -> runInteractiveUpdate` (defined in Part B below before the combined GREEN gate). `plan` retains its old selector order; noninteractive apply calls `runDesiredApply` directly.

Add these compile-time CLI contracts to `environment_flow.go` in this step:

```go
type EnvironmentRegistry interface {
    Load(context.Context) (registry.Registry, registry.LoadState, error)
    Promote(context.Context, string) error
}

type registrySession struct {
    store EnvironmentRegistry
    loaded bool
    snapshot registry.Registry
    state registry.LoadState
    loadWarningShown bool
    promoteWarningShown bool
}
```

Add `Registry EnvironmentRegistry` and `Environments environment.Inspector` to `Runner`. `withDefaults` assigns only `Environments = environment.NewInspector()` when nil; it deliberately leaves `Registry` nil. `Run` creates `session := &registrySession{store: r.Registry}` once after parsing. Existing `plan`, `status`, and unsuccessful/no-op paths never call a session method.

- [ ] **Step 4: Map exact add inspection failures**

Return `newOperationalError(codeWorkspaceExistsUseUpdate, "выберите режим обновления", nil)` for `AddWorkspaceExists`. Define and use this typed adapter for both add and Part B update/manual selection:

```go
func operationalInspectionError(err error) error {
    var inspection *environment.Error
    if !errors.As(err, &inspection) {
        return newOperationalError(codeWorkspaceInspectionFailed, "environment inspection failed", err)
    }
    switch inspection.State {
    case environment.RetryRequired:
        return newOperationalError(codeRetryRequired, inspection.Detail, err)
    case environment.Foreign:
        return newOperationalError(codeForeignWorkspace, inspection.Detail, err)
    case environment.InspectionFailed:
        return newOperationalError(codeWorkspaceInspectionFailed, inspection.Detail, err)
    default:
        return newOperationalError(codeWorkspaceInspectionFailed, "environment inspection returned an invalid state", err)
    }
}
```

Reject add with actionful scope through `codeUpdateChoiceNotApplicable`. Never parse an error prefix and never fall through to credentials/service on these paths.

Continue directly to Part B without a commit or reviewer boundary. Task 6 is independently compilable only after both dispatcher branches, session methods, selection, retry rendering and the combined GREEN gate below are complete.

#### Part B: Interactive update discovery, selection, summary, retry and absolute no-op

**Files:**
- Modify: `internal/cli/environment_flow.go`
- Modify: `internal/cli/environment_flow_test.go`
- Modify: `internal/cli/run.go`
- Modify: `internal/cli/run_test.go`

**Interfaces:**
- Consumes: Part A's `EnvironmentRegistry` and invocation-scoped `registrySession`.
- Produces: nil-safe `(*registrySession).ensureLoaded(context.Context, io.Writer) (registry.Registry, bool)` and `(*registrySession).promote(context.Context, io.Writer, string)`; every command uses this one session, and only these methods call registry adapters.
- Produces: `(Runner).selectEnvironment(context.Context, *questionnaire, options, *registrySession) (environment.VerifiedEnvironment, error)`.
- Produces: `writeEnvironmentSummary(io.Writer, environment.VerifiedEnvironment) error`.
- Produces: `formatRetryCommand(goos, executable, home string) string` with PowerShell/POSIX single-quote escaping.
- Produces: `boundedDiagnostic(string) string`, ASCII-escaped, control-free and at most 640 bytes for registry promotion diagnostics.
- Produces: `Runner.GOOS string` and `Runner.Executable func() (string, error)`, defaulting to `runtime.GOOS` and `os.Executable` in `withDefaults`.
- Consumes: Task 4 `environment.Discover`; Task 5 exact menus; `Service.Update/Status`.

- [ ] **Step 1: Write discovery UI and summary tests**

```go
func TestSelectEnvironment_OneReadyAutoSelectsWithoutPathQuestion(t *testing.T) {
    home := filepath.Join(testutil.TempDir(t), "apa")
    registrySpy := &fakeRegistry{snapshot: registry.Registry{SchemaVersion: 1, Homes: []string{home}}, state: registry.LoadValid}
    inspector := &fakeInspector{byHome: map[string]inspectResult{home: {verified: verifiedEnvironment(t, home, "apa"), state: environment.Ready}}}
    var out bytes.Buffer
    runner := Runner{Registry: registrySpy, Environments: inspector, Out: &out, Err: io.Discard, GOOS: runtime.GOOS, Executable: os.Executable}
    q := newQuestionnaire(strings.NewReader(""), &out)
    session := &registrySession{store: registrySpy}
    got, err := runner.selectEnvironment(context.Background(), q, options{}, session)
    if err != nil || got.Home != home { t.Fatalf("got=%#v err=%v", got, err) }
    if strings.Contains(out.String(), "Введите KIT_ALL_TEAM_HOME") || strings.Contains(out.String(), "Выберите окружение") { t.Fatalf("out=%q", out.String()) }
}

func TestSelectEnvironment_MultipleShowsProjectsPendingMarkerAndManual(t *testing.T) {
    base := testutil.TempDir(t)
    ready := verifiedEnvironment(t, filepath.Join(base, "apa"), "apa")
    pending := verifiedEnvironment(t, filepath.Join(base, "wms"), "wms")
    pending.Pending = true
    manual := verifiedEnvironment(t, filepath.Join(base, "manual"), "wms")
    registrySpy := &fakeRegistry{snapshot: registry.Registry{SchemaVersion: 1, Homes: []string{ready.Home, pending.Home}}, state: registry.LoadValid}
    inspector := &fakeInspector{byHome: map[string]inspectResult{
        ready.Home: {verified: ready, state: environment.Ready},
        pending.Home: {verified: pending, state: environment.RetryRequired, err: &environment.Error{State: environment.RetryRequired, Detail: "pending"}},
        manual.Home: {verified: manual, state: environment.Ready},
    }}
    var out bytes.Buffer
    runner := Runner{Registry: registrySpy, Environments: inspector, Out: &out, Err: io.Discard, GOOS: runtime.GOOS, Executable: os.Executable}
    q := newQuestionnaire(strings.NewReader("3\n"+manual.Home+"\n"), &out)
    got, err := runner.selectEnvironment(context.Background(), q, options{}, &registrySession{store: registrySpy})
    if err != nil || got.Home != manual.Home { t.Fatalf("got=%#v err=%v", got, err) }
    want := "Выберите окружение:\n  1. apa — "+ready.Home+"\n  2. "+pending.Home+" — незавершённая операция\n  3. Указать другой путь\nВведите номер ответа: "
    if !strings.Contains(out.String(), want) || inspector.inspectCalls != 3 { t.Fatalf("calls=%d output=%q", inspector.inspectCalls, out.String()) }
}

func TestWriteEnvironmentSummary_ContainsOnlyPublicSelections(t *testing.T) {
    var out bytes.Buffer
    home := filepath.Join(testutil.TempDir(t), "apa")
    env := verifiedEnvironment(t, home, "apa")
    if err := writeEnvironmentSummary(&out, env); err != nil { t.Fatal(err) }
    for _, want := range []string{"Найдено окружение:", "KIT_ALL_TEAM_HOME: "+home, "Проект: apa", "AI-приложение: Hermes", "Роль: developer", "Набор skills: cc_1c_skills"} {
        if !strings.Contains(out.String(), want) { t.Fatalf("missing %q in %q", want, out.String()) }
    }
    if strings.Contains(out.String(), "TOKEN") { t.Fatalf("secret-like output=%q", out.String()) }
}
```

Use the full `fakeRegistry` already defined by Task 6; later tasks only add assertions or fields and never redeclare it. Define only the verified-state helper here:

```go
func verifiedEnvironment(t *testing.T, home, project string) environment.VerifiedEnvironment {
    t.Helper()
    desired, err := domain.NewDesiredState(domain.DesiredStateInput{
        OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
        KitHome: home, HermesHome: filepath.Join(filepath.Dir(home), "hermes"), HermesVersion: "0.20.2",
        Project: domain.ProjectID(project), Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
    })
    if err != nil { t.Fatal(err) }
    return environment.VerifiedEnvironment{Home: home, Desired: desired}
}
```

- [ ] **Step 2: Write corrupt/unavailable registry warning tests**

```go
func TestRegistrySession_LoadFailureWarnsOnceAndDisablesPromotion(t *testing.T) {
    for _, state := range []registry.LoadState{registry.LoadCorrupt, registry.LoadUnavailable} {
        t.Run(fmt.Sprint(state), func(t *testing.T) {
            store := &fakeRegistry{state: state, loadErr: errors.New("registry unavailable")}
            session := &registrySession{store: store}
            var errOut bytes.Buffer
            if _, writable := session.ensureLoaded(context.Background(), &errOut); writable { t.Fatal("registry unexpectedly writable") }
            if _, writable := session.ensureLoaded(context.Background(), &errOut); writable { t.Fatal("registry unexpectedly writable on second load") }
            session.promote(context.Background(), &errOut, filepath.Join(testutil.TempDir(t), "apa"))
            const warning = "Предупреждение: локальный реестр Team Kit повреждён, недоступен или имеет неподдерживаемый формат и будет проигнорирован."
            if strings.Count(errOut.String(), warning) != 1 || store.loads != 1 || store.promotes != 0 {
                t.Fatalf("loads=%d promotes=%d stderr=%q", store.loads, store.promotes, errOut.String())
            }
        })
    }
}
```

- [ ] **Step 3: Write RETRY_REQUIRED tests with exact commands**

```go
func TestSelectEnvironment_PendingPrintsExecutableRetryAndStopsEffects(t *testing.T) {
    home := filepath.Join(testutil.TempDir(t), "apa")
    executable := filepath.Join(testutil.TempDir(t), "teamkit")
    pending := verifiedEnvironment(t, home, "apa")
    pending.Pending = true
    inspector := &fakeInspector{byHome: map[string]inspectResult{home: {verified: pending, state: environment.RetryRequired, err: &environment.Error{State: environment.RetryRequired, Detail: "pending"}}}}
    service := &fakeService{}
    registrySpy := &fakeRegistry{}
    var out, errOut bytes.Buffer
    runner := Runner{
        Service: service, Registry: registrySpy, Environments: inspector,
        In: strings.NewReader("2\n"), Out: &out, Err: &errOut, GOOS: runtime.GOOS,
        Executable: func() (string, error) { return executable, nil },
    }
    code := runner.Run(context.Background(), []string{"apply", "--kit-home", home})
    if code != ExitFailure || !strings.Contains(errOut.String(), "RETRY_REQUIRED") || !strings.Contains(errOut.String(), executable) || !strings.Contains(errOut.String(), home) { t.Fatalf("code=%d stderr=%q", code, errOut.String()) }
    if service.plans != 0 || service.applies != 0 || service.statuses != 0 || registrySpy.promotes != 0 { t.Fatalf("service=%#v registry=%#v", service, registrySpy) }
}

func TestFormatRetryCommand_POSIXEscapesSingleQuotes(t *testing.T) {
    got := formatRetryCommand("linux", "/opt/teamkit", "/srv/team'kit")
    want := `'/opt/teamkit' retry --kit-home '/srv/team'\''kit'`
    if got != want { t.Fatalf("got=%q want=%q", got, want) }
}

func TestFormatRetryCommand_WindowsEscapesPowerShellSingleQuotes(t *testing.T) {
    got := formatRetryCommand("windows", `C:\Program Files\O'Brien\teamkit.exe`, `C:\Team's\apa`)
    want := `& 'C:\Program Files\O''Brien\teamkit.exe' retry --kit-home 'C:\Team''s\apa'`
    if got != want { t.Fatalf("got=%q want=%q", got, want) }
}
```

- [ ] **Step 4: Write the absolute no-op barrier test**

```go
func TestRunInteractiveUpdate_NoneStopsAfterSummaryWithoutEffects(t *testing.T) {
    home := filepath.Join(testutil.TempDir(t), "apa")
    ready := verifiedEnvironment(t, home, "apa")
    inspector := &fakeInspector{byHome: map[string]inspectResult{home: {verified: ready, state: environment.Ready}}}
    service := &fakeService{}
    credentials := &planCredentials{}
    registrySpy := &fakeRegistry{}
    var out, errOut bytes.Buffer
    runner := Runner{Service: service, Credentials: credentials, Registry: registrySpy, Environments: inspector, In: strings.NewReader("2\n1\n"), Out: &out, Err: &errOut, GOOS: runtime.GOOS, Executable: os.Executable}
    code := runner.Run(context.Background(), []string{"apply", "--kit-home", home})
    if code != ExitOK || !strings.Contains(out.String(), "Найдено окружение:") { t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String()) }
    if inspector.inspectCalls != 1 || service.plans != 0 || service.applies != 0 || service.statuses != 0 || credentials.calls != 0 || registrySpy.loads != 0 || registrySpy.promotes != 0 {
        t.Fatalf("inspect=%d service=%#v credentials=%d registry=%#v", inspector.inspectCalls, service, credentials.calls, registrySpy)
    }
}

func TestSelectEnvironment_InvalidManualPathIsFatalWithoutFallback(t *testing.T) {
    inspector := &fakeInspector{byHome: map[string]inspectResult{
        "relative": {state: environment.Foreign, err: &environment.Error{State: environment.Foreign, Detail: "relative"}},
    }}
    var out bytes.Buffer
    runner := Runner{Environments: inspector, Out: &out, Err: io.Discard, GOOS: runtime.GOOS, Executable: os.Executable}
    q := newQuestionnaire(strings.NewReader("relative\n"), &out)
    _, err := runner.selectEnvironment(context.Background(), q, options{}, &registrySession{})
    var operational *operationalError
    if !errors.As(err, &operational) || operational.Code != codeForeignWorkspace || inspector.inspectCalls != 1 {
        t.Fatalf("calls=%d err=%T %v", inspector.inspectCalls, err, err)
    }
}
```

- [ ] **Step 5: Run update tests and observe RED**

Run: `go test -mod=vendor -count=1 ./internal/cli -run 'TestSelectEnvironment|TestWriteEnvironmentSummary|TestRunInteractiveApply_Update'`

Expected: missing flow methods or current selector prompts appear.

- [ ] **Step 6: Implement registry-session load and candidate selection**

Implement `boundedDiagnostic` exactly as a rune-bounded encoder; because each selected rune is quoted independently, the byte cap never slices an escape sequence:

```go
func boundedDiagnostic(value string) string {
    const maxRunes = 48
    runes := []rune(value)
    truncated := len(runes) > maxRunes
    if truncated { runes = runes[:maxRunes] }
    var encoded strings.Builder
    for _, r := range runes {
        quoted := strconv.QuoteRuneToASCII(r)
        fragment := quoted[1 : len(quoted)-1]
        if encoded.Len()+len(fragment) > 620 { truncated = true; break }
        encoded.WriteString(fragment)
    }
    if truncated { encoded.WriteString(`\u2026`) }
    return `"` + encoded.String() + `"`
}
```

Implement the session methods with these exact state transitions:

```go
func (s *registrySession) ensureLoaded(ctx context.Context, errOut io.Writer) (registry.Registry, bool) {
    if s == nil || s.store == nil { return registry.Registry{SchemaVersion: registry.SchemaVersion}, false }
    if !s.loaded {
        s.loaded = true
        s.snapshot, s.state, _ = s.store.Load(ctx)
    }
    writable := s.state == registry.LoadMissing || s.state == registry.LoadValid
    if !writable && !s.loadWarningShown {
        fmt.Fprintln(errOut, "Предупреждение: локальный реестр Team Kit повреждён, недоступен или имеет неподдерживаемый формат и будет проигнорирован.")
        s.loadWarningShown = true
    }
    return s.snapshot, writable
}

func (s *registrySession) promote(ctx context.Context, errOut io.Writer, home string) {
    if s == nil || s.store == nil { return }
    _, writable := s.ensureLoaded(ctx, errOut)
    if !writable || s.promoteWarningShown { return }
    if err := s.store.Promote(ctx, home); err != nil {
        fmt.Fprintln(errOut, "Предупреждение: не удалось обновить локальный реестр Team Kit: "+boundedDiagnostic(err.Error()))
        s.promoteWarningShown = true
    }
}
```

Every `Runner.Run` creates exactly one `registrySession` and passes its pointer to discovery and every possible promotion path. Implement selection with these exact branches:

```go
func (r Runner) selectEnvironment(ctx context.Context, q *questionnaire, opts options, session *registrySession) (environment.VerifiedEnvironment, error) {
    request := environment.DiscoveryRequest{}
    if opts.kitHomeSet {
        if strings.TrimSpace(opts.kitHome) == "" { return environment.VerifiedEnvironment{}, newOperationalError(codeInputRequired, "KIT_ALL_TEAM_HOME", nil) }
        request.Explicit, request.ExplicitHome = true, opts.kitHome
    } else {
        snapshot, _ := session.ensureLoaded(ctx, r.Err)
        request.RegistryHomes = append([]string(nil), snapshot.Homes...)
        request.EnvironmentHome = os.Getenv("KIT_ALL_TEAM_HOME")
    }
    discovered, err := environment.Discover(ctx, request, r.Environments)
    if err != nil { return environment.VerifiedEnvironment{}, operationalInspectionError(err) }
    for _, warning := range discovered.Warnings { fmt.Fprintln(r.Err, warning.String()) }
    switch len(discovered.Environments) {
    case 0:
        var home string
        if err := q.askText(ctx, &home, "Введите KIT_ALL_TEAM_HOME"); err != nil { return environment.VerifiedEnvironment{}, err }
        verified, state, inspectErr := r.Environments.Inspect(ctx, home)
        return acceptManualInspection(verified, state, inspectErr)
    case 1:
        return discovered.Environments[0], nil
    default:
        choices := environmentChoices(discovered.Environments)
        var selected string
        if err := q.askChoice(ctx, &selected, "Выберите окружение", choices); err != nil { return environment.VerifiedEnvironment{}, err }
        if selected == manualEnvironmentChoice {
            var home string
            if err := q.askText(ctx, &home, "Введите KIT_ALL_TEAM_HOME"); err != nil { return environment.VerifiedEnvironment{}, err }
            verified, state, inspectErr := r.Environments.Inspect(ctx, home)
            return acceptManualInspection(verified, state, inspectErr)
        }
        index, parseErr := strconv.Atoi(selected)
        if parseErr != nil || index < 0 || index >= len(discovered.Environments) { return environment.VerifiedEnvironment{}, newOperationalError(codeInputRequired, "Выберите окружение", parseErr) }
        return discovered.Environments[index], nil
    }
}

func acceptManualInspection(verified environment.VerifiedEnvironment, state environment.InspectionState, err error) (environment.VerifiedEnvironment, error) {
    switch state {
    case environment.Ready:
        if err != nil { return environment.VerifiedEnvironment{}, newOperationalError(codeWorkspaceInspectionFailed, "ready inspection returned an error", err) }
        return verified, nil
    case environment.RetryRequired:
        var typed *environment.Error
        if !errors.As(err, &typed) || typed.State != environment.RetryRequired {
            return environment.VerifiedEnvironment{}, newOperationalError(codeWorkspaceInspectionFailed, "inspection state and typed error disagree", err)
        }
        return verified, nil
    case environment.Foreign, environment.InspectionFailed:
        return environment.VerifiedEnvironment{}, operationalInspectionError(err)
    default:
        return environment.VerifiedEnvironment{}, newOperationalError(codeWorkspaceInspectionFailed, "environment inspection returned an invalid state", err)
    }
}
```

`environmentChoices` uses stable zero-based index strings as values and appends `choice{value: manualEnvironmentChoice, label: "Указать другой путь"}`. Explicit selection never calls `ensureLoaded` and never reads `KIT_ALL_TEAM_HOME`. Registry/env comparison or inspection failures are warnings+skip; explicit/manual failures pass only through `operationalInspectionError` and are fatal without fallback.

- [ ] **Step 7: Implement pending command, public summary and scope dispatch**

Implement retry rendering independently of the tests' expected strings:

```go
func posixQuote(value string) string {
    return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
func powerShellQuote(value string) string {
    return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
func formatRetryCommand(goos, executable, home string) string {
    if goos == "windows" {
        return "& " + powerShellQuote(executable) + " retry --kit-home " + powerShellQuote(home)
    }
    return posixQuote(executable) + " retry --kit-home " + posixQuote(home)
}
```

Implement `(Runner).runInteractiveUpdate(ctx context.Context, opts *options, q *questionnaire, session *registrySession) int` as follows: call `selectEnvironment`; if selected `Pending`, obtain the actual executable from `Runner.Executable`, render `formatRetryCommand(r.GOOS, executable, selected.Home)`, and fail with `newOperationalError(codeRetryRequired, command, nil)` before summary/scope/service. For ready candidates call `writeEnvironmentSummary`; if `opts.updateSet` parse and honor that scope without a prompt, otherwise call `q.askChoice` with the exact four options. `none` returns `ExitOK` immediately. `content|database|both` calls `updatedPlan, err := Service.Update(ctx, selected.Home, scope)`, then `status, finalPlan, err := Service.Status(ctx, selected.Home)`, and emits `commandResult{Command:"update", Status:status, Plan:finalPlan}`. Keep `updatedPlan` in scope for Task 7 promotion; no preliminary `Plan` call is permitted.

- [ ] **Step 8: Run all focused CLI tests to GREEN**

Run: `go test -mod=vendor -count=1 ./internal/cli`

Expected: PASS; old interactive nonempty-workspace test is replaced with explicit update-mode coverage, while direct update/plan tests remain green.

- [ ] **Step 9: Commit the complete independently compiling dispatcher**

```powershell
git add internal/cli
git commit -m "feat(cli): add verified environment add and update flows"
```

### Task 7: Success-only registry promotion across all mutation entry points

**Files:**
- Modify: `internal/cli/run.go`
- Modify: `internal/cli/run_test.go`
- Modify: `cmd/teamkit/main.go`
- Create: `cmd/teamkit/main_test.go`

**Interfaces:**
- Consumes: Task 6 `registrySession.promote(ctx, errOut, home)`; it is the only promotion entry point.
- Consumes: the `OperationPlan` actually returned by `Service.Apply`/`Service.Update`, never the preliminary `Plan`, plus final `Status == reconcile.StatusReady` as the actionful-success proof; successful retry also requires final Ready.
- Wires: production `registry.NewDefault()` and `environment.NewInspector()` without touching disk during construction.

- [ ] **Step 1: Add table-driven promotion policy tests**

```go
func TestRunRegistryPromotionPolicy(t *testing.T) {
    actionful := oneActionPlan()
    empty := reconcile.OperationPlan{}
    tests := []struct {
        name string
        args func(*testing.T) []string
        configure func(*fakeService)
        wantPromotes int
    }{
        {"apply returned action and final ready", nativeApplyArgs, func(s *fakeService) { s.plan = actionful; s.hasPlan = true; s.applyResult = &actionful; s.status = reconcile.StatusReady }, 1},
        {"preliminary action but apply returned empty", nativeApplyArgs, func(s *fakeService) { s.plan = actionful; s.hasPlan = true; s.applyResult = &empty; s.status = reconcile.StatusReady }, 0},
        {"apply returned action but final needs apply", nativeApplyArgs, func(s *fakeService) { s.plan = actionful; s.hasPlan = true; s.applyResult = &actionful; s.status = reconcile.StatusNeedsApply }, 0},
        {"direct update returned action and final ready", func(t *testing.T) []string { return []string{"update", "--kit-home", filepath.Join(testutil.TempDir(t), "kit"), "--target", "both"} }, func(s *fakeService) { s.updateResult = &actionful; s.status = reconcile.StatusReady }, 1},
        {"direct update returned empty", func(t *testing.T) []string { return []string{"update", "--kit-home", filepath.Join(testutil.TempDir(t), "kit"), "--target", "both"} }, func(s *fakeService) { s.updateResult = &empty; s.status = reconcile.StatusReady }, 0},
        {"direct update final needs apply", func(t *testing.T) []string { return []string{"update", "--kit-home", filepath.Join(testutil.TempDir(t), "kit"), "--target", "both"} }, func(s *fakeService) { s.updateResult = &actionful; s.status = reconcile.StatusNeedsApply }, 0},
        {"retry final ready", func(t *testing.T) []string { return []string{"retry", "--kit-home", filepath.Join(testutil.TempDir(t), "kit")} }, func(s *fakeService) { s.status = reconcile.StatusReady }, 1},
        {"retry final needs apply", func(t *testing.T) []string { return []string{"retry", "--kit-home", filepath.Join(testutil.TempDir(t), "kit")} }, func(s *fakeService) { s.status = reconcile.StatusNeedsApply }, 0},
        {"status never promotes", func(t *testing.T) []string { return []string{"status", "--kit-home", filepath.Join(testutil.TempDir(t), "kit")} }, func(s *fakeService) { s.status = reconcile.StatusReady }, 0},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            service := &fakeService{}
            test.configure(service)
            store := &fakeRegistry{state: registry.LoadMissing}
            runner := Runner{Service: service, Registry: store, In: strings.NewReader(""), Out: io.Discard, Err: io.Discard}
            if code := runner.Run(context.Background(), test.args(t)); code != ExitOK { t.Fatalf("exit=%d", code) }
            if store.promotes != test.wantPromotes { t.Fatalf("promotes=%d want=%d", store.promotes, test.wantPromotes) }
        })
    }
}

func nativeApplyArgs(t *testing.T) []string {
    t.Helper()
    base := testutil.TempDir(t)
    return []string{"apply", "--non-interactive", "--os", nativeOS(), "--app", "hermes", "--app-installed=true", "--kit-home", filepath.Join(base, "kit"), "--hermes-home", filepath.Join(base, "hermes"), "--project", "wms", "--role", "developer", "--toolchain", "ai_rules_1c"}
}
func nativeOS() string {
    if runtime.GOOS == "windows" { return "windows" }
    if runtime.GOOS == "darwin" { return "macos" }
    return "linux"
}
```

Use the complete `fakeService` from Task 6 unchanged; Task 7 only configures its already-defined `applyResult`, `updateResult`, counters and final status.

Prove both plan variants bypass registry I/O:

```go
func TestRunPlanNeverLoadsRegistry(t *testing.T) {
    base := testutil.TempDir(t)
    tests := []struct { name string; args []string; input string }{
        {"noninteractive", append([]string{"plan"}, nativeApplyArgs(t)[1:]...), ""},
        {"interactive", []string{"plan"}, strings.Join([]string{"3", "1", filepath.Join(base, "kit"), filepath.Join(base, "hermes"), "2", "2", "1", ""}, "\n")},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            store := &fakeRegistry{state: registry.LoadValid}
            runner := Runner{Service: &fakeService{}, Registry: store, In: strings.NewReader(test.input), Out: io.Discard, Err: io.Discard, HermesDiscovery: installedHermes(t)}
            if code := runner.Run(context.Background(), test.args); code != ExitOK { t.Fatalf("exit=%d", code) }
            if store.loads != 0 || store.promotes != 0 { t.Fatalf("registry=%#v", store) }
        })
    }
}
```

- [ ] **Step 2: Add best-effort promotion failure test**

```go
func TestRunPromotionFailureWarnsButKeepsSuccessfulExit(t *testing.T) {
    actionful := oneActionPlan()
    registrySpy := &fakeRegistry{state: registry.LoadValid, promoteErr: errors.New(strings.Repeat("x", 2000) + "\n\x1b[31m")}
    service := &fakeService{hasPlan: true, plan: actionful, applyResult: &actionful, status: reconcile.StatusReady}
    var out, errOut bytes.Buffer
    runner := Runner{Service: service, Registry: registrySpy, In: strings.NewReader(""), Out: &out, Err: &errOut}
    if code := runner.Run(context.Background(), nativeApplyArgs(t)); code != ExitOK { t.Fatalf("code=%d stderr=%q", code, errOut.String()) }
    const prefix = "Предупреждение: не удалось обновить локальный реестр Team Kit:"
    if strings.Count(errOut.String(), prefix) != 1 || len(errOut.String()) > 768 || strings.ContainsRune(errOut.String(), '\x1b') { t.Fatalf("stderr len=%d %q", len(errOut.String()), errOut.String()) }
    if service.applies != 1 || registrySpy.loads != 1 || registrySpy.promotes != 1 { t.Fatalf("service=%#v registry=%#v", service, registrySpy) }
}
```

Add command-entry coverage for both disabled registry states. The bytes fixture proves orchestration never attempts repair; the fake would mutate it if `Promote` were incorrectly called:

```go
type guardingRegistry struct {
    fakeRegistry
    path string
}
func (g *guardingRegistry) Promote(ctx context.Context, home string) error {
    if err := os.WriteFile(g.path, []byte("REWRITTEN"), 0o600); err != nil { return err }
    return g.fakeRegistry.Promote(ctx, home)
}

func TestRunCorruptAndUnavailableRegistryNeverRewriteAcrossMutationCommands(t *testing.T) {
    actionful := oneActionPlan()
    entries := []struct {
        name string
        args func(*testing.T, string) []string
        input string
        interactive bool
    }{
        {"noninteractive apply", func(t *testing.T, _ string) []string { return nativeApplyArgs(t) }, "", false},
        {"direct update", func(_ *testing.T, home string) []string { return []string{"update", "--kit-home", home, "--target", "both"} }, "", false},
        {"retry", func(_ *testing.T, home string) []string { return []string{"retry", "--kit-home", home} }, "", false},
        {"interactive update", func(_ *testing.T, _ string) []string { return []string{"apply", "--update", "both"} }, "2\n", true},
    }
    for _, loadState := range []registry.LoadState{registry.LoadCorrupt, registry.LoadUnavailable} {
        for _, entry := range entries {
            t.Run(fmt.Sprintf("%d/%s", loadState, entry.name), func(t *testing.T) {
                home := filepath.Join(testutil.TempDir(t), "kit")
                registryPath := filepath.Join(testutil.TempDir(t), "environments.json")
                original := []byte("USER-REGISTRY-BYTES\n")
                if err := os.WriteFile(registryPath, original, 0o600); err != nil { t.Fatal(err) }
                store := &guardingRegistry{fakeRegistry: fakeRegistry{state: loadState, loadErr: errors.New("load disabled")}, path: registryPath}
                service := &fakeService{plan: actionful, hasPlan: true, applyResult: &actionful, updateResult: &actionful, status: reconcile.StatusReady}
                inspector := &fakeInspector{}
                if entry.interactive {
                    t.Setenv("KIT_ALL_TEAM_HOME", home)
                    inspector.byHome = map[string]inspectResult{home: {verified: verifiedEnvironment(t, home, "apa"), state: environment.Ready}}
                }
                var errOut bytes.Buffer
                runner := Runner{Service: service, Registry: store, Environments: inspector, In: strings.NewReader(entry.input), Out: io.Discard, Err: &errOut, GOOS: runtime.GOOS, Executable: os.Executable}
                if exit := runner.Run(context.Background(), entry.args(t, home)); exit != ExitOK { t.Fatalf("exit=%d stderr=%q", exit, errOut.String()) }
                const warning = "Предупреждение: локальный реестр Team Kit повреждён, недоступен или имеет неподдерживаемый формат и будет проигнорирован."
                after, err := os.ReadFile(registryPath)
                if err != nil { t.Fatal(err) }
                if strings.Count(errOut.String(), warning) != 1 || store.loads != 1 || store.promotes != 0 || !bytes.Equal(after, original) {
                    t.Fatalf("loads=%d promotes=%d stderr=%q bytes=%q", store.loads, store.promotes, errOut.String(), after)
                }
                if service.applies+service.updates+service.retries != 1 { t.Fatalf("product operation did not succeed: %#v", service) }
            })
        }
    }
}
```

- [ ] **Step 3: Run policy tests and observe RED**

Run: `go test -mod=vendor -count=1 ./internal/cli -run 'TestRunRegistryPromotion'`

Expected: fake registry receives zero promotions.

- [ ] **Step 4: Place promotion only after final success**

Change `runDesiredApply` to capture `appliedPlan, err := Service.Apply(...)`; after the successful final `Status`, execute only:

```go
if len(appliedPlan.Actions) > 0 && status == reconcile.StatusReady {
    session.promote(ctx, r.Err, desired.KitHome())
}
```

In both interactive and direct update, capture `updatedPlan, err := Service.Update(...)`; after successful final `Status`, execute only:

```go
if update != reconcile.UpdateNone && len(updatedPlan.Actions) > 0 && status == reconcile.StatusReady {
    session.promote(ctx, r.Err, selectedHome)
}
```

After successful `Service.Retry` and successful final `Status`, execute `if status == reconcile.StatusReady { session.promote(ctx, r.Err, opts.kitHome) }`. Never substitute preliminary/final observation actions for returned mutation actions. Never call `ensureLoaded`/`promote` for plan/status/no-op/error/cancellation. `session` is always nonnil inside `Run`, and its `store` may be nil by the Task 6 backward-compatibility contract.

- [ ] **Step 5: Wire production adapters and constructor-I/O test**

Extract `newRunner(in io.Reader, out, errOut io.Writer) cli.Runner` in `cmd/teamkit/main.go`; it wires `Registry: registry.NewDefault()` and `Environments: environment.NewInspector()`, while `main` supplies `os.Stdin/Stdout/Stderr`. Add this test:

```go
func TestNewRunner_DoesNotCreateRegistry(t *testing.T) {
    base := testutil.TempDir(t)
    localAppData := filepath.Join(base, "missing-local")
    xdgConfig := filepath.Join(base, "missing-xdg")
    userHome := filepath.Join(base, "missing-home")
    t.Setenv("LOCALAPPDATA", localAppData)
    t.Setenv("XDG_CONFIG_HOME", xdgConfig)
    t.Setenv("HOME", userHome)
    t.Setenv("USERPROFILE", userHome)
    registryPath, err := registry.DefaultPath(registry.LocationOptions{GOOS: runtime.GOOS, Getenv: os.Getenv, UserHomeDir: os.UserHomeDir})
    if err != nil { t.Fatal(err) }
    _ = newRunner(strings.NewReader(""), io.Discard, io.Discard)
    if _, err := os.Lstat(registryPath); !errors.Is(err, os.ErrNotExist) { t.Fatalf("constructor touched registry file %q: %v", registryPath, err) }
    if _, err := os.Lstat(filepath.Dir(registryPath)); !errors.Is(err, os.ErrNotExist) { t.Fatalf("constructor touched registry directory %q: %v", filepath.Dir(registryPath), err) }
}
```

- [ ] **Step 6: Run CLI/main tests to GREEN**

Run: `go test -mod=vendor -count=1 ./internal/cli ./internal/service ./cmd/teamkit`

Expected: PASS.

- [ ] **Step 7: Commit promotion wiring**

```powershell
git add internal/cli cmd/teamkit/main.go cmd/teamkit/main_test.go
git commit -m "feat(cli): promote successful environment mutations"
```

### Task 8: Single-toolchain Hermes and non-Hermes regression matrix

**Files:**
- Modify: `internal/apps/apps_test.go`
- Modify: `internal/bootstrap/effects_test.go`
- Modify: `internal/cli/run_test.go`

**Interfaces:**
- Consumes unchanged `apps.SupportedApplications()`, `apps.PrepareHandoff`, `catalog.Toolchains()`, Hermes profile effect contracts.
- Verifies stable IDs in desired state/`.env` while only display labels change.
- Produces no new production interface; any regression failure is fixed inside the existing `apps`, `bootstrap`, `hermes` or `cli` contracts named above.

- [ ] **Step 1: Add the exact 10×2 handoff matrix**

```go
func TestPrepareHandoff_AllAlternativeApplicationsAndToolchainsAreExclusive(t *testing.T) {
    const canary = "TEAMKIT_SECRET_CANARY"
    for _, application := range SupportedApplications() {
        for _, selected := range catalog.Toolchains() {
            t.Run(string(application)+"/"+string(selected.ID), func(t *testing.T) {
                got, err := PrepareHandoff(Application{ID: string(application), Installed: true}, HandoffRequest{
                    Toolchain: Toolchain{Name: string(selected.ID), Origin: selected.Origin, Version: selected.Commit},
                    V8StdEndpoint: catalog.V8StdMCP().Endpoint,
                    SecretValues: []string{canary},
                })
                if err != nil { t.Fatal(err) }
                if !strings.Contains(got.Command, selected.Origin) || !strings.Contains(got.Command, selected.Commit) || !strings.Contains(got.Command, catalog.V8StdMCP().Endpoint) { t.Fatalf("handoff=%q", got.Command) }
                for _, other := range catalog.Toolchains() {
                    if other.ID != selected.ID && (strings.Contains(got.Command, other.Origin) || strings.Contains(got.Command, other.Commit)) { t.Fatalf("unselected toolchain leaked: %q", got.Command) }
                }
                if strings.Contains(got.Command, canary) { t.Fatalf("secret leaked: %q", got.Command) }
            })
        }
    }
}
```

- [ ] **Step 2: Add missing-app early-exit matrix**

```go
func TestRunInteractiveAdd_AllMissingAlternativeApplicationsExitBeforeWorkspace(t *testing.T) {
    for index, application := range apps.SupportedApplications() {
        t.Run(string(application), func(t *testing.T) {
            parent := testutil.TempDir(t)
            root := filepath.Join(parent, "must-not-exist")
            service := &fakeService{}
            inspector := &fakeInspector{}
            credentials := &planCredentials{}
            store := &fakeRegistry{state: registry.LoadMissing}
            input := fmt.Sprintf("1\n3\n%d\n2\n", index+2)
            var out, errOut bytes.Buffer
            runner := Runner{Service: service, Credentials: credentials, Registry: store, Environments: inspector, In: strings.NewReader(input), Out: &out, Err: &errOut}
            code := runner.Run(context.Background(), []string{"apply", "--kit-home", root})
            if code != ExitApplicationRequired || !strings.HasPrefix(errOut.String(), "AI_APP_REQUIRED: ") { t.Fatalf("exit=%d stderr=%q", code, errOut.String()) }
            if strings.Contains(out.String(), "Выберите набор skills:") { t.Fatalf("toolchain prompt reached: %q", out.String()) }
            if inspector.addCalls != 0 || service.plans != 0 || service.applies != 0 || credentials.calls != 0 || store.loads != 0 || store.promotes != 0 {
                t.Fatalf("inspector=%#v service=%#v credentials=%d registry=%#v", inspector, service, credentials.calls, store)
            }
            if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) { t.Fatalf("root created: %v", err) }
        })
    }
}
```

- [ ] **Step 3: Add persisted handoff exclusivity tests**

Add the following to `effects_test.go`; `desiredAlternative` is included so the matrix has no implicit fixture:

```go
func desiredAlternative(t *testing.T, home string, application domain.AIApplication, toolchain domain.Toolchain) domain.DesiredState {
    t.Helper()
    state, err := domain.NewDesiredState(domain.DesiredStateInput{
        OS: domain.OSLinux, Application: application, AppInstalled: true, KitHome: home,
        Project: domain.ProjectAISUZ, Role: domain.RoleDeveloper, Toolchain: toolchain,
    })
    if err != nil { t.Fatal(err) }
    return state
}

func TestEffects_AlternativeHandoffPersistsExactlyOnePinnedToolchain(t *testing.T) {
    const canary = "TEAMKIT_SECRET_CANARY"
    for _, application := range apps.SupportedApplications() {
        for _, selected := range catalog.Toolchains() {
            t.Run(string(application)+"/"+string(selected.ID), func(t *testing.T) {
                home := testutil.TempDir(t)
                state := desiredAlternative(t, home, application, selected.ID)
                installerCalls, profileCalls := 0, 0
                effects := &Effects{
                    Installer: InstallerPortFunc(func(context.Context, string) error { installerCalls++; return nil }),
                    Profile: ProfilePortFuncs{CreateFunc: func(context.Context, string) error { profileCalls++; return nil }},
                }
                if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil { t.Fatal(err) }
                if err := effects.Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionConfigureApplication}); err != nil { t.Fatal(err) }
                body, err := os.ReadFile(filepath.Join(home, ".teamkit", "handoff.txt"))
                if err != nil { t.Fatal(err) }
                text := string(body)
                for _, want := range []string{selected.Origin, selected.Commit, catalog.V8StdMCP().Endpoint} {
                    if !strings.Contains(text, want) { t.Fatalf("missing %q in %q", want, text) }
                }
                for _, other := range catalog.Toolchains() {
                    if other.ID != selected.ID && (strings.Contains(text, other.Origin) || strings.Contains(text, other.Commit)) { t.Fatalf("unselected toolchain in %q", text) }
                }
                if strings.Contains(text, canary) || installerCalls != 0 || profileCalls != 0 { t.Fatalf("handoff=%q installer=%d profile=%d", text, installerCalls, profileCalls) }
            })
        }
    }
}
```

- [ ] **Step 4: Add Hermes role×profile-state×toolchain lifecycle matrix**

Add this matrix to `internal/bootstrap/effects_test.go`. It uses the real `Effects.ensureHermes` profile create/adopt/owner lifecycle; the new-profile branch does not pass a nonexistent directory directly to `MaterializeToolchain`, and the existing-profile branch creates the exact safe owner marker before reuse:

```go
func TestEffects_HermesProfilesAllRolesStatesAndToolchainsRemainExclusive(t *testing.T) {
    roles := []domain.Role{domain.RoleAnalyst, domain.RoleDeveloper, domain.RoleArchitect}
    for _, role := range roles {
        for _, existing := range []bool{false, true} {
            for _, selected := range catalog.Toolchains() {
                name := fmt.Sprintf("%s/existing=%t/%s", role, existing, selected.ID)
                t.Run(name, func(t *testing.T) {
                    kitHome := filepath.Join(testutil.TempDir(t), "kit")
                    hermesHome := filepath.Join(testutil.TempDir(t), "hermes")
                    state, err := domain.NewDesiredState(domain.DesiredStateInput{
                        OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
                        KitHome: kitHome, HermesHome: hermesHome, HermesVersion: "0.20.2",
                        Project: domain.ProjectAISUZ, Role: role, Toolchain: selected.ID,
                    })
                    if err != nil { t.Fatal(err) }
                    if err := (&Effects{}).Apply(context.Background(), state, reconcile.Action{Kind: reconcile.ActionPrepareWorkspace}); err != nil { t.Fatal(err) }
                    profileRoot := profileDirectory(state)
                    if existing {
                        if err := os.MkdirAll(profileRoot, 0o700); err != nil { t.Fatal(err) }
                        if err := workspace.WriteFileAtomic(profileOwnerPath(state), []byte(profileIdentity(state)+"\n"), 0o600); err != nil { t.Fatal(err) }
                    }
                    creates := 0
                    effects := &Effects{
                        HermesExecutable: testHermesExecutable(t),
                        Profile: ProfilePortFuncs{CreateFunc: func(_ context.Context, identity string) error {
                            creates++
                            return os.MkdirAll(filepath.Join(hermesHome, "profiles", identity), 0o700)
                        }, DoctorFunc: func(context.Context, string) error { return nil }},
                        Git: GitPortFunc{SyncPinnedFunc: func(_ context.Context, _, commit, destination string) error {
                            return writePinnedSkillFixture(destination, commit, selected.ID)
                        }},
                    }
                    action := reconcile.Action{Kind: reconcile.ActionInstallToolchain}
                    if err := effects.Apply(context.Background(), state, action); err != nil { t.Fatal(err) }
                    if err := effects.Apply(context.Background(), state, action); err != nil { t.Fatalf("second install: %v", err) }
                    wantCreates := 1
                    if existing { wantCreates = 0 }
                    if creates != wantCreates { t.Fatalf("profile creates=%d want=%d", creates, wantCreates) }
                    lockPath := filepath.Join(profileRoot, "external", string(selected.ID)+".json")
                    lockData, err := os.ReadFile(lockPath)
                    if err != nil { t.Fatal(err) }
                    var lock hermes.ToolchainLock
                    if err := json.Unmarshal(lockData, &lock); err != nil { t.Fatal(err) }
                    if lock.Toolchain != selected.ID || lock.Commit != selected.Commit { t.Fatalf("lock=%#v", lock) }
                    for _, other := range catalog.Toolchains() {
                        if other.ID == selected.ID { continue }
                        if _, err := os.Lstat(filepath.Join(profileRoot, "external", string(other.ID)+".json")); !errors.Is(err, os.ErrNotExist) { t.Fatalf("unselected lock exists: %v", err) }
                    }
                    installed, err := hermes.ToolchainInstalled(profileRoot, selected)
                    if err != nil || !installed { t.Fatalf("installed=%t err=%v", installed, err) }
                })
            }
        }
    }
}
```

- [ ] **Step 5: Run the regression matrix**

Run: `go test -mod=vendor -count=1 ./internal/apps ./internal/bootstrap ./internal/hermes ./internal/cli`

Expected: PASS; total handoff subtests include exactly 20 installed-app cases and 10 missing-app cases.

- [ ] **Step 6: Commit regression evidence**

```powershell
git add internal/apps internal/bootstrap internal/cli/run_test.go
git commit -m "test(toolchains): prove exclusive skills selection"
```

### Task 9: Black-box workflows and Russian user/security documentation

**Files:**
- Modify: `test/integration/blackbox_test.go`
- Modify: `README.md`
- Modify: `docs/INSTALL.md`
- Modify: `docs/SECURITY.md`
- Modify: `docs/TEST-MATRIX.md`
- Modify: `CHANGELOG.md`
- Modify: `test/release/docs_test.go`

**Interfaces:**
- Consumes: final CLI text/error codes/registry locations from Tasks 2–8.
- Produces: user-visible documentation and release checks that prevent drift.

- [ ] **Step 1: Add process-level add/update/no-op tests**

Keep the existing `teamkitRunConfigured` build selection, but add these exact process helpers so stdin, stdout/stderr, exit status and all home/config variables are isolated. `teamkitProcessWithEnv` is the only variant allowed to add row-specific variables such as Git URL rewrites or `KIT_ALL_TEAM_HOME`:

```go
func teamkitProcess(t *testing.T, isolatedRoot, stdin string, args ...string) (string, string, int) {
    t.Helper()
    return teamkitProcessWithEnv(t, isolatedRoot, stdin, nil, args...)
}

func teamkitProcessWithEnv(t *testing.T, isolatedRoot, stdin string, extra map[string]string, args ...string) (string, string, int) {
    t.Helper()
    repository, err := filepath.Abs("../..")
    if err != nil { t.Fatal(err) }
    var command *exec.Cmd
    if binary := strings.TrimSpace(os.Getenv("TEAMKIT_TEST_BINARY")); binary != "" {
        if !filepath.IsAbs(binary) { binary = filepath.Join(repository, binary) }
        command = exec.Command(binary, args...)
    } else {
        suffix := ""
        if runtime.GOOS == "windows" { suffix = ".exe" }
        goTool := filepath.Join(runtime.GOROOT(), "bin", "go"+suffix)
        command = exec.Command(goTool, append([]string{"run", "./cmd/teamkit"}, args...)...)
    }
    userHome := filepath.Join(isolatedRoot, "home")
    localAppData := filepath.Join(isolatedRoot, "local")
    appData := filepath.Join(isolatedRoot, "roaming")
    xdg := filepath.Join(isolatedRoot, "config")
    bin := filepath.Join(isolatedRoot, "bin")
    for _, directory := range []string{userHome, localAppData, appData, xdg, bin} {
        if err := os.MkdirAll(directory, 0o700); err != nil { t.Fatal(err) }
    }
    launcher := filepath.Join(bin, "codex")
    launcherBody := []byte("#!/bin/sh\nexit 0\n")
    if runtime.GOOS == "windows" { launcher += ".cmd"; launcherBody = []byte("@exit /b 0\r\n") }
    if err := os.WriteFile(launcher, launcherBody, 0o700); err != nil { t.Fatal(err) }
    environment := append([]string(nil), os.Environ()...)
    fixed := map[string]string{
        "USERPROFILE": userHome, "HOME": userHome, "LOCALAPPDATA": localAppData,
        "APPDATA": appData, "XDG_CONFIG_HOME": xdg, "KIT_ALL_TEAM_HOME": "",
        "PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH"),
    }
    for key, value := range extra { fixed[key] = value }
    for key, value := range fixed { environment = append(environment, key+"="+value) }
    command.Dir, command.Env, command.Stdin = repository, environment, strings.NewReader(stdin)
    var stdout, stderr bytes.Buffer
    command.Stdout, command.Stderr = &stdout, &stderr
    runErr := command.Run()
    if runErr == nil { return stdout.String(), stderr.String(), 0 }
    var exitErr *exec.ExitError
    if errors.As(runErr, &exitErr) { return stdout.String(), stderr.String(), exitErr.ExitCode() }
    t.Fatalf("run teamkit: %v", runErr)
    return "", "", -1
}

func isolatedRegistryPath(root string) string {
    switch runtime.GOOS {
    case "windows": return filepath.Join(root, "local", "TeamKit", "environments.json")
    case "darwin": return filepath.Join(root, "home", "Library", "Application Support", "TeamKit", "environments.json")
    default: return filepath.Join(root, "config", "teamkit", "environments.json")
    }
}

func writeStrictRegistry(t *testing.T, path string, homes []string) {
    t.Helper()
    store := registry.New(path)
    for index := len(homes)-1; index >= 0; index-- {
        if err := store.Promote(context.Background(), homes[index]); err != nil { t.Fatal(err) }
    }
}
```

Refactor the current ready fixture and add local Git/process snapshots with these exact bodies:

```go
type fileSnapshot struct { SHA256 [32]byte; Size int64; ModTime int64 }

func prepareReadyWorkspaceAt(t *testing.T, kit string, projectID domain.ProjectID) string {
    t.Helper()
    project, err := catalog.LookupProject(projectID)
    if err != nil { t.Fatal(err) }
    desired, err := domain.NewDesiredState(domain.DesiredStateInput{
        OS: domain.OSFamily(nativeTeamKitOS()), Application: domain.AppCodex, AppInstalled: true,
        KitHome: kit, Project: projectID, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainAIRules1C,
    })
    if err != nil { t.Fatal(err) }
    for _, directory := range []string{filepath.Join(kit, ".teamkit"), filepath.Join(kit, "db")} {
        if err := os.MkdirAll(directory, 0o700); err != nil { t.Fatal(err) }
    }
    files := map[string]string{
        filepath.Join(kit, ".teamkit", "owner"): string(projectID)+"\n",
        filepath.Join(kit, ".teamkit", "content.ready"): project.ContentBranch+"\n",
        filepath.Join(kit, ".teamkit", "database.ready"): "develop\n",
        filepath.Join(kit, ".teamkit", "handoff.txt"): mustHandoff(t, desired)+"\n",
        filepath.Join(kit, ".gitignore"): ".env\n/db/\n/.teamkit/\n",
    }
    for path, body := range files { if err := os.WriteFile(path, []byte(body), 0o600); err != nil { t.Fatal(err) } }
    if err := workspace.WritePublicEnv(filepath.Join(kit, ".env"), config.Encode(desired)); err != nil { t.Fatal(err) }
    gitRun(t, kit, "init"); gitRun(t, kit, "checkout", "-b", project.ContentBranch)
    gitRun(t, kit, "config", "user.name", "Team Kit Test"); gitRun(t, kit, "config", "user.email", "teamkit@example.invalid")
    gitRun(t, kit, "remote", "add", "origin", project.ContentRepository); gitRun(t, kit, "add", ".gitignore"); gitRun(t, kit, "commit", "-m", "fixture")
    database := filepath.Join(kit, "db")
    gitRun(t, database, "init"); gitRun(t, database, "checkout", "-b", "develop"); gitRun(t, database, "remote", "add", "origin", project.DatabaseRepository)
    if err := gitx.InstallHooks(filepath.Join(database, ".git", "hooks")); err != nil { t.Fatal(err) }
    return kit
}

func prepareReadyWorkspace(t *testing.T) string {
    t.Helper()
    return prepareReadyWorkspaceAt(t, filepath.Join(testutil.TempDir(t), "kit"), domain.ProjectWMS)
}

func snapshotRegularTree(t *testing.T, root string) map[string]fileSnapshot {
    t.Helper()
    result := map[string]fileSnapshot{}
    err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
        if walkErr != nil { return walkErr }
        info, err := entry.Info()
        if err != nil { return err }
        if info.Mode()&os.ModeSymlink != 0 { return fmt.Errorf("symlink in fixture: %s", path) }
        if !info.Mode().IsRegular() { return nil }
        data, err := os.ReadFile(path)
        if err != nil { return err }
        relative, err := filepath.Rel(root, path)
        if err != nil { return err }
        result[relative] = fileSnapshot{SHA256: sha256.Sum256(data), Size: info.Size(), ModTime: info.ModTime().UnixNano()}
        return nil
    })
    if err != nil { t.Fatal(err) }
    return result
}

func createBareRemote(t *testing.T, root, branch string) string {
    t.Helper()
    work, bare := root+"-work", root+".git"
    if err := os.MkdirAll(work, 0o700); err != nil { t.Fatal(err) }
    gitRun(t, work, "init"); gitRun(t, work, "checkout", "-b", branch)
    gitRun(t, work, "config", "user.name", "Team Kit Test"); gitRun(t, work, "config", "user.email", "teamkit@example.invalid")
    if err := os.WriteFile(filepath.Join(work, "fixture.txt"), []byte(branch+"\n"), 0o600); err != nil { t.Fatal(err) }
    gitRun(t, work, "add", "fixture.txt"); gitRun(t, work, "commit", "-m", "fixture")
    if output, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil { t.Fatalf("init bare: %v: %s", err, output) }
    gitRun(t, work, "remote", "add", "fixture", bare); gitRun(t, work, "push", "fixture", branch)
    return bare
}

func fileURL(path string) string {
    slash := filepath.ToSlash(path)
    if runtime.GOOS == "windows" && !strings.HasPrefix(slash, "/") { slash = "/"+slash }
    return (&url.URL{Scheme: "file", Path: slash}).String()
}

func localProjectRewriteEnvironment(t *testing.T, root string) map[string]string {
    t.Helper()
    content := fileURL(createBareRemote(t, filepath.Join(root, "content-remote"), "content-wms"))
    database := fileURL(createBareRemote(t, filepath.Join(root, "database-remote"), "develop"))
    return map[string]string{
        "GIT_CONFIG_COUNT": "2",
        "GIT_CONFIG_KEY_0": "url."+content+".insteadOf", "GIT_CONFIG_VALUE_0": "https://gitlab.example.invalid/1c/aisuz/ai.git",
        "GIT_CONFIG_KEY_1": "url."+database+".insteadOf", "GIT_CONFIG_VALUE_1": "https://gitlab.example.invalid/1c/fulfillment/wms",
    }
}
```

```go
func mustHandoff(t *testing.T, desired domain.DesiredState) string {
    t.Helper()
    toolchain, err := apps.PinnedToolchain(desired.Toolchain())
    if err != nil { t.Fatal(err) }
    handoff, err := apps.PrepareHandoff(apps.Application{ID: string(desired.Application()), Installed: true}, apps.HandoffRequest{Toolchain: toolchain, V8StdEndpoint: catalog.V8StdMCP().Endpoint})
    if err != nil { t.Fatal(err) }
    return handoff.Command
}
```

No test starts an HTTP server or uses a network remote.

Add these compilable public-contract tests (the helper names above are defined in the same file):

```go
func TestBlackBox_InteractiveAddAndRegistry(t *testing.T) {
    isolated := testutil.TempDir(t)
    kit := filepath.Join(isolated, "kit")
    input := strings.Join([]string{"1", "3", "4", "1", kit, "11", "2", "1", "fixture-user", "TEAMKIT_SECRET_CANARY", ""}, "\n")
    stdout, stderr, exit := teamkitProcessWithEnv(t, isolated, input, localProjectRewriteEnvironment(t, isolated), "apply")
    if exit != 0 || !strings.HasPrefix(stdout, "Что вы хотите сделать:") || !strings.Contains(stdout, "apply: ready") { t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr) }
    raw, err := os.ReadFile(isolatedRegistryPath(isolated))
    if err != nil { t.Fatal(err) }
    var document map[string]json.RawMessage
    decoder := json.NewDecoder(bytes.NewReader(raw))
    if err := decoder.Decode(&document); err != nil { t.Fatal(err) }
    var extra any
    if err := decoder.Decode(&extra); err != io.EOF { t.Fatalf("trailing JSON: %v", err) }
    keys := make([]string, 0, len(document))
    for key := range document { keys = append(keys, key) }
    sort.Strings(keys)
    var version int
    var homes []string
    if err := json.Unmarshal(document["schema_version"], &version); err != nil { t.Fatal(err) }
    if err := json.Unmarshal(document["homes"], &homes); err != nil { t.Fatal(err) }
    if !reflect.DeepEqual(keys, []string{"homes", "schema_version"}) || version != 1 || !reflect.DeepEqual(homes, []string{filepath.Clean(kit)}) { t.Fatalf("keys=%v version=%d homes=%v", keys, version, homes) }
    for _, forbidden := range []string{"PROJECT", "ROLE", "TOOLCHAIN", "TOKEN", "aisuz", "apa", "wms", "TEAMKIT_SECRET_CANARY"} {
        if bytes.Contains(raw, []byte(forbidden)) { t.Fatalf("registry contains %q: %q", forbidden, raw) }
    }
}

func TestBlackBox_InteractiveUpdateSelectionAndNoOp(t *testing.T) {
    isolated := testutil.TempDir(t)
    apa := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "apa"), domain.ProjectAPA)
    wms := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "wms"), domain.ProjectWMS)
    writeStrictRegistry(t, isolatedRegistryPath(isolated), []string{wms, apa})
    beforeTree := snapshotRegularTree(t, wms)
    beforeRegistry, err := os.ReadFile(isolatedRegistryPath(isolated))
    if err != nil { t.Fatal(err) }
    stdout, stderr, exit := teamkitProcess(t, isolated, "2\n1\n1\n", "apply")
    if exit != 0 || !strings.Contains(stdout, "1. wms — "+wms) || !strings.Contains(stdout, "2. apa — "+apa) || !strings.Contains(stdout, "3. Указать другой путь") || !strings.Contains(stdout, "KIT_ALL_TEAM_HOME: "+wms) {
        t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
    }
    afterRegistry, err := os.ReadFile(isolatedRegistryPath(isolated))
    if err != nil { t.Fatal(err) }
    if !reflect.DeepEqual(beforeTree, snapshotRegularTree(t, wms)) || !bytes.Equal(beforeRegistry, afterRegistry) { t.Fatal("none changed workspace or registry") }
}

func TestBlackBox_InteractiveUpdateAutoSelectsSingleRegistryHome(t *testing.T) {
    isolated := testutil.TempDir(t)
    kit := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "kit"), domain.ProjectWMS)
    writeStrictRegistry(t, isolatedRegistryPath(isolated), []string{kit})
    stdout, stderr, exit := teamkitProcess(t, isolated, "2\n1\n", "apply")
    if exit != 0 || strings.Contains(stdout, "Выберите окружение:") || !strings.Contains(stdout, "KIT_ALL_TEAM_HOME: "+kit) { t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr) }
}

func TestBlackBox_RetryRequiredLeavesWorkspaceAndRegistryUnchanged(t *testing.T) {
    isolated := testutil.TempDir(t)
    home := pendingWorkspaceFixture(t, filepath.Join(isolated, "pending"))
    registryPath := isolatedRegistryPath(isolated)
    writeStrictRegistry(t, registryPath, []string{home})
    before, err := os.ReadFile(registryPath)
    if err != nil { t.Fatal(err) }
    _, stderr, exit := teamkitProcess(t, isolated, "2\n", "apply")
    if exit == 0 || !strings.Contains(stderr, "RETRY_REQUIRED") || !strings.Contains(stderr, home) { t.Fatalf("exit=%d stderr=%q", exit, stderr) }
    after, err := os.ReadFile(registryPath)
    if err != nil { t.Fatal(err) }
    if !bytes.Equal(before, after) { t.Fatal("retry-required flow rewrote registry") }
    for _, path := range []string{filepath.Join(home, ".teamkit", "owner"), filepath.Join(home, ".env")} {
        if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) { t.Fatalf("unexpected metadata %s: %v", path, err) }
    }
}

func TestBlackBox_NonInteractiveApplyNeverPrintsMode(t *testing.T) {
    isolated := testutil.TempDir(t)
    kit := prepareReadyWorkspaceAt(t, filepath.Join(isolated, "kit"), domain.ProjectWMS)
    stdout, stderr, exit := teamkitProcess(t, isolated, "", "apply", "--non-interactive", "--os", nativeTeamKitOS(), "--app", "codex", "--app-installed=true", "--kit-home", kit, "--project", "wms", "--role", "developer", "--toolchain", "ai_rules_1c", "--update", "none")
    if exit != 0 || strings.Contains(stdout, "Что вы хотите сделать:") { t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr) }
}
```

Define the operation-first fixture exactly; it deliberately creates neither owner nor `.env`:

```go
func pendingWorkspaceFixture(t *testing.T, home string) string {
    t.Helper()
    desired, err := domain.NewDesiredState(domain.DesiredStateInput{
        OS: domain.OSFamily(nativeTeamKitOS()), Application: domain.AppCodex, AppInstalled: true,
        KitHome: home, Project: domain.ProjectWMS, Role: domain.RoleDeveloper, Toolchain: domain.ToolchainAIRules1C,
    })
    if err != nil { t.Fatal(err) }
    plan := reconcile.OperationPlan{ContractHash: "blackbox-contract", Actions: []reconcile.Action{{ID: "10-prepare-workspace", Kind: reconcile.ActionPrepareWorkspace, Idempotent: true}}}
    store, err := state.New(home)
    if err != nil { t.Fatal(err) }
    if err := store.SaveOperation(plan, reconcile.NewReceipt(desired, plan)); err != nil { t.Fatal(err) }
    return home
}
```

- [ ] **Step 2: Run black-box tests and observe any contract gaps**

Run: `go test -mod=vendor -count=1 ./test/integration -run 'TestBlackBox_(InteractiveAdd|InteractiveUpdate|Registry|RetryRequired)'`

Expected: PASS if Tasks 1–8 satisfy the public contract. Any failure is a regression: return it to the task that owns the mismatched output/path/promotion rule, add the smallest unit regression there, fix it, and rerun this command to PASS before editing documentation.

- [ ] **Step 3: Rewrite the user path as an explicit scenario tree**

In `README.md` and `docs/INSTALL.md`, add exact numbered sections:

1. Start `teamkit ... apply` in PowerShell/POSIX.
2. Choose `1 — Добавить новое окружение` if this project/path does not yet exist; answer OS/app/path/project/role/skills.
3. Choose `2 — Обновить существующее окружение` for an existing root; explain auto-selection, multiple list and manual path.
4. Explain summary and `1 Ничего / 2 content / 3 database / 4 both`.
5. Explain `RETRY_REQUIRED` as “copy and execute the complete generated command; update did not start”.
6. Show both skills display labels and state that internal IDs do not change.
7. Explain that non-Hermes receives a handoff instruction rather than direct installation.

Do not claim macOS/Linux/ALT runtime confirmation beyond existing evidence labels.

- [ ] **Step 4: Document registry and security invariants**

In `docs/SECURITY.md`, list all three locations, exact JSON sample, 64/65536 bounds, atomic owner-only policy, corrupt/unavailable no-rewrite behavior, and forbidden metadata. State that `.env`, owner and operation receipt are public metadata with bounded strict no-reparse checks rather than owner-only DACL migration. Document secret-free handoff and no disk scan.

- [ ] **Step 5: Update test matrix and changelog**

Add rows for four platforms, mode menu, operation-first receipt, registry states, no-op barrier, promotion failure, 10×2 handoff and Hermes single-toolchain. In `CHANGELOG.md` under the current unreleased/RC section state “локальный MRU хранит только абсолютные пути и не содержит секретов или сведений проекта”.

- [ ] **Step 6: Add release-document drift assertions**

```go
func TestUserDocsDescribeEnvironmentModesAndRegistrySafety(t *testing.T) {
    readme := readRepositoryFile(t, "README.md")
    for _, text := range []string{"Добавить новое окружение", "Обновить существующее окружение", "cc_1c_skills от Широкова", "ai_rules_1c от Филиппова", "RETRY_REQUIRED"} {
        if !strings.Contains(readme, text) { t.Fatalf("README missing %q", text) }
    }
    security := readRepositoryFile(t, "docs/SECURITY.md")
    for _, text := range []string{"schema_version", "65536", "64", "не содержит", "секрет"} {
        if !strings.Contains(security, text) { t.Fatalf("SECURITY missing %q", text) }
    }
}
```

- [ ] **Step 7: Run integration and documentation tests to GREEN**

Run: `go test -mod=vendor -count=1 ./test/integration ./test/release`

Expected: PASS.

- [ ] **Step 8: Commit docs and black-box evidence**

```powershell
git add README.md CHANGELOG.md docs test/integration/blackbox_test.go test/release/docs_test.go
git commit -m "docs: explain add and update environment workflows"
```

### Task 10: Full verification, race tests and four-platform builds

**Files:**
- No files are created or modified by this verification task.
- Record evidence in the implementation handoff; do not commit generated binaries, test caches, tokens, `.env`, `db/` or registry files.

**Interfaces:**
- Verifies all public/internal contracts from the approved spec.
- Produces no new interface.

- [ ] **Step 1: Prove formatting without mutating the worktree**

Run in PowerShell:

```powershell
$goFiles = git ls-files --cached --others --exclude-standard -- '*.go'
if ($LASTEXITCODE -ne 0) { throw 'git ls-files failed' }
$unformatted = if ($goFiles) { gofmt -l @goFiles } else { @() }
if ($LASTEXITCODE -ne 0) { throw 'gofmt failed' }
if ($unformatted) { throw "gofmt required: $($unformatted -join ', ')" }
git diff --check
if ($LASTEXITCODE -ne 0) { throw 'git diff --check failed' }
```

Expected: exit `0`, `$unformatted` is empty and `git diff --check` prints nothing. A failure returns to the task owning every path printed in `$unformatted`; pass that exact `$unformatted` array to `gofmt -w`, rerun the focused test named by the owning task, and commit that correction before restarting Task 10.

- [ ] **Step 2: Run the complete deterministic suite**

Run: `go test -mod=vendor -count=1 ./...`

Expected: every package reports PASS/`ok`; no test performs real GitLab/CustomLLM/GitHub access.

- [ ] **Step 3: Run static analysis**

Run: `go vet -mod=vendor ./...`

Expected: exit `0` with no diagnostics.

- [ ] **Step 4: Run race detection on the current native executor**

Run in PowerShell:

```powershell
$cc = (go env CC).Trim()
if ($LASTEXITCODE -ne 0) { throw 'go env CC failed' }
$ccCommand = ($cc -split '\s+')[0]
if (Get-Command $ccCommand -ErrorAction SilentlyContinue) {
  $previousCGO = [Environment]::GetEnvironmentVariable('CGO_ENABLED', 'Process')
  try {
    $env:CGO_ENABLED = '1'
    go test -mod=vendor -race -count=1 ./...
    if ($LASTEXITCODE -ne 0) { throw 'local race suite failed' }
  } finally {
    if ($null -eq $previousCGO) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $previousCGO }
  }
} else {
  Write-Host "Local race preflight skipped: C compiler '$ccCommand' is unavailable. GitHub native exact-SHA race matrix remains mandatory."
}
```

Expected: when a C compiler is available, PASS with no race report, especially for `questionnaire.readLine`, per-invocation registry session and cached `Store` state. When unavailable, record the single local skip line; this does not waive Step 6.

This command is not a cross-run: execute it on the current Windows amd64 worktree. The GitHub `ci.yml` native matrix in Step 6 repeats the identical race gate on Windows amd64, Linux amd64, macOS Intel and macOS ARM.

- [ ] **Step 5: Cross-build exact release targets**

```powershell
$env:CGO_ENABLED='0'
$targets = @(
  @{GOOS='windows'; GOARCH='amd64'; Out="$env:TEMP\teamkit-windows-amd64.exe"},
  @{GOOS='linux'; GOARCH='amd64'; Out="$env:TEMP\teamkit-linux-amd64"},
  @{GOOS='darwin'; GOARCH='amd64'; Out="$env:TEMP\teamkit-darwin-amd64"},
  @{GOOS='darwin'; GOARCH='arm64'; Out="$env:TEMP\teamkit-darwin-arm64"}
)
foreach ($target in $targets) {
  $env:GOOS=$target.GOOS
  $env:GOARCH=$target.GOARCH
  go build -mod=vendor -trimpath -o $target.Out ./cmd/teamkit
  if ($LASTEXITCODE -ne 0) { throw "cross-build failed: $($target.GOOS)/$($target.GOARCH)" }
}
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
```

Expected: four binaries exist in `$env:TEMP`; none is added to Git.

- [ ] **Step 6: Push the exact SHA and require the existing GitHub native matrix**

Run in PowerShell after Steps 1–5 pass:

```powershell
$branch = (git branch --show-current).Trim()
$sha = (git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($branch) -or $sha -notmatch '^[0-9a-f]{40}$') { throw 'cannot resolve branch/HEAD' }
git push -u origin HEAD
if ($LASTEXITCODE -ne 0) { throw 'git push failed' }
$remote = git ls-remote origin "refs/heads/$branch"
if ($LASTEXITCODE -ne 0 -or ($remote -split '\s+')[0] -ne $sha) { throw "origin branch is not exact HEAD $sha" }
$dispatchedAfter = (Get-Date).ToUniversalTime().AddSeconds(-2)
gh workflow run ci.yml --ref $branch
if ($LASTEXITCODE -ne 0) { throw 'workflow dispatch failed' }
$run = $null
for ($attempt = 0; $attempt -lt 30 -and $null -eq $run; $attempt++) {
  $runs = gh run list --workflow ci.yml --branch $branch --event workflow_dispatch --limit 20 --json databaseId,headSha,createdAt,event | ConvertFrom-Json
  if ($LASTEXITCODE -ne 0) { throw 'cannot list CI runs' }
  $run = $runs | Where-Object { $_.headSha -eq $sha -and $_.event -eq 'workflow_dispatch' -and ([datetime]$_.createdAt).ToUniversalTime() -ge $dispatchedAfter } | Sort-Object createdAt -Descending | Select-Object -First 1
  if ($null -eq $run) { Start-Sleep -Seconds 2 }
}
if ($null -eq $run -or $run.headSha -ne $sha) { throw "cannot resolve workflow run for exact SHA $sha" }
$runID = $run.databaseId
$beforeWatch = gh run view $runID --json headSha | ConvertFrom-Json
if ($LASTEXITCODE -ne 0 -or $beforeWatch.headSha -ne $sha) { throw "refusing to watch non-HEAD run $runID" }
gh run watch $runID --exit-status
if ($LASTEXITCODE -ne 0) { throw "GitHub CI failed: $runID" }
$result = gh run view $runID --json headSha,conclusion,jobs | ConvertFrom-Json
if ($LASTEXITCODE -ne 0 -or $result.headSha -ne $sha -or $result.conclusion -ne 'success') { throw "GitHub result is not successful exact SHA $sha" }
$build = @($result.jobs | Where-Object { $_.name -eq 'build-candidate' })
$alt = @($result.jobs | Where-Object { $_.name -eq 'alt-p11-userspace' })
$native = @($result.jobs | Where-Object { $_.name -like 'native (*' })
if ($build.Count -ne 1 -or $build[0].conclusion -ne 'success') { throw 'build-candidate failed/missing/duplicated' }
if ($alt.Count -ne 1 -or $alt[0].conclusion -ne 'success') { throw 'alt-p11-userspace failed/missing/duplicated' }
if ($native.Count -ne 4) { throw "native matrix row count=$($native.Count), want 4" }
foreach ($runner in @('windows-2025','ubuntu-24.04','macos-15-intel','macos-15')) {
  $pattern = '^native \(' + [regex]::Escape($runner) + '(,|\))'
  $matching = @($native | Where-Object { $_.name -match $pattern })
  if ($matching.Count -ne 1 -or $matching[0].conclusion -ne 'success') { throw "native row failed/missing/duplicated: $runner; jobs=$($native.name -join ', ')" }
}
```

Expected: workflow `headSha` equals local `$sha` before watching and after completion; conclusion is `success`; `build-candidate`, all four `native` matrix rows and `alt-p11-userspace` succeed. Each native row executes `go vet ./...`, `go test -json ./...`, `go test -race ./...` and the exact platform binary. Windows junction/protected-DACL tests therefore run on `windows-2025`; POSIX mode/symlink tests run on Ubuntu and both macOS runners. `alt-p11-userspace` remains pinned container evidence and must not be described as native ALT VM evidence. There is no GitLab step because `origin` is GitHub and this repository exposes no GitLab remote/API contract from which an exact-SHA pipeline could be resolved.

- [ ] **Step 7: Audit the final diff for forbidden material and spec coverage**

Run:

```powershell
git diff --check
if ($LASTEXITCODE -ne 0) { throw 'git diff --check failed' }
git status --short
if ($LASTEXITCODE -ne 0) { throw 'git status failed' }
$sha = (git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $sha -notmatch '^[0-9a-f]{40}$') { throw 'cannot resolve exact audit commit' }
$auditRoot = Join-Path $env:TEMP ('teamkit-security-audit-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $auditRoot -ErrorAction Stop | Out-Null
$auditPath = Join-Path $auditRoot 'report.json'
try {
  go run ./cmd/teamkit-security-audit --repository . --commit $sha --output $auditPath
  if ($LASTEXITCODE -ne 0) { throw 'teamkit-security-audit rejected the repository' }
  $audit = Get-Content -LiteralPath $auditPath -Raw -Encoding UTF8 | ConvertFrom-Json
  $findingCount = @($audit.findings | Where-Object { $null -ne $_ }).Count
  if ($audit.commit -ne $sha -or $audit.passed -ne $true -or $findingCount -ne 0) {
    throw 'security audit evidence does not prove a clean exact commit'
  }
} finally {
  if (Test-Path -LiteralPath $auditRoot) { Remove-Item -LiteralPath $auditRoot -Recurse -Force }
}
```

Expected: `git diff --check` passes; the repository security auditor exits `0` and its private temporary JSON proves `commit == git rev-parse HEAD`, `passed == true` and zero findings. The temporary evidence directory is removed in `finally`; `git status --short` contains no generated binary, `.env`, `db/`, `.teamkit/`, registry JSON, token or certificate. Existing synthetic canaries remain covered by the auditor's explicit test semantics rather than being mistaken for real credentials. In the handoff, list acceptance criteria 1–11 from the approved spec and name the exact passing test(s) from Tasks 5–9 beside every criterion.

- [ ] **Step 8: Run the required two-stage final review**

Dispatch the specification reviewer with exactly these inputs: approved spec path, `git diff $(git merge-base HEAD main)..HEAD`, and the acceptance-criterion/test map from Step 7. It returns only missing/incorrect requirements with severity and file/line evidence. After that reviewer reports no Critical/Important issue, dispatch a separate code-quality/security reviewer with the same diff plus the outputs of Steps 1–7; it checks registry confidentiality, strict JSON, reparse/symlink safety, DACL/modes, typed error exits, warning bounds and race safety.

Expected: both reviewers report no unresolved Critical/Important finding. For any finding, stop Task 10, return to its owning Task 1–9, add a failing regression, make the minimal correction, commit it with that task’s Conventional Commit scope, and restart Task 10 from Step 1. Task 10 itself never creates a commit.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-16-environment-mode-and-registry.md`. Two execution options:

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task and perform specification-compliance plus code-quality review between tasks.
2. **Inline Execution** — use `superpowers:executing-plans` in this worktree, execute tasks in batches and stop at review checkpoints.
