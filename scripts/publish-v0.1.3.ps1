[CmdletBinding()]
param(
    [string]$CandidateSha,
    [string]$Version = 'v0.1.3',
    [string]$MaxMinutes = '180',
    [string]$OutputPath
)

$entryStopwatch = [System.Diagnostics.Stopwatch]::StartNew()
$script:EntryDeadlineSeconds = 180 * 60
$ErrorActionPreference = 'Stop'
$ConfirmPreference = 'None'
$ProgressPreference = 'SilentlyContinue'
$env:GIT_TERMINAL_PROMPT = '0'

function Get-EntryRemainingSeconds {
    return [Math]::Max([double]0, [double]$script:EntryDeadlineSeconds - $entryStopwatch.Elapsed.TotalSeconds)
}

function Get-EntryElapsedSeconds {
    return [int][Math]::Floor([Math]::Min([double]$script:EntryDeadlineSeconds, $entryStopwatch.Elapsed.TotalSeconds))
}

function New-EntryFailure([string]$ReasonCode) {
    return @{ status = 'failed'; exit_code = 1; stage = 'entry'; reason_code = $ReasonCode; error = 'publisher initialization failed'; duration_seconds = Get-EntryElapsedSeconds }
}

function Write-ReleaseResult([hashtable]$Result) {
    if (-not $Result.ContainsKey('duration_seconds')) { $Result.duration_seconds = Get-EntryElapsedSeconds }
    $json = $Result | ConvertTo-Json -Depth 8 -Compress
    [Console]::Out.WriteLine($json)
    if ([string]::IsNullOrWhiteSpace($OutputPath) -or (Get-EntryRemainingSeconds) -le 0) { return }
    $cancellation = $null
    try {
        $milliseconds = [Math]::Max(1, [int][Math]::Floor((Get-EntryRemainingSeconds) * 1000))
        $cancellation = [System.Threading.CancellationTokenSource]::new()
        $cancellation.CancelAfter($milliseconds)
        $writeTask = [System.IO.File]::WriteAllTextAsync($OutputPath, $json, [Text.UTF8Encoding]::new($false), $cancellation.Token)
        $deadlineTask = [System.Threading.Tasks.Task]::Delay($milliseconds)
        $winner = [System.Threading.Tasks.Task]::WhenAny([System.Threading.Tasks.Task[]]@($writeTask, $deadlineTask)).GetAwaiter().GetResult()
        if ($winner -eq $writeTask) { $writeTask.GetAwaiter().GetResult() } else { $cancellation.Cancel() }
    } catch {
        # The terminal JSON has already been written. Output persistence is optional.
    } finally {
        if ($cancellation) { $cancellation.Dispose() }
    }
}

$result = $null
try {
    if ([string]::IsNullOrWhiteSpace($CandidateSha)) { throw [System.InvalidOperationException]::new('ENTRY_CANDIDATE_SHA_REQUIRED') }
    if ($CandidateSha -notmatch '^[0-9a-fA-F]{40}$') { throw [System.InvalidOperationException]::new('ENTRY_CANDIDATE_SHA_INVALID') }
    if ($Version -cne 'v0.1.3') { throw [System.InvalidOperationException]::new('ENTRY_VERSION_INVALID') }
    [int]$parsedMaxMinutes = 0
    if (-not [int]::TryParse($MaxMinutes, [ref]$parsedMaxMinutes) -or $parsedMaxMinutes -lt 1 -or $parsedMaxMinutes -gt 180) { throw [System.InvalidOperationException]::new('ENTRY_MAX_MINUTES_INVALID') }
    $script:EntryDeadlineSeconds = $parsedMaxMinutes * 60
    if ((Get-EntryRemainingSeconds) -le 0) { throw [System.TimeoutException]::new('ENTRY_DEADLINE_EXCEEDED') }
    if ([string]::IsNullOrWhiteSpace($env:GH_TOKEN) -or [string]::IsNullOrWhiteSpace($env:GITLAB_TOKEN)) { throw [System.InvalidOperationException]::new('ENTRY_TOKENS_REQUIRED') }
    Import-Module (Join-Path $PSScriptRoot 'release/BoundedRelease.psm1') -Force
    if ((Get-EntryRemainingSeconds) -le 0) { throw [System.TimeoutException]::new('ENTRY_DEADLINE_EXCEEDED') }
    $context = New-ReleaseContext -CandidateSha $CandidateSha -Version $Version -MaxMinutes $parsedMaxMinutes -Stopwatch $entryStopwatch
    $result = Publish-TeamKitRelease -Context $context
} catch [System.TimeoutException] {
    $result = @{ status = 'deadline_exceeded'; exit_code = 124; stage = 'entry'; reason_code = 'DEADLINE_EXCEEDED'; error = 'publisher initialization failed'; duration_seconds = Get-EntryElapsedSeconds }
} catch {
    $reasonCode = if ($_.Exception.Message -match '^ENTRY_[A-Z0-9_]+$') { $_.Exception.Message } else { 'ENTRY_INITIALIZATION_FAILED' }
    $result = New-EntryFailure $reasonCode
}

Write-ReleaseResult $result
exit [int]$result.exit_code
