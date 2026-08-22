# Task 1 report — Atlassian runtime contract

## Status

Completed. Jira and Confluence MCPs are enabled in rendered and validated Hermes profiles. Hermes now requires, persists, and forwards exactly the provider, Jira, and Confluence secrets for configure actions.

## Files

- `internal/hermes/profile.go`, `internal/hermes/managed_state.go`, and their tests: enabled Atlassian MCP render and managed-state validation.
- `internal/credentials/resolver.go`, `internal/credentials/console.go`, and tests: exported `JiraToken` / `ConfluenceToken`, ordered resolution and masked labels.
- `internal/service/service.go` and tests: exact three-key Hermes profile environment for apply/retry.
- `internal/service/operation_contract_test.go`: fixture-only nonempty Jira/Confluence values for existing Hermes configure/retry cases; no operation-contract production code, canonical JSON/hash, or assertions changed.
- `internal/cli/run_test.go`, `test/integration/blackbox_test.go`, and `test/security/redaction_test.go`: Jira/Confluence redaction coverage in CLI output, rejected flags, config, plan/receipt/log/audit data, and captured argv.

## RED/GREEN evidence

- RED (enabled): `go test ./internal/hermes -run 'TestProfile_RenderForSchema_RendersCorporateAtlassianMCPs|Test.*Managed.*Atlassian' -count=1` failed for schemas 34/37 with `Jira MCP must be enabled`, and rejected the enabled managed config as `managed config mismatch`.
- GREEN (enabled): `go test ./internal/hermes -count=1` passed.
- RED (credentials): `go test ./internal/credentials ./internal/service -run 'Test.*(Hermes|Credential|Secret|Retry)' -count=1` failed because only the provider key was loaded/forwarded and the new labels were absent.
- GREEN (credentials/service): `go test ./internal/credentials ./internal/service -count=1` passed.
- RED (redaction): temporarily injecting `jira-personal-canary-7xQ2mN9pL4vK8dR6` into the log fixture made `TestApplyFailure_MultiSecretCanariesStayOutOfEveryObservableSink` fail with `execution_log leaked a runtime secret canary`; the injection was reverted.
- GREEN (final target suite): `go test ./internal/hermes ./internal/credentials ./internal/service ./internal/cli ./test/integration ./test/security -count=1` completed without failures. Focused checks also passed for the new integration and security coverage.

## Self-review

Reviewed the complete diff and ran `git diff --check`. The three-key selection is explicit rather than forwarding the entire credential map; Git-only and non-Hermes paths retain their existing credential selection; the operation contract is untouched apart from test fixture credentials; no secret value is added to production output or config.

## Commit

`HEAD: feat(hermes): require enabled Atlassian MCP access`.

## Concerns

None. The pre-existing untracked `.teamkit-test-cache/` was preserved.
