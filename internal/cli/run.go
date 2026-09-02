// Package cli implements Team Kit's stable command-line contract.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/apps"
	"github.com/mi1man-cmd/kit-all-team/internal/buildinfo"
	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/credentials"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

const (
	ExitOK                  = 0
	ExitFailure             = 1
	ExitUsage               = 2
	ExitApplicationRequired = 3
	ExitLocalChanges        = 4
	ExitInterrupted         = 130
)

const maxCredentialPayloadBytes = 64 << 10

// Service is the application boundary used by all CLI commands.
type Service interface {
	Plan(context.Context, domain.DesiredState, reconcile.UpdateChoice) (reconcile.OperationPlan, error)
	Apply(context.Context, domain.DesiredState, reconcile.UpdateChoice, ApplyInputs) (reconcile.OperationPlan, error)
	Status(context.Context, string) (reconcile.PlanStatus, reconcile.OperationPlan, error)
	Retry(context.Context, string) error
	Update(context.Context, string, reconcile.UpdateChoice) (reconcile.OperationPlan, error)
	UpdateVerified(context.Context, environment.VerifiedEnvironment, reconcile.UpdateChoice) (reconcile.OperationPlan, error)
}

// ApplyInputs are non-selector inputs needed only while applying effects.
type ApplyInputs struct {
	Secrets            map[string]string
	HermesInstaller    string
	CertificateArchive string
}

// CredentialSource supplies secrets without accepting them as command flags.
type CredentialSource interface {
	Resolve(context.Context, domain.DesiredState, bool) (map[string]string, error)
}

// PlanCredentialSource resolves only credentials required by a planned action set.
// Runner retains CredentialSource support for existing embedders and test fakes.
type PlanCredentialSource interface {
	ResolveForPlan(context.Context, domain.DesiredState, []reconcile.Action, bool) (map[string]string, error)
}

// ProvidedPlanCredentialSource persists a GUI-provided secret payload through
// the existing private credential store after validating the planned keys.
type ProvidedPlanCredentialSource interface {
	ResolveProvidedForPlan(context.Context, domain.DesiredState, []reconcile.Action, map[string]string) (map[string]string, error)
}

// Runner owns I/O and dispatch for one command invocation.
type Runner struct {
	Service             Service
	Credentials         CredentialSource
	ServiceFactory      func() Service
	CredentialFactory   func() CredentialSource
	In                  io.Reader
	Out                 io.Writer
	Err                 io.Writer
	HermesDiscovery     func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error)
	ApplicationLookPath platform.LookPath
	Environments        environment.Inspector
	Registry            EnvironmentRegistry
	GOOS                string
	Executable          func() (string, error)
	ConfigureHermesHome func(string) error
}

type commandResult struct {
	Command             string                  `json:"command"`
	Status              reconcile.PlanStatus    `json:"status,omitempty"`
	Plan                reconcile.OperationPlan `json:"plan,omitempty"`
	Handoff             string                  `json:"handoff,omitempty"`
	RequiredCredentials []string                `json:"required_credentials,omitempty"`
	Info                any                     `json:"info,omitempty"`
	Hermes              *hermesResult           `json:"hermes,omitempty"`
	CheckpointPath      string                  `json:"checkpoint_path,omitempty"`
	Mode                string                  `json:"mode,omitempty"`
}

type wizardEvent struct {
	SchemaVersion       int                     `json:"schema_version"`
	Event               string                  `json:"event"`
	Phase               string                  `json:"phase,omitempty"`
	Command             string                  `json:"command,omitempty"`
	Status              reconcile.PlanStatus    `json:"status,omitempty"`
	Plan                reconcile.OperationPlan `json:"plan,omitempty"`
	Handoff             string                  `json:"handoff,omitempty"`
	RequiredCredentials []string                `json:"required_credentials,omitempty"`
	Hermes              *hermesResult           `json:"hermes,omitempty"`
	CheckpointPath      string                  `json:"checkpoint_path,omitempty"`
	Mode                string                  `json:"mode,omitempty"`
	Code                string                  `json:"code,omitempty"`
	Message             string                  `json:"message,omitempty"`
}

type hermesResult struct {
	Installed bool   `json:"installed"`
	Home      string `json:"home"`
	Version   string `json:"version,omitempty"`
}

// Run parses, validates, dispatches, and returns a stable process exit code.
func (r Runner) Run(ctx context.Context, args []string) int {
	r.withDefaults()
	var parseErrors strings.Builder
	opts, err := parseOptions(args, &parseErrors)
	if err != nil {
		message := parseErrors.String()
		if message == "" {
			message = err.Error() + "\n"
		}
		fmt.Fprint(r.Err, message)
		return ExitUsage
	}
	if isPublicQuery(opts.command) {
		return r.runPublicQuery(ctx, opts)
	}
	if opts.command == "wizard-state" {
		return r.runWizardState(ctx, opts)
	}
	r.withOperationalDefaults()
	r.ensureService()
	session := &registrySession{store: r.Registry}
	if opts.command == "help" {
		fmt.Fprintln(r.Out, "teamkit plan|apply|status|retry|update|wizard-state|user-check|version")
		return ExitOK
	}
	if opts.command == "version" {
		return r.writeResult(opts, commandResult{Command: "version", Info: buildinfo.Current()})
	}
	if opts.command == "user-check" {
		return r.runUserCheck(ctx)
	}
	if opts.nonInteractive && opts.isHermesContinuationShape() {
		return r.fail(opts, newOperationalError(codeInputRequired, "Hermes continuation requires interactive credential choices", nil), nil)
	}
	if (opts.command == "plan" || opts.command == "apply") && opts.nonInteractive && isKnownNonHermesApplication(opts.application) {
		if _, err := opts.applicationInstalled(); err != nil {
			return r.fail(opts, err, nil)
		}
	}
	if opts.command == "apply" && !opts.nonInteractive {
		q := newQuestionnaire(r.In, r.Out)
		if opts.isHermesContinuation() {
			return r.runHermesContinuation(ctx, &opts, q, session)
		}
		mode, modeErr := q.askApplyMode(ctx)
		if modeErr != nil {
			return r.fail(opts, modeErr, nil)
		}
		if r.Service == nil {
			return r.fail(opts, errors.New("SERVICE_REQUIRED"), nil)
		}
		switch mode {
		case "add":
			return r.runInteractiveAdd(ctx, &opts, q, session)
		case "update":
			return r.runInteractiveUpdate(ctx, &opts, q, session)
		default:
			return r.fail(opts, newOperationalError(codeInputRequired, "Что вы хотите сделать", nil), nil)
		}
	}
	if r.Service == nil {
		return r.fail(opts, errors.New("SERVICE_REQUIRED"), nil)
	}

	if opts.command == "plan" || opts.command == "apply" {
		if !opts.nonInteractive {
			q := newQuestionnaire(r.In, r.Out)
			if err := q.completeApplication(ctx, &opts); err != nil {
				return r.fail(opts, err, nil)
			}
			if opts.application == string(domain.AppHermes) && opts.operatingSystem == "windows" {
				if err := r.prepareHermesHome(ctx, &opts, q); err != nil {
					return r.fail(opts, err, nil)
				}
			} else if err := q.completeKitHome(ctx, &opts); err != nil {
				return r.fail(opts, err, nil)
			}
			if err := r.discoverHermes(ctx, &opts); err != nil {
				return r.fail(opts, err, nil)
			}
			if err := q.completeProjectSelectors(ctx, &opts); err != nil {
				return r.fail(opts, err, nil)
			}
			if err := q.completeLegacyPlanScope(ctx, &opts); err != nil {
				return r.fail(opts, err, nil)
			}
		} else if err := r.discoverHermes(ctx, &opts); err != nil {
			return r.fail(opts, err, nil)
		}
		hermesMetadata := metadataFor(opts)
		desired, err := opts.desiredState()
		if err != nil {
			return r.fail(opts, err, nil)
		}
		update, err := parseUpdate(opts.update)
		if err != nil {
			return r.fail(opts, err, nil)
		}
		if opts.command == "plan" {
			plan, err := r.Service.Plan(ctx, desired, update)
			if err != nil {
				return r.fail(opts, err, nil)
			}
			return r.writeResult(opts, commandResult{Command: "plan", Status: reconcile.Status(plan), Plan: plan, Hermes: hermesMetadata, RequiredCredentials: credentials.RequiredNamesForPlan(desired, plan.Actions)})
		}
		return r.runDesiredApply(ctx, opts, desired, hermesMetadata, session)
	}

	if strings.TrimSpace(opts.kitHome) == "" {
		return r.fail(opts, errors.New("KIT_HOME_REQUIRED"), nil)
	}
	switch opts.command {
	case "status":
		status, plan, err := r.Service.Status(ctx, opts.kitHome)
		if err != nil {
			return r.fail(opts, err, nil)
		}
		return r.writeResult(opts, commandResult{Command: "status", Status: status, Plan: plan})
	case "retry":
		ctx = r.withMutationOutput(ctx, opts)
		if err := r.Service.Retry(ctx, opts.kitHome); err != nil {
			return r.fail(opts, err, nil)
		}
		status, plan, err := r.Service.Status(ctx, opts.kitHome)
		if err != nil {
			return r.fail(opts, err, nil)
		}
		if status == reconcile.StatusReady {
			session.promote(ctx, r.Err, opts.kitHome)
		}
		return r.writeResult(opts, commandResult{Command: "retry", Status: status, Plan: plan})
	case "update":
		update, err := parseUpdate(opts.update)
		if err != nil {
			return r.fail(opts, err, nil)
		}
		var updatedPlan reconcile.OperationPlan
		if opts.credentialsStdin {
			if !opts.nonInteractive {
				return r.fail(opts, newOperationalError(codeInputRequired, "--credentials-stdin requires --non-interactive", nil), nil)
			}
			verified, state, inspectErr := r.environmentInspector().Inspect(ctx, opts.kitHome)
			verified, err = acceptManualInspection(verified, state, inspectErr)
			if err != nil {
				return r.fail(opts, err, nil)
			}
			plan, planErr := r.Service.Plan(ctx, verified.Desired, update)
			if planErr != nil {
				return r.fail(opts, planErr, nil)
			}
			r.ensureCredentials()
			providedSource, ok := r.Credentials.(ProvidedPlanCredentialSource)
			if !ok {
				return r.fail(opts, errors.New("CREDENTIAL_STDIN_UNSUPPORTED"), nil)
			}
			provided, payloadErr := r.readCredentialPayload()
			if payloadErr != nil {
				return r.fail(opts, payloadErr, nil)
			}
			if _, err := providedSource.ResolveProvidedForPlan(ctx, verified.Desired, plan.Actions, provided); err != nil {
				return r.fail(opts, err, nil)
			}
			ctx = r.withMutationOutput(ctx, opts)
			updatedPlan, err = r.Service.UpdateVerified(ctx, verified, update)
		} else {
			ctx = r.withMutationOutput(ctx, opts)
			updatedPlan, err = r.Service.Update(ctx, opts.kitHome, update)
		}
		if err != nil {
			return r.fail(opts, err, nil)
		}
		status, plan, err := r.Service.Status(ctx, opts.kitHome)
		if err != nil {
			return r.fail(opts, err, nil)
		}
		if update != reconcile.UpdateNone && len(updatedPlan.Actions) > 0 && status == reconcile.StatusReady {
			session.promote(ctx, r.Err, opts.kitHome)
		}
		return r.writeResult(opts, commandResult{Command: "update", Status: status, Plan: plan})
	default:
		return r.fail(opts, fmt.Errorf("unknown command %q", opts.command), nil)
	}
}

func (r *Runner) runDesiredApply(ctx context.Context, opts options, desired domain.DesiredState, metadata *hermesResult, session *registrySession) int {
	update, err := parseUpdate(opts.update)
	if err != nil {
		return r.fail(opts, err, nil)
	}
	plan, err := r.Service.Plan(ctx, desired, update)
	if err != nil {
		return r.fail(opts, err, nil)
	}
	if len(plan.Actions) == 0 {
		handoff, handoffErr := handoffFor(desired)
		if handoffErr != nil {
			return r.fail(opts, handoffErr, nil)
		}
		return r.writeResult(opts, commandResult{Command: "apply", Status: reconcile.Status(plan), Plan: plan, Handoff: handoff, Hermes: metadata, RequiredCredentials: credentials.RequiredNamesForPlan(desired, plan.Actions)})
	}
	secrets := map[string]string{}
	r.ensureCredentials()
	if r.Credentials != nil {
		if opts.credentialsStdin {
			if !opts.nonInteractive {
				return r.fail(opts, newOperationalError(codeInputRequired, "--credentials-stdin requires --non-interactive", nil), nil)
			}
			provided, payloadErr := r.readCredentialPayload()
			if payloadErr != nil {
				return r.fail(opts, payloadErr, nil)
			}
			providedSource, ok := r.Credentials.(ProvidedPlanCredentialSource)
			if !ok {
				return r.fail(opts, errors.New("CREDENTIAL_STDIN_UNSUPPORTED"), nil)
			}
			secrets, err = providedSource.ResolveProvidedForPlan(ctx, desired, plan.Actions, provided)
		} else if planned, ok := r.Credentials.(PlanCredentialSource); ok {
			secrets, err = planned.ResolveForPlan(ctx, desired, plan.Actions, !opts.nonInteractive)
		} else {
			secrets, err = r.Credentials.Resolve(ctx, desired, !opts.nonInteractive)
		}
		if err != nil {
			return r.fail(opts, err, nil)
		}
	}
	ctx = r.withMutationOutput(ctx, opts)
	appliedPlan, err := r.Service.Apply(ctx, desired, update, ApplyInputs{Secrets: secrets, HermesInstaller: opts.installerPath, CertificateArchive: opts.certificates})
	if err != nil {
		return r.fail(opts, err, secretValues(secrets))
	}
	status, finalPlan, err := r.Service.Status(ctx, desired.KitHome())
	if err != nil {
		return r.fail(opts, err, secretValues(secrets))
	}
	handoff, err := handoffFor(desired)
	if err != nil {
		return r.fail(opts, err, secretValues(secrets))
	}
	if len(appliedPlan.Actions) > 0 && status == reconcile.StatusReady {
		session.promote(ctx, r.Err, desired.KitHome())
	}
	return r.writeResult(opts, commandResult{Command: "apply", Status: status, Plan: finalPlan, Handoff: handoff, Hermes: metadata, RequiredCredentials: credentials.RequiredNamesForPlan(desired, plan.Actions)})
}

func (r Runner) readCredentialPayload() (map[string]string, error) {
	data, err := io.ReadAll(io.LimitReader(r.In, maxCredentialPayloadBytes+1))
	if err != nil {
		return nil, errors.New("CREDENTIAL_STDIN_INVALID")
	}
	if len(data) == 0 || len(data) > maxCredentialPayloadBytes {
		return nil, errors.New("CREDENTIAL_STDIN_INVALID")
	}
	var payload struct {
		SchemaVersion int               `json:"schema_version"`
		Credentials   map[string]string `json:"credentials"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || payload.SchemaVersion != 1 || len(payload.Credentials) == 0 {
		return nil, errors.New("CREDENTIAL_STDIN_INVALID")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("CREDENTIAL_STDIN_INVALID")
	}
	return payload.Credentials, nil
}

func (r Runner) discoverHermes(ctx context.Context, opts *options) error {
	if opts.application != string(domain.AppHermes) {
		return nil
	}
	if r.HermesDiscovery == nil {
		if opts.appInstalled != "" && opts.hermesHome != "" {
			return nil
		}
		return errors.New("HERMES_DISCOVERY_REQUIRED")
	}
	var override *bool
	if opts.appInstalled != "" {
		value, err := strconv.ParseBool(opts.appInstalled)
		if err != nil {
			return fmt.Errorf("APP_INSTALLED_INVALID: %q", opts.appInstalled)
		}
		override = &value
	}
	result, err := r.HermesDiscovery(ctx, hermes.DiscoveryRequest{OS: domain.OSFamily(opts.operatingSystem), ExplicitHome: opts.hermesHome, InstalledOverride: override, KitHome: opts.kitHome})
	if err != nil {
		return err
	}
	opts.appInstalled = strconv.FormatBool(result.Installed)
	opts.hermesHome = result.Home
	opts.hermesVersion = result.Version
	return nil
}

func metadataFor(opts options) *hermesResult {
	if opts.application != string(domain.AppHermes) {
		return nil
	}
	installed, _ := strconv.ParseBool(opts.appInstalled)
	return &hermesResult{Installed: installed, Home: opts.hermesHome, Version: opts.hermesVersion}
}

func (r *Runner) withDefaults() {
	if r.In == nil {
		r.In = os.Stdin
	}
	if r.Out == nil {
		r.Out = os.Stdout
	}
	if r.Err == nil {
		r.Err = os.Stderr
	}
	if r.GOOS == "" {
		r.GOOS = runtime.GOOS
	}
	if r.Executable == nil {
		r.Executable = os.Executable
	}
}

func (r *Runner) withOperationalDefaults() {
	if r.Environments == nil {
		r.Environments = environment.NewInspector()
	}
}

func (r *Runner) environmentInspector() environment.Inspector {
	r.withOperationalDefaults()
	return r.Environments
}

func (r *Runner) ensureService() {
	if r.Service == nil && r.ServiceFactory != nil {
		r.Service = r.ServiceFactory()
	}
}

func (r *Runner) ensureCredentials() {
	if r.Credentials == nil && r.CredentialFactory != nil {
		r.Credentials = r.CredentialFactory()
	}
}

func isPublicQuery(command string) bool {
	switch command {
	case "catalog", "detect-app", "environments":
		return true
	default:
		return false
	}
}

func (r *Runner) runPublicQuery(ctx context.Context, opts options) int {
	if !opts.jsonOutput {
		return r.fail(opts, newOperationalError(codeInputRequired, "--json", nil), nil)
	}
	switch opts.command {
	case "catalog":
		return r.writePublicJSON(publicCatalog())
	case "detect-app":
		result, err := r.publicDetection(ctx, opts.application, opts.appInstalled)
		if err != nil {
			return r.fail(opts, publicDetectionError(err), nil)
		}
		return r.writePublicJSON(result)
	case "environments":
		result, err := r.publicEnvironments(ctx, opts.application)
		if err != nil {
			return r.fail(opts, publicEnvironmentError(err), nil)
		}
		return r.writePublicJSON(result)
	default:
		return r.fail(opts, fmt.Errorf("unknown command %q", opts.command), nil)
	}
}

func publicDetectionError(err error) error {
	if errors.Is(err, apps.ErrApplicationRequired) || errors.Is(err, context.Canceled) {
		return err
	}
	var operational *operationalError
	if errors.As(err, &operational) && operational.Code == codeInputRequired {
		return err
	}
	return &publicQueryError{Code: codeAIAppInspectionFailed, Message: "selected AI application could not be verified", Cause: err}
}

func publicEnvironmentError(err error) error {
	if errors.Is(err, apps.ErrApplicationRequired) || errors.Is(err, context.Canceled) {
		return err
	}
	return &publicQueryError{Code: codeWorkspaceInspectionFailed, Message: "environment discovery failed", Cause: err}
}

func (r Runner) writePublicJSON(result any) int {
	if err := json.NewEncoder(r.Out).Encode(result); err != nil {
		fmt.Fprintln(r.Err, "OUTPUT_FAILED")
		return ExitFailure
	}
	return ExitOK
}

func (r Runner) writeResult(opts options, result commandResult) int {
	if opts.jsonEvents {
		if err := json.NewEncoder(r.Out).Encode(wizardEvent{
			SchemaVersion:       1,
			Event:               "result",
			Command:             result.Command,
			Status:              result.Status,
			Plan:                result.Plan,
			Handoff:             result.Handoff,
			RequiredCredentials: result.RequiredCredentials,
			Hermes:              result.Hermes,
			CheckpointPath:      result.CheckpointPath,
			Mode:                result.Mode,
		}); err != nil {
			fmt.Fprintln(r.Err, "OUTPUT_FAILED")
			return ExitFailure
		}
		return ExitOK
	}
	if opts.jsonOutput {
		if err := json.NewEncoder(r.Out).Encode(result); err != nil {
			fmt.Fprintln(r.Err, "OUTPUT_FAILED")
			return ExitFailure
		}
		return ExitOK
	}
	if result.Command == "version" {
		data, err := json.Marshal(result.Info)
		if err != nil {
			fmt.Fprintln(r.Err, "OUTPUT_FAILED")
			return ExitFailure
		}
		fmt.Fprintln(r.Out, string(data))
		return ExitOK
	}
	if result.Hermes != nil && result.Hermes.Installed {
		fmt.Fprintln(r.Out, "Hermes найден.")
		fmt.Fprintf(r.Out, "Версия: %s\n", result.Hermes.Version)
		fmt.Fprintf(r.Out, "HERMES_HOME: %s\n", result.Hermes.Home)
	}
	fmt.Fprintf(r.Out, "%s: %s\n", result.Command, result.Status)
	for _, action := range result.Plan.Actions {
		fmt.Fprintf(r.Out, "- %s\n", action.ID)
	}
	if result.Handoff != "" {
		fmt.Fprintf(r.Out, "handoff: %s\n", result.Handoff)
	}
	if result.Info != nil {
		data, _ := json.Marshal(result.Info)
		fmt.Fprintln(r.Out, string(data))
	}
	return ExitOK
}

func (r Runner) runWizardState(ctx context.Context, opts options) int {
	if !opts.nonInteractive || (!opts.jsonOutput && !opts.jsonEvents) {
		return r.fail(opts, newOperationalError(codeInputRequired, "wizard-state requires --non-interactive and --json or --json-events", nil), nil)
	}
	if !opts.wizardModeSet || !opts.operatingSystemSet || opts.operatingSystem != string(domain.OSWindows) ||
		!opts.applicationSet || !opts.appInstalledSet || !opts.appInstalledExact || !opts.kitHomeSet {
		return r.fail(opts, newOperationalError(codeInputRequired, "wizard-state requires canonical Windows selections", nil), nil)
	}
	if _, err := catalog.LookupAIApplication(domain.AIApplication(opts.application)); err != nil {
		return r.fail(opts, err, nil)
	}
	installed, err := opts.applicationInstalled()
	if err != nil {
		return r.fail(opts, err, nil)
	}
	if !installed {
		return r.fail(opts, apps.ErrApplicationRequired, nil)
	}
	if err := environment.ValidateTerminalPath(opts.kitHome); err != nil {
		return r.fail(opts, newOperationalError(codeForeignWorkspace, "workspace path is unsafe", err), nil)
	}

	update, err := parseUpdate(opts.update)
	if err != nil {
		return r.fail(opts, err, nil)
	}
	values := map[string]string{
		"TEAMKIT_APP_ID":         opts.application,
		"TEAMKIT_MODE":           opts.wizardMode,
		"TEAMKIT_WORKSPACE_ROOT": opts.kitHome,
	}
	switch opts.wizardMode {
	case "add":
		if update != reconcile.UpdateNone || !opts.projectSet || !opts.roleSet || !opts.toolchainSet {
			return r.fail(opts, newOperationalError(codeInputRequired, "add checkpoint requires project, role, toolchain, and --update none", nil), nil)
		}
		if _, err := catalog.LookupProject(domain.ProjectID(opts.project)); err != nil {
			return r.fail(opts, err, nil)
		}
		if _, err := catalog.LookupRole(domain.Role(opts.role)); err != nil {
			return r.fail(opts, err, nil)
		}
		if _, err := catalog.LookupToolchain(domain.Toolchain(opts.toolchain)); err != nil {
			return r.fail(opts, err, nil)
		}
		state, err := r.environmentInspector().ClassifyAdd(ctx, opts.kitHome)
		if err != nil {
			return r.fail(opts, publicEnvironmentError(err), nil)
		}
		if state != environment.AddTargetReady {
			return r.fail(opts, newOperationalError(codeWorkspaceExistsUseUpdate, "workspace already exists", nil), nil)
		}
		values["TEAMKIT_PROJECT_ID"] = opts.project
		values["TEAMKIT_ROLE_ID"] = opts.role
		values["TEAMKIT_TOOLCHAIN_ID"] = opts.toolchain
	case "update":
		if opts.projectSet || opts.roleSet || opts.toolchainSet {
			return r.fail(opts, newOperationalError(codeInputRequired, "update checkpoint does not accept project, role, or toolchain", nil), nil)
		}
		verified, state, err := r.environmentInspector().Inspect(ctx, opts.kitHome)
		if err != nil {
			return r.fail(opts, publicEnvironmentError(err), nil)
		}
		if state != environment.Ready || verified.Desired.Application() != domain.AIApplication(opts.application) {
			return r.fail(opts, newOperationalError(codeForeignWorkspace, "workspace identity does not match selected application", nil), nil)
		}
		values["TEAMKIT_UPDATE_SCOPE"] = string(update)
	default:
		return r.fail(opts, newOperationalError(codeInputRequired, "mode must be add or update", nil), nil)
	}
	if err := workspace.WriteWizardEnv(opts.kitHome, values); err != nil {
		return r.fail(opts, err, nil)
	}
	return r.writeResult(opts, commandResult{Command: "wizard-state", CheckpointPath: workspace.WizardEnvPath(opts.kitHome), Mode: opts.wizardMode})
}

func handoffFor(desired domain.DesiredState) (string, error) {
	if desired.Application() == domain.AppHermes {
		return "", nil
	}
	toolchain, err := apps.PinnedToolchain(desired.Toolchain())
	if err != nil {
		return "", err
	}
	handoff, err := apps.PrepareHandoff(apps.Application{ID: string(desired.Application()), Installed: desired.AppInstalled()}, apps.HandoffRequest{Toolchain: toolchain})
	if err != nil {
		return "", err
	}
	return handoff.Command, nil
}

func (r Runner) fail(opts options, err error, secrets []string) int {
	message := redact(err.Error(), secrets)
	code, exit := errorIdentity(err)
	if opts.jsonEvents {
		if encodeErr := json.NewEncoder(r.Out).Encode(wizardEvent{SchemaVersion: 1, Event: "failure", Code: code, Message: message}); encodeErr != nil {
			fmt.Fprintln(r.Err, "OUTPUT_FAILED")
		}
		return exit
	}
	if exit == ExitApplicationRequired && !opts.nonInteractive && !opts.jsonOutput {
		fmt.Fprintln(r.Err, applicationSetupInstructions(opts.application))
		return exit
	}
	if opts.jsonOutput {
		_ = json.NewEncoder(r.Err).Encode(map[string]any{"error": code, "message": message})
	} else {
		fmt.Fprintf(r.Err, "%s: %s\n", code, message)
	}
	return exit
}

func (r Runner) withMutationOutput(ctx context.Context, opts options) context.Context {
	if opts.jsonEvents {
		return withJSONEventProgress(ctx, r.Out)
	}
	return withMutationProgress(ctx, r.Out, !opts.jsonOutput)
}

func applicationSetupInstructions(application string) string {
	entry, err := catalog.LookupAIApplication(domain.AIApplication(application))
	if err != nil {
		return "Выбранное AI-приложение не установлено. Установите его, подключите языковую модель и повторите запуск TeamKit."
	}
	return fmt.Sprintf("%s не установлен.\nУстановите %s, подключите в нём языковую модель и повторите запуск TeamKit.\nПосле подготовки окружения откройте чат %s и вставьте туда инструкцию TeamKit из .teamkit\\handoff.txt.", entry.Label, entry.Label, entry.Label)
}

func secretValues(values map[string]string) []string {
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func redact(message string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	return message
}

func (r Runner) runHermesContinuation(ctx context.Context, opts *options, q *questionnaire, session *registrySession) int {
	if err := r.prepareHermesHome(ctx, opts, q); err != nil {
		return r.fail(*opts, err, nil)
	}
	if err := r.discoverHermes(ctx, opts); err != nil {
		if errors.Is(err, hermes.ErrExecutableNotFound) {
			return r.writeHermesInstallHandoff(*opts)
		}
		return r.fail(*opts, err, nil)
	}
	installed, _ := strconv.ParseBool(opts.appInstalled)
	if !installed {
		return r.writeHermesInstallHandoff(*opts)
	}
	if r.Service == nil {
		return r.fail(*opts, errors.New("SERVICE_REQUIRED"), nil)
	}
	desired, err := opts.desiredState()
	if err != nil {
		return r.fail(*opts, err, nil)
	}
	return r.runDesiredApply(ctx, *opts, desired, metadataFor(*opts), session)
}
