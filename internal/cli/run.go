// Package cli implements Team Kit's stable command-line contract.
package cli

import (
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
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

const (
	ExitOK                  = 0
	ExitFailure             = 1
	ExitUsage               = 2
	ExitApplicationRequired = 3
	ExitLocalChanges        = 4
	ExitInterrupted         = 130
)

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

// Runner owns I/O and dispatch for one command invocation.
type Runner struct {
	Service         Service
	Credentials     CredentialSource
	In              io.Reader
	Out             io.Writer
	Err             io.Writer
	HermesDiscovery func(context.Context, hermes.DiscoveryRequest) (hermes.DiscoveryResult, error)
	Environments    environment.Inspector
	Registry        EnvironmentRegistry
	GOOS            string
	Executable      func() (string, error)
}

type commandResult struct {
	Command string                  `json:"command"`
	Status  reconcile.PlanStatus    `json:"status,omitempty"`
	Plan    reconcile.OperationPlan `json:"plan,omitempty"`
	Handoff string                  `json:"handoff,omitempty"`
	Info    any                     `json:"info,omitempty"`
	Hermes  *hermesResult           `json:"hermes,omitempty"`
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
	session := &registrySession{store: r.Registry}
	if opts.command == "help" {
		fmt.Fprintln(r.Out, "teamkit plan|apply|status|retry|update|version")
		return ExitOK
	}
	if opts.command == "version" {
		return r.writeResult(opts, commandResult{Command: "version", Info: buildinfo.Current()})
	}
	if (opts.command == "plan" || opts.command == "apply") && opts.nonInteractive && isKnownNonHermesApplication(opts.application) {
		if _, err := opts.applicationInstalled(); err != nil {
			return r.fail(opts, err, nil)
		}
	}
	if opts.command == "apply" && !opts.nonInteractive {
		q := newQuestionnaire(r.In, r.Out)
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
			if err := q.completeKitHome(ctx, &opts); err != nil {
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
			return r.writeResult(opts, commandResult{Command: "plan", Status: reconcile.Status(plan), Plan: plan, Hermes: hermesMetadata})
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
		updatedPlan, err := r.Service.Update(ctx, opts.kitHome, update)
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

func (r Runner) runDesiredApply(ctx context.Context, opts options, desired domain.DesiredState, metadata *hermesResult, session *registrySession) int {
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
		return r.writeResult(opts, commandResult{Command: "apply", Status: reconcile.Status(plan), Plan: plan, Handoff: handoff, Hermes: metadata})
	}
	secrets := map[string]string{}
	if r.Credentials != nil {
		if planned, ok := r.Credentials.(PlanCredentialSource); ok {
			secrets, err = planned.ResolveForPlan(ctx, desired, plan.Actions, !opts.nonInteractive)
		} else {
			secrets, err = r.Credentials.Resolve(ctx, desired, !opts.nonInteractive)
		}
		if err != nil {
			return r.fail(opts, err, nil)
		}
	}
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
	return r.writeResult(opts, commandResult{Command: "apply", Status: status, Plan: finalPlan, Handoff: handoff, Hermes: metadata})
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
	if r.Environments == nil {
		r.Environments = environment.NewInspector()
	}
	if r.GOOS == "" {
		r.GOOS = runtime.GOOS
	}
	if r.Executable == nil {
		r.Executable = os.Executable
	}
}

func (r Runner) writeResult(opts options, result commandResult) int {
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
	if opts.jsonOutput {
		_ = json.NewEncoder(r.Err).Encode(map[string]any{"error": code, "message": message})
	} else {
		fmt.Fprintf(r.Err, "%s: %s\n", code, message)
	}
	return exit
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
