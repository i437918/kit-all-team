package cli

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/catalog"
	"github.com/mi1man-cmd/kit-all-team/internal/domain"
	"github.com/mi1man-cmd/kit-all-team/internal/reconcile"
)

type options struct {
	command         string
	operatingSystem string
	application     string
	appInstalled    string
	kitHome         string
	hermesHome      string
	hermesVersion   string
	project         string
	role            string
	toolchain       string
	update          string
	kitHomeSet      bool
	toolchainSet    bool
	updateSet       bool
	jsonOutput      bool
	nonInteractive  bool
	installerPath   string
	certificates    string
}

func parseOptions(args []string, errors io.Writer) (options, error) {
	if len(args) == 1 {
		switch args[0] {
		case "-h", "--help":
			args = []string{"help"}
		case "--version":
			args = []string{"version"}
		}
	}
	if len(args) == 0 {
		return options{}, fmt.Errorf("command required: plan, apply, status, retry, update, or version")
	}
	opts := options{command: strings.ToLower(args[0])}
	flags := flag.NewFlagSet(opts.command, flag.ContinueOnError)
	flags.SetOutput(errors)
	flags.BoolVar(&opts.jsonOutput, "json", false, "emit machine-readable JSON")

	switch opts.command {
	case "plan", "apply":
		flags.StringVar(&opts.operatingSystem, "os", "", "windows, macos, linux, or altlinux")
		flags.StringVar(&opts.application, "app", "", "AI application ID")
		flags.StringVar(&opts.appInstalled, "app-installed", "", "whether the AI application is installed")
		flags.StringVar(&opts.kitHome, "kit-home", "", "absolute KIT_ALL_TEAM_HOME")
		flags.StringVar(&opts.hermesHome, "hermes-home", "", "absolute HERMES_HOME")
		flags.StringVar(&opts.project, "project", "", "project ID")
		flags.StringVar(&opts.role, "role", "", "analyst, developer, or architect")
		flags.StringVar(&opts.toolchain, "toolchain", "", "ai_rules_1c or cc_1c_skills")
		flags.StringVar(&opts.update, "update", "", "none, content, database, or both")
		flags.StringVar(&opts.installerPath, "hermes-installer", "", "verified local Hermes installer")
		flags.StringVar(&opts.certificates, "certs", "", "verified certificate ZIP")
		flags.BoolVar(&opts.nonInteractive, "non-interactive", false, "fail instead of asking for missing selections")
	case "status", "retry":
		flags.StringVar(&opts.kitHome, "kit-home", "", "absolute KIT_ALL_TEAM_HOME")
	case "update":
		flags.StringVar(&opts.kitHome, "kit-home", "", "absolute KIT_ALL_TEAM_HOME")
		flags.StringVar(&opts.update, "target", "", "content, database, both, or none")
	case "version", "help":
	default:
		return options{}, fmt.Errorf("unknown command %q", opts.command)
	}
	if err := flags.Parse(args[1:]); err != nil {
		return options{}, err
	}
	flags.Visit(func(flag *flag.Flag) {
		switch flag.Name {
		case "kit-home":
			opts.kitHomeSet = true
		case "toolchain":
			opts.toolchainSet = true
		case "update", "target":
			opts.updateSet = true
		}
	})
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return opts, nil
}

func (o options) desiredState() (domain.DesiredState, error) {
	installed, err := o.applicationInstalled()
	if err != nil {
		return domain.DesiredState{}, err
	}
	return domain.NewDesiredState(domain.DesiredStateInput{
		OS:            domain.OSFamily(o.operatingSystem),
		Application:   domain.AIApplication(o.application),
		AppInstalled:  installed,
		KitHome:       o.kitHome,
		HermesHome:    o.hermesHome,
		HermesVersion: o.hermesVersion,
		Project:       domain.ProjectID(o.project),
		Role:          domain.Role(o.role),
		Toolchain:     domain.Toolchain(o.toolchain),
	})
}

func (o options) applicationInstalled() (bool, error) {
	installed, err := strconv.ParseBool(o.appInstalled)
	if err != nil {
		return false, fmt.Errorf("APP_INSTALLED_INVALID: %q", o.appInstalled)
	}
	if !installed && isKnownNonHermesApplication(o.application) {
		return false, domain.NewValidationError(domain.AIAppRequired, "application", o.application)
	}
	return installed, nil
}

func isKnownNonHermesApplication(application string) bool {
	for _, item := range catalog.AIApplications() {
		if string(item.ID) == application {
			return item.ID != domain.AppHermes
		}
	}
	return false
}

func parseUpdate(value string) (reconcile.UpdateChoice, error) {
	if value == "" {
		return reconcile.UpdateNone, nil
	}
	choice := reconcile.UpdateChoice(value)
	switch choice {
	case reconcile.UpdateNone, reconcile.UpdateContent, reconcile.UpdateDatabase, reconcile.UpdateBoth:
		return choice, nil
	default:
		return "", fmt.Errorf("UPDATE_CHOICE_UNKNOWN: %q", value)
	}
}
