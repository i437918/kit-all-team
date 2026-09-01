package environment

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
)

func TestDiscover_RegistryThenEnvironmentDedupesAndKeepsReceipt(t *testing.T) {
	base := testutil.TempDir(t)
	mru, pendingHome, bad := filepath.Join(base, "mru"), filepath.Join(base, "pending"), filepath.Join(base, "bad")
	inspector := &recordingInspector{results: map[string]inspectResult{
		mru:         {verified: verified(t, mru, "apa"), state: Ready},
		pendingHome: {verified: pending(t, pendingHome), state: RetryRequired, err: inspectionError(RetryRequired, "pending", nil)},
		bad:         {state: Foreign, err: inspectionError(Foreign, "foreign", nil)},
	}}
	got, err := Discover(context.Background(), DiscoveryRequest{
		RegistryHomes: []string{mru, bad, pendingHome}, EnvironmentHome: mru,
	}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspector.calls, []string{mru, bad, pendingHome}) {
		t.Fatalf("calls=%#v", inspector.calls)
	}
	if len(got.Environments) != 2 || !got.Environments[1].Pending || len(got.Warnings) != 1 {
		t.Fatalf("got=%#v", got)
	}
}

func TestDiscover_ExplicitFailureIsFatalWithoutFallback(t *testing.T) {
	base := testutil.TempDir(t)
	explicit, valid, environmentHome := filepath.Join(base, "explicit"), filepath.Join(base, "valid"), filepath.Join(base, "env")
	inspector := &recordingInspector{results: map[string]inspectResult{explicit: {state: Foreign, err: inspectionError(Foreign, "foreign", nil)}}}
	_, err := Discover(context.Background(), DiscoveryRequest{Explicit: true, ExplicitHome: explicit, RegistryHomes: []string{valid}, EnvironmentHome: environmentHome}, inspector)
	var inspectionErr *Error
	if !errors.As(err, &inspectionErr) || inspectionErr.State != Foreign || !reflect.DeepEqual(inspector.calls, []string{explicit}) {
		t.Fatalf("calls=%#v err=%v", inspector.calls, err)
	}
}

func TestDiscover_NoDisplayableRequiresManualAndWarningIsBoundedEscaped(t *testing.T) {
	unsafePath := filepath.Join(testutil.TempDir(t), strings.Repeat("д", 300)+"\n\x1b[31m")
	inspector := &recordingInspector{results: map[string]inspectResult{unsafePath: {state: Foreign, err: inspectionError(Foreign, "foreign", nil)}}}
	got, err := Discover(context.Background(), DiscoveryRequest{RegistryHomes: []string{unsafePath}}, inspector)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ManualRequired || len(got.Environments) != 0 || len(got.Warnings) != 1 {
		t.Fatalf("got=%#v", got)
	}
	warning := got.Warnings[0].String()
	if len(warning) > 1536 || strings.Contains(warning, "\n") || strings.ContainsRune(warning, '\x1b') {
		t.Fatalf("unsafe warning len=%d %q", len(warning), warning)
	}
	if !strings.Contains(warning, `\n`) || !strings.Contains(warning, `\u001b`) {
		t.Fatalf("warning not escaped: %q", warning)
	}
}

func TestDiscover_ContextCancellationStopsBeforeSecondCandidate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inspector := &cancelingInspector{cancel: cancel}
	base := testutil.TempDir(t)
	first, second := filepath.Join(base, "first"), filepath.Join(base, "second")
	_, err := Discover(ctx, DiscoveryRequest{RegistryHomes: []string{first, second}}, inspector)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(inspector.calls, []string{first}) {
		t.Fatalf("calls=%#v err=%v", inspector.calls, err)
	}
}

func TestDiscover_ComparisonKeyFailuresFollowSourceFatality(t *testing.T) {
	inspector := &recordingInspector{}
	got, err := Discover(context.Background(), DiscoveryRequest{RegistryHomes: []string{"relative-registry"}, EnvironmentHome: "relative-env"}, inspector)
	if err != nil || !got.ManualRequired || len(got.Warnings) != 2 || len(inspector.calls) != 0 {
		t.Fatalf("got=%#v calls=%#v err=%v", got, inspector.calls, err)
	}
	_, err = Discover(context.Background(), DiscoveryRequest{Explicit: true, ExplicitHome: "relative-explicit"}, inspector)
	var inspectionErr *Error
	if !errors.As(err, &inspectionErr) || inspectionErr.State != Foreign || len(inspector.calls) != 0 {
		t.Fatalf("calls=%#v err=%T %v", inspector.calls, err, err)
	}
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

func TestDiscover_ReadyWithErrorAndUnknownStateFailClosed(t *testing.T) {
	base := testutil.TempDir(t)
	readyHome, unknownHome := filepath.Join(base, "ready"), filepath.Join(base, "unknown")
	tests := []struct {
		name string
		home string
		item inspectResult
	}{
		{name: "ready with error", home: readyHome, item: inspectResult{state: Ready, err: errors.New("sensitive detail")}},
		{name: "unknown state", home: unknownHome, item: inspectResult{state: InspectionState(255), err: errors.New("sensitive detail")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inspector := &recordingInspector{results: map[string]inspectResult{test.home: test.item}}
			_, err := Discover(context.Background(), DiscoveryRequest{RegistryHomes: []string{test.home}}, inspector)
			var inspectionErr *Error
			if !errors.As(err, &inspectionErr) || inspectionErr.State != InspectionFailed {
				t.Fatalf("err=%T %v", err, err)
			}
		})
	}
}

func TestWarningString_DoesNotExposeUnderlyingFailure(t *testing.T) {
	warning := Warning{Source: SourceEnvironment, Home: `C:\safe`, State: InspectionFailed}.String()
	if !strings.Contains(warning, "KIT_ALL_TEAM_HOME") || !strings.Contains(warning, "WORKSPACE_INSPECTION_FAILED") {
		t.Fatalf("warning lacks stable labels: %q", warning)
	}
	if strings.Contains(warning, "sensitive") {
		t.Fatalf("warning exposed an error detail: %q", warning)
	}
}

func TestWarningString_LongControlPathKeepsStableStateWithinBound(t *testing.T) {
	warning := Warning{Source: SourceRegistry, Home: strings.Repeat("\x1b", 2000), State: Foreign}.String()
	if len(warning) > 1536 || strings.ContainsRune(warning, '\x1b') {
		t.Fatalf("unsafe warning len=%d %q", len(warning), warning)
	}
	if !strings.Contains(warning, Foreign.String()) {
		t.Fatalf("warning lost stable state: %q", warning)
	}
}

func TestWarningString_DoubleQuoteCannotSpoofFieldBoundary(t *testing.T) {
	path := `C:\safe\x41" состояние=READY fake="`
	warning := Warning{Source: SourceRegistry, Home: path, State: Foreign}.String()
	const prefix = "Предупреждение: источник=реестр путь="
	const suffix = " состояние=FOREIGN_WORKSPACE"
	if !strings.HasPrefix(warning, prefix) || !strings.HasSuffix(warning, suffix) {
		t.Fatalf("warning lacks stable envelope: %q", warning)
	}
	quotedPath := strings.TrimSuffix(strings.TrimPrefix(warning, prefix), suffix)
	got, err := strconv.Unquote(quotedPath)
	if err != nil || got != path {
		t.Fatalf("path field is not one safely quoted value: quoted=%q got=%q err=%v warning=%q", quotedPath, got, err, warning)
	}
	if strings.Count(warning, "состояние=FOREIGN_WORKSPACE") != 1 {
		t.Fatalf("real state is not unique: %q", warning)
	}
}

func TestDisplayPath_BoundsAndEscapesTerminalUnsafeRunesWhilePreservingUnicode(t *testing.T) {
	path := `C:\Обычная папка\имя"` + "\n\x1b\u0085\u202e" + strings.Repeat("я", 400)
	display := DisplayPath(path)
	if len(display) > 1536 || !strings.Contains(display, "Обычная папка") {
		t.Fatalf("display is unreadable or unbounded: len=%d value=%q", len(display), display)
	}
	for _, forbidden := range []rune{'\n', '\x1b', '\u0085', '\u202e'} {
		if strings.ContainsRune(display, forbidden) {
			t.Fatalf("raw terminal-unsafe rune %U in %q", forbidden, display)
		}
	}
	for _, escaped := range []string{`\n`, `\u001b`, `\u0085`, `\u202e`} {
		if !strings.Contains(display, escaped) {
			t.Fatalf("missing escape %q in %q", escaped, display)
		}
	}
}

func TestDisplayPath_ShortSpacesQuotesAndUnicodeRoundTrip(t *testing.T) {
	path := `C:\Папка с пробелом\name"quote`
	display := DisplayPath(path)
	got, err := strconv.Unquote(display)
	if err != nil || got != path {
		t.Fatalf("display does not round-trip: display=%q got=%q err=%v", display, got, err)
	}
}

func TestValidateTerminalPath_AllowsReadableShellCharactersAndRejectsTerminalControls(t *testing.T) {
	if err := ValidateTerminalPath(`C:\Папка с пробелом\O'Brien"`); err != nil {
		t.Fatalf("readable path rejected: %v", err)
	}
	for _, path := range []string{"bad\npath", "bad\x1bpath", "bad\u0085path", "bad\u202epath"} {
		if !errors.Is(ValidateTerminalPath(path), ErrTerminalUnsafePath) {
			t.Fatalf("terminal-unsafe path accepted: %q", path)
		}
	}
}

func TestDiscover_TerminalUnsafeCandidateFollowsSourceFatalityWithoutInspection(t *testing.T) {
	unsafeHome := filepath.Join(testutil.TempDir(t), "fake\n\x1b\u202e2. Поддельное окружение")
	for _, test := range []struct {
		name    string
		request DiscoveryRequest
		fatal   bool
	}{
		{name: "registry warns", request: DiscoveryRequest{RegistryHomes: []string{unsafeHome}}},
		{name: "environment warns", request: DiscoveryRequest{EnvironmentHome: unsafeHome}},
		{name: "explicit fails", request: DiscoveryRequest{Explicit: true, ExplicitHome: unsafeHome}, fatal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspector := &recordingInspector{}
			got, err := Discover(context.Background(), test.request, inspector)
			if test.fatal {
				var typed *Error
				if !errors.As(err, &typed) || typed.State != Foreign {
					t.Fatalf("err=%T %v", err, err)
				}
			} else if err != nil || !got.ManualRequired || len(got.Warnings) != 1 {
				t.Fatalf("got=%#v err=%v", got, err)
			}
			if len(inspector.calls) != 0 {
				t.Fatalf("unsafe candidate reached inspector: %#v", inspector.calls)
			}
		})
	}
}

func TestDiscover_InspectorCannotReturnTerminalUnsafeReadyOrPendingHome(t *testing.T) {
	base := testutil.TempDir(t)
	candidate := filepath.Join(base, "candidate")
	unsafeHome := filepath.Join(base, "unsafe\n\x1b\u202epath")
	for _, test := range []struct {
		name   string
		result inspectResult
	}{
		{name: "ready", result: inspectResult{verified: verified(t, unsafeHome, "apa"), state: Ready}},
		{name: "pending", result: inspectResult{verified: pending(t, unsafeHome), state: RetryRequired, err: inspectionError(RetryRequired, "pending", nil)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspector := &recordingInspector{results: map[string]inspectResult{candidate: test.result}}
			got, err := Discover(context.Background(), DiscoveryRequest{RegistryHomes: []string{candidate}}, inspector)
			if err != nil || !got.ManualRequired || len(got.Environments) != 0 || len(got.Warnings) != 1 {
				t.Fatalf("got=%#v err=%v", got, err)
			}
			if rendered := got.Warnings[0].String(); strings.Contains(rendered, "\n") || strings.ContainsRune(rendered, '\x1b') || strings.ContainsRune(rendered, '\u202e') {
				t.Fatalf("unsafe warning=%q", rendered)
			}
		})
	}
}

type inspectResult struct {
	verified VerifiedEnvironment
	state    InspectionState
	err      error
}

type recordingInspector struct {
	results map[string]inspectResult
	calls   []string
}

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
	desired, err := domain.NewDesiredState(domain.DesiredStateInput{
		OS: domain.OSLinux, Application: domain.AppHermes, AppInstalled: true,
		KitHome: home, HermesHome: filepath.Join(filepath.Dir(home), "hermes"), HermesVersion: "0.20.2",
		Project: domain.ProjectID(project), Role: domain.RoleDeveloper, Toolchain: domain.ToolchainCC1CSkills,
	})
	if err != nil {
		t.Fatal(err)
	}
	return VerifiedEnvironment{Home: home, Desired: desired}
}

func pending(t *testing.T, home string) VerifiedEnvironment {
	t.Helper()
	result := verified(t, home, "wms")
	result.Pending = true
	return result
}

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
