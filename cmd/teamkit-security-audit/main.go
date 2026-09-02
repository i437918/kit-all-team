// Command teamkit-security-audit scans release inputs without printing matched
// paths or values and emits deterministic JSON evidence.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mi1man-cmd/kit-all-team/internal/securityaudit"
	"github.com/mi1man-cmd/kit-all-team/internal/workspace"
)

type pathFlags []string

func (p *pathFlags) String() string { return strings.Join(*p, string(os.PathListSeparator)) }
func (p *pathFlags) Set(value string) error {
	*p = append(*p, value)
	return nil
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("teamkit-security-audit", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var paths pathFlags
	repository := flags.String("repository", "", "repository root")
	commit := flags.String("commit", "", "expected source commit")
	historyRef := flags.String("history-ref", "", "exact HEAD commit whose ancestry is audited; empty scans all refs")
	output := flags.String("output", "", "JSON evidence path")
	flags.Var(&paths, "path", "artifact file or directory; repeatable")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || strings.TrimSpace(*output) == "" {
		fmt.Fprintln(stderr, "SECURITY_AUDIT_USAGE")
		return 2
	}

	report, err := securityaudit.Audit(ctx, securityaudit.Options{
		Repository: *repository,
		Paths:      []string(paths),
		Commit:     *commit,
		HistoryRef: *historyRef,
	})
	if err != nil {
		fmt.Fprintln(stderr, "SECURITY_AUDIT_ERROR")
		return 2
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "SECURITY_AUDIT_ERROR")
		return 2
	}
	data = append(data, '\n')
	if err := workspace.WriteFileAtomic(*output, data, 0o600); err != nil {
		fmt.Fprintln(stderr, "SECURITY_AUDIT_ERROR")
		return 2
	}
	if !report.Passed {
		fmt.Fprintln(stdout, "security-audit: failed")
		return 1
	}
	fmt.Fprintln(stdout, "security-audit: passed")
	return 0
}
