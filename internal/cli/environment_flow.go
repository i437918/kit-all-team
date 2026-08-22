package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/registry"
)

// EnvironmentRegistry is the paths-only registry boundary used by one CLI invocation.
type EnvironmentRegistry interface {
	Load(context.Context) (registry.Registry, registry.LoadState, error)
	Promote(context.Context, string) error
}

type registrySession struct {
	store               EnvironmentRegistry
	loaded              bool
	snapshot            registry.Registry
	state               registry.LoadState
	loadWarningShown    bool
	promoteWarningShown bool
}

func (s *registrySession) ensureLoaded(ctx context.Context, errOut io.Writer) (registry.Registry, bool) {
	if s == nil || s.store == nil {
		return registry.Registry{SchemaVersion: registry.SchemaVersion}, false
	}
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
	if s == nil || s.store == nil {
		return
	}
	_, writable := s.ensureLoaded(ctx, errOut)
	if !writable || s.promoteWarningShown {
		return
	}
	if err := s.store.Promote(ctx, home); err != nil {
		fmt.Fprintln(errOut, "Предупреждение: не удалось обновить локальный реестр Team Kit: "+boundedDiagnostic(err.Error()))
		s.promoteWarningShown = true
	}
}

func boundedDiagnostic(value string) string {
	const maxRunes = 48
	runes := []rune(value)
	truncated := len(runes) > maxRunes
	if truncated {
		runes = runes[:maxRunes]
	}
	var encoded strings.Builder
	for _, r := range runes {
		quoted := strconv.QuoteRuneToASCII(r)
		fragment := quoted[1 : len(quoted)-1]
		if encoded.Len()+len(fragment) > 620 {
			truncated = true
			break
		}
		encoded.WriteString(fragment)
	}
	if truncated {
		encoded.WriteString(`\u2026`)
	}
	return `"` + encoded.String() + `"`
}

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

func (r Runner) runInteractiveAdd(ctx context.Context, opts *options, q *questionnaire, session *registrySession) int {
	if opts.updateSet && opts.update != "" && opts.update != string(reconcile.UpdateNone) {
		return r.fail(*opts, newOperationalError(codeUpdateChoiceNotApplicable, opts.update, nil), nil)
	}
	opts.update = string(reconcile.UpdateNone)
	if err := q.completeApplication(ctx, opts); err != nil {
		return r.fail(*opts, err, nil)
	}
	if err := q.completeKitHome(ctx, opts); err != nil {
		return r.fail(*opts, err, nil)
	}
	if err := r.discoverHermes(ctx, opts); err != nil {
		return r.fail(*opts, err, nil)
	}
	if err := q.completeProjectSelectors(ctx, opts); err != nil {
		return r.fail(*opts, err, nil)
	}
	addState, err := r.Environments.ClassifyAdd(ctx, opts.kitHome)
	if err != nil {
		return r.fail(*opts, operationalInspectionError(err), nil)
	}
	if addState == environment.AddWorkspaceExists {
		return r.fail(*opts, newOperationalError(codeWorkspaceExistsUseUpdate, "выберите режим обновления", nil), nil)
	}
	desired, err := opts.desiredState()
	if err != nil {
		return r.fail(*opts, err, nil)
	}
	return r.runDesiredApply(ctx, *opts, desired, metadataFor(*opts), session)
}

func (r Runner) selectEnvironment(ctx context.Context, q *questionnaire, opts options, session *registrySession) (environment.VerifiedEnvironment, error) {
	request := environment.DiscoveryRequest{}
	if opts.kitHomeSet {
		if strings.TrimSpace(opts.kitHome) == "" {
			return environment.VerifiedEnvironment{}, newOperationalError(codeInputRequired, "KIT_ALL_TEAM_HOME", nil)
		}
		request.Explicit, request.ExplicitHome = true, opts.kitHome
	} else {
		snapshot, _ := session.ensureLoaded(ctx, r.Err)
		request.RegistryHomes = append([]string(nil), snapshot.Homes...)
		request.EnvironmentHome = os.Getenv("KIT_ALL_TEAM_HOME")
	}
	discovered, err := environment.Discover(ctx, request, r.Environments)
	if err != nil {
		return environment.VerifiedEnvironment{}, operationalInspectionError(err)
	}
	for _, warning := range discovered.Warnings {
		fmt.Fprintln(r.Err, warning.String())
	}
	switch len(discovered.Environments) {
	case 0:
		var home string
		if err := q.askText(ctx, &home, "Введите KIT_ALL_TEAM_HOME"); err != nil {
			return environment.VerifiedEnvironment{}, err
		}
		return r.inspectManualEnvironment(ctx, home)
	case 1:
		return discovered.Environments[0], nil
	default:
		choices := environmentChoices(discovered.Environments)
		var selected string
		if err := q.askChoice(ctx, &selected, "Выберите окружение", choices); err != nil {
			return environment.VerifiedEnvironment{}, err
		}
		if selected == manualEnvironmentChoice {
			var home string
			if err := q.askText(ctx, &home, "Введите KIT_ALL_TEAM_HOME"); err != nil {
				return environment.VerifiedEnvironment{}, err
			}
			return r.inspectManualEnvironment(ctx, home)
		}
		index, parseErr := strconv.Atoi(selected)
		if parseErr != nil || index < 0 || index >= len(discovered.Environments) {
			return environment.VerifiedEnvironment{}, newOperationalError(codeInputRequired, "Выберите окружение", parseErr)
		}
		return discovered.Environments[index], nil
	}
}

func (r Runner) inspectManualEnvironment(ctx context.Context, home string) (environment.VerifiedEnvironment, error) {
	if err := environment.ValidateTerminalPath(home); err != nil {
		return environment.VerifiedEnvironment{}, operationalInspectionError(&environment.Error{State: environment.Foreign, Detail: "manual path is unsafe for terminal use", Cause: err})
	}
	verified, state, inspectErr := r.Environments.Inspect(ctx, home)
	return acceptManualInspection(verified, state, inspectErr)
}

func acceptManualInspection(verified environment.VerifiedEnvironment, state environment.InspectionState, err error) (environment.VerifiedEnvironment, error) {
	switch state {
	case environment.Ready:
		if err != nil {
			return environment.VerifiedEnvironment{}, newOperationalError(codeWorkspaceInspectionFailed, "ready inspection returned an error", err)
		}
		if pathErr := environment.ValidateTerminalPath(verified.Home); pathErr != nil {
			return environment.VerifiedEnvironment{}, operationalInspectionError(&environment.Error{State: environment.Foreign, Detail: "verified path is unsafe for terminal use", Cause: pathErr})
		}
		return verified, nil
	case environment.RetryRequired:
		var typed *environment.Error
		if !errors.As(err, &typed) || typed.State != environment.RetryRequired {
			return environment.VerifiedEnvironment{}, newOperationalError(codeWorkspaceInspectionFailed, "inspection state and typed error disagree", err)
		}
		if pathErr := environment.ValidateTerminalPath(verified.Home); pathErr != nil {
			return environment.VerifiedEnvironment{}, operationalInspectionError(&environment.Error{State: environment.Foreign, Detail: "verified path is unsafe for terminal use", Cause: pathErr})
		}
		return verified, nil
	case environment.Foreign, environment.InspectionFailed:
		return environment.VerifiedEnvironment{}, operationalInspectionError(err)
	default:
		return environment.VerifiedEnvironment{}, newOperationalError(codeWorkspaceInspectionFailed, "environment inspection returned an invalid state", err)
	}
}

func writeEnvironmentSummary(out io.Writer, verified environment.VerifiedEnvironment) error {
	application, err := catalog.LookupAIApplication(verified.Desired.Application())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out,
		"Найдено окружение:\n  KIT_ALL_TEAM_HOME: %s\n  Проект: %s\n  AI-приложение: %s\n  Роль: %s\n  Набор skills: %s\n",
		environment.DisplayPath(verified.Home),
		verified.Desired.Project(),
		application.Label,
		verified.Desired.Role(),
		verified.Desired.Toolchain(),
	)
	return err
}

func posixQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func formatRetryCommand(goos, executable, home string) (string, error) {
	if err := environment.ValidateTerminalPath(executable); err != nil {
		return "", &environment.Error{State: environment.InspectionFailed, Detail: "Team Kit executable path is unsafe for terminal use", Cause: err}
	}
	if err := environment.ValidateTerminalPath(home); err != nil {
		return "", &environment.Error{State: environment.Foreign, Detail: "environment path is unsafe for terminal use", Cause: err}
	}
	if goos == "windows" {
		return "& " + powerShellQuote(executable) + " retry --kit-home " + powerShellQuote(home), nil
	}
	return posixQuote(executable) + " retry --kit-home " + posixQuote(home), nil
}

func (r Runner) runInteractiveUpdate(ctx context.Context, opts *options, q *questionnaire, session *registrySession) int {
	selected, err := r.selectEnvironment(ctx, q, *opts, session)
	if err != nil {
		return r.fail(*opts, err, nil)
	}
	if selected.Pending {
		executable, executableErr := r.Executable()
		if executableErr != nil {
			return r.fail(*opts, newOperationalError(codeWorkspaceInspectionFailed, "не удалось определить путь к Team Kit", executableErr), nil)
		}
		command, commandErr := formatRetryCommand(r.GOOS, executable, selected.Home)
		if commandErr != nil {
			return r.fail(*opts, operationalInspectionError(commandErr), nil)
		}
		return r.fail(*opts, newOperationalError(codeRetryRequired, command, nil), nil)
	}
	if err := writeEnvironmentSummary(r.Out, selected); err != nil {
		return r.fail(*opts, err, nil)
	}
	if !opts.updateSet {
		if err := q.askChoice(ctx, &opts.update, "Что обновить в существующем окружении", updateChoices()); err != nil {
			return r.fail(*opts, err, nil)
		}
	}
	scope, err := parseUpdate(opts.update)
	if err != nil {
		return r.fail(*opts, err, nil)
	}
	if scope == reconcile.UpdateNone {
		return ExitOK
	}
	updatedPlan, err := r.Service.UpdateVerified(ctx, selected, scope)
	if err != nil {
		return r.fail(*opts, err, nil)
	}
	status, finalPlan, err := r.Service.Status(ctx, selected.Home)
	if err != nil {
		return r.fail(*opts, err, nil)
	}
	if scope != reconcile.UpdateNone && len(updatedPlan.Actions) > 0 && status == reconcile.StatusReady {
		session.promote(ctx, r.Err, selected.Home)
	}
	return r.writeResult(*opts, commandResult{Command: "update", Status: status, Plan: finalPlan})
}
