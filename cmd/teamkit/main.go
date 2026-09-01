package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/signal"

	"github.com/mi1man-cmd/kit-all-team/internal/cli"
	"github.com/mi1man-cmd/kit-all-team/internal/credentials"
	"github.com/mi1man-cmd/kit-all-team/internal/environment"
	"github.com/mi1man-cmd/kit-all-team/internal/gitx"
	"github.com/mi1man-cmd/kit-all-team/internal/hermes"
	"github.com/mi1man-cmd/kit-all-team/internal/platform"
	"github.com/mi1man-cmd/kit-all-team/internal/registry"
	"github.com/mi1man-cmd/kit-all-team/internal/service"
)

func main() {
	if gitx.IsAskPassInvocation(os.Getenv) {
		os.Exit(gitx.RunAskPass(os.Args[1:], os.Getenv, os.Stdout))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := newRunner(os.Stdin, os.Stdout, os.Stderr).Run(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}

func newRunner(in io.Reader, out, errOut io.Writer) cli.Runner {
	var credentialFactory func() cli.CredentialSource
	if inputFile, ok := in.(*os.File); ok {
		sharedInput := bufio.NewReader(inputFile)
		in = sharedInput
		reader := credentials.NewConsoleReaderWithBufferedInput(inputFile, errOut, sharedInput)
		credentialFactory = func() cli.CredentialSource {
			return credentials.Resolver{Reader: reader, Chooser: reader}
		}
	}
	return cli.Runner{
		ServiceFactory:      func() cli.Service { return service.New(service.Options{}) },
		CredentialFactory:   credentialFactory,
		In:                  in,
		Out:                 out,
		Err:                 errOut,
		Environments:        environment.NewInspector(),
		Registry:            registry.NewDefault(),
		ConfigureHermesHome: platform.ConfigureHermesHome,
		HermesDiscovery: func(ctx context.Context, request hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			return hermes.Discover(ctx, request, hermes.DiscoveryDependencies{})
		},
	}
}
