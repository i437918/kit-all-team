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
	var credentialSource cli.CredentialSource
	if inputFile, ok := in.(*os.File); ok {
		sharedInput := bufio.NewReader(inputFile)
		in = sharedInput
		credentialSource = credentials.Resolver{Reader: credentials.NewConsoleReaderWithBufferedInput(inputFile, errOut, sharedInput)}
	}
	return cli.Runner{
		Service:      service.New(service.Options{}),
		Credentials:  credentialSource,
		In:           in,
		Out:          out,
		Err:          errOut,
		Environments: environment.NewInspector(),
		Registry:     registry.NewDefault(),
		HermesDiscovery: func(ctx context.Context, request hermes.DiscoveryRequest) (hermes.DiscoveryResult, error) {
			return hermes.Discover(ctx, request, hermes.DiscoveryDependencies{})
		},
	}
}
