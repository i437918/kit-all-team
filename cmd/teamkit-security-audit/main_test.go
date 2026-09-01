package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/mi1man-cmd/kit-all-team/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/securityaudit"
)

func TestRun_WritesSecretFreeFailureEvidenceAndStableOutput(t *testing.T) {
	root := testutil.TempDir(t)
	secret := strings.Join([]string{"ghp", "_", strings.Repeat("C", 40)}, "")
	if err := os.WriteFile(filepath.Join(root, "output.log"), []byte("failure "+secret), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(testutil.TempDir(t), "security-audit.json")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--path", root, "--commit", strings.Repeat("d", 40), "--output", manifest}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	manifestBytes, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	combined := append(append([]byte{}, stdout.Bytes()...), stderr.Bytes()...)
	combined = append(combined, manifestBytes...)
	if bytes.Contains(combined, []byte(secret)) {
		t.Fatal("command output exposed the detected secret")
	}
	var report securityaudit.Report
	if err := json.Unmarshal(manifestBytes, &report); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	if report.Passed || len(report.Findings) == 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestRun_RejectsMissingInputWithoutEchoingPath(t *testing.T) {
	missing := filepath.Join(testutil.TempDir(t), "PRIVATE-PATH-CANARY")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--path", missing, "--output", filepath.Join(testutil.TempDir(t), "report.json")}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit=%d", code)
	}
	if strings.Contains(stdout.String()+stderr.String(), missing) || !strings.Contains(stderr.String(), "SECURITY_AUDIT_ERROR") {
		t.Fatalf("unsafe diagnostics stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRun_RejectsHistoryRefWithoutRepository(t *testing.T) {
	root := testutil.TempDir(t)
	manifest := filepath.Join(testutil.TempDir(t), "report.json")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"--path", root,
		"--commit", strings.Repeat("d", 40),
		"--history-ref", strings.Repeat("d", 40),
		"--output", manifest,
	}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "SECURITY_AUDIT_ERROR") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
