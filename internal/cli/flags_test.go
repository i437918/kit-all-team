package cli

import (
	"io"
	"testing"
)

func TestOptions_HermesContinuationRequiresExactRawAppInstalledToken(t *testing.T) {
	for name, spelling := range map[string][]string{
		"separate long flag value":   {"--app-installed", "true"},
		"single dash with equals":    {"-app-installed=true"},
		"single dash separate value": {"-app-installed", "true"},
	} {
		t.Run(name, func(t *testing.T) {
			canonical := continuationArgs(`C:\TeamKit`, `C:\Hermes`)
			args := append([]string{"apply"}, spelling...)
			args = append(args, canonical[2:]...)

			opts, err := parseOptions(args, io.Discard)
			if err != nil {
				t.Fatalf("ordinary flag parsing changed: %v", err)
			}
			if opts.appInstalled != "true" || !opts.appInstalledSet {
				t.Fatalf("ordinary parsed value = %q, set=%t", opts.appInstalled, opts.appInstalledSet)
			}
			if opts.isHermesContinuation() || opts.isHermesContinuationShape() {
				t.Fatalf("noncanonical spelling triggered early continuation: %v", args)
			}
		})
	}
}
