param([ValidatePattern('^Test-V015[A-Za-z0-9]+$')][string]$Only)

$ErrorActionPreference = 'Stop'

$modulePath = Join-Path $PSScriptRoot 'BoundedRelease.psm1'
Import-Module $modulePath -Force

function Assert-Equal([object]$Actual, [object]$Expected, [string]$Message) {
    if ($Actual -ne $Expected) { throw "$Message expected=[$Expected] actual=[$Actual]" }
}

function Assert-True([bool]$Value, [string]$Message) {
    if (-not $Value) { throw $Message }
}

function Test-V015ReleaseContextUsesOnlyCallerSuppliedGitLabBaseUrl {
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y -GitLabBaseUrl 'https://gitlab.local.invalid'
    Assert-Equal $context.GitLabBaseUrl 'https://gitlab.local.invalid' 'release context must use caller-supplied GitLab base URL'
}

function Test-NullSecurityAuditFindingsMeansZeroFindings {
    $candidate = 'b' * 40
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y
    $audit = @{ commit = $candidate; passed = $true; findings = $null }
    $command = & (Get-Module BoundedRelease) { Get-Command Test-SecurityAuditEvidence -ErrorAction SilentlyContinue }
    Assert-True ($null -ne $command) 'the release module must validate the auditor JSON shape through one production helper'
    & (Get-Module BoundedRelease) { param($Context, $Audit) Test-SecurityAuditEvidence $Context $Audit } $context $audit
}

function Test-NarrowProductionSeamsAreAccepted {
    $http = { param($Context, $Request) throw 'test transport was not expected' }
    $process = { param($Context, $FileName, $Arguments, $TimeoutSeconds) throw 'test process was not expected' }
    $sleep = { param($Context, $Seconds) }
    $clock = { param($Context) 0 }
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken 'release-token-canary-8qN4sV7zK2mP6xR9' -GitLabToken 'release-token-canary-8qN4sV7zK2mP6xR9' -HttpAdapter $http -ProcessAdapter $process -SleepAdapter $sleep -ClockAdapter $clock
    Assert-True ($null -ne $context.HttpAdapter) 'HTTP adapter must be stored'
    Assert-True ($null -ne $context.SleepAdapter) 'sleep adapter must be stored'
}

function Test-FinalValidationPayloadContract {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    Assert-True $module.Contains('ConvertTo-Json -Depth 8 -Compress') 'nested release payload must use sufficient JSON depth'
    Assert-True $module.Contains('-join "`n"') 'GitLab checksum rows must be newline-delimited'
    Assert-True $module.Contains('candidate-binaries') 'candidate artifact must be selected by exact name'
    Assert-True $module.Contains('.digest') 'final validation must use the GitHub artifact digest'
    Assert-True (-not $module.Contains('"$($_):$($Data.Hashes[$_])"')) 'CSV filename:hash payload is forbidden'
}

function Test-BoundedProcessDoesNotDeadlockOrLeakUnboundedOutput {
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
    $context.DeadlineSeconds = 5
    $child = "`$out = 'o' * 131072; `$err = 'e' * 131072; [Console]::Out.Write(`$out); [Console]::Error.Write(`$err)"
    $result = Invoke-BoundedProcess -Context $context -FileName 'pwsh' -ArgumentList @('-NoLogo', '-NoProfile', '-Command', $child)
    Assert-Equal $result.ExitCode 0 'noisy child exit'
    Assert-True ($result.StdOut.Length -le 262144) 'stdout capture must be bounded'
    Assert-True ($result.StdErr.Length -le 262144) 'stderr capture must be bounded'
}

function Test-BoundedProcessKillsHangingChildAtGlobalDeadline {
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
    $context.DeadlineSeconds = 2
    $child = "[Console]::Out.Write('starting'); [Console]::Error.Write('waiting'); Start-Sleep -Seconds 30"
    $watch = [Diagnostics.Stopwatch]::StartNew()
    $timedOut = $false
    try { Invoke-BoundedProcess -Context $context -FileName 'pwsh' -ArgumentList @('-NoLogo', '-NoProfile', '-Command', $child) | Out-Null } catch [System.TimeoutException] { $timedOut = $true }
    $watch.Stop()
    Assert-True $timedOut 'a hanging child must become a deadline timeout'
    Assert-True ($watch.Elapsed.TotalSeconds -lt 8) 'deadline must kill the hanging process tree promptly'
}

function Test-BoundedProcessDeadlineDoesNotWaitForDescendantHeldPipe {
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
    $context.DeadlineSeconds = 2
    $nonce = [guid]::NewGuid().ToString('N')
    $pidPath = [IO.Path]::Combine([IO.Path]::GetTempPath(), "teamkit-bounded-descendant-$nonce.pid")
    $parentPath = [IO.Path]::Combine([IO.Path]::GetTempPath(), "teamkit-bounded-parent-$nonce.pid")
    $markerPath = [IO.Path]::Combine([IO.Path]::GetTempPath(), "teamkit-bounded-descendant-$nonce.marker")
    $descendantPid = 0
    try {
        $grandchildScript = "[IO.File]::WriteAllText('$pidPath', [string]`$PID); [Console]::Out.Write('descendant-open'); Start-Sleep -Milliseconds 3500; [IO.File]::WriteAllText('$markerPath', 'survived')"
        $grandchild = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($grandchildScript))
        $descendantScript = "`$psi = [Diagnostics.ProcessStartInfo]::new(); `$psi.FileName = 'pwsh'; `$psi.UseShellExecute = `$false; `$psi.Arguments = '-NoLogo -NoProfile -EncodedCommand $grandchild'; [Diagnostics.Process]::Start(`$psi) | Out-Null; `$until = [datetime]::UtcNow.AddSeconds(5); while (-not [IO.File]::Exists('$pidPath') -and [datetime]::UtcNow -lt `$until) { Start-Sleep -Milliseconds 10 }"
        $descendant = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($descendantScript))
        $parent = "`$psi = [Diagnostics.ProcessStartInfo]::new(); `$psi.FileName = 'pwsh'; `$psi.UseShellExecute = `$false; `$psi.Arguments = '-NoLogo -NoProfile -EncodedCommand $descendant'; [Diagnostics.Process]::Start(`$psi) | Out-Null; `$until = [datetime]::UtcNow.AddSeconds(5); while (-not [IO.File]::Exists('$pidPath') -and [datetime]::UtcNow -lt `$until) { Start-Sleep -Milliseconds 10 }; [IO.File]::WriteAllText('$parentPath', [string]`$PID); [Environment]::Exit(0)"
        $watch = [Diagnostics.Stopwatch]::StartNew()
        $timedOut = $false
        try { Invoke-BoundedProcess -Context $context -FileName 'pwsh' -ArgumentList @('-NoLogo', '-NoProfile', '-Command', $parent) | Out-Null } catch [System.TimeoutException] { $timedOut = $true }
        $watch.Stop()
        Assert-True $timedOut 'a descendant holding inherited stdout after parent exit must still be bounded by the global deadline'
        Assert-True ($watch.Elapsed.TotalSeconds -lt 3.5) 'descendant-held pipes must not add an unbounded post-exit drain wait'
        for ($attempt = 0; $attempt -lt 20 -and -not [IO.File]::Exists($pidPath); $attempt++) { Start-Sleep -Milliseconds 50 }
        Assert-True ([IO.File]::Exists($pidPath)) 'the descendant process must have started for the process-tree regression'
        $descendantPid = [int][IO.File]::ReadAllText($pidPath)
        $alive = $true
        for ($attempt = 0; $attempt -lt 20 -and $alive; $attempt++) {
            try {
                $descendantProcess = [Diagnostics.Process]::GetProcessById($descendantPid)
                $alive = -not $descendantProcess.HasExited
                $descendantProcess.Dispose()
            } catch { $alive = $false }
            if ($alive) { Start-Sleep -Milliseconds 50 }
        }
        Start-Sleep -Milliseconds 1800
        Assert-True (-not $alive) 'the bounded process deadline must terminate a descendant after its direct parent exited'
        Assert-True (-not [IO.File]::Exists($markerPath)) 'a terminated descendant must not create a delayed marker after publisher return'
    } finally {
        if ($descendantPid -gt 0) {
            try { [Diagnostics.Process]::GetProcessById($descendantPid).Kill($true) } catch { }
        }
        if ([IO.File]::Exists($pidPath)) { [IO.File]::Delete($pidPath) }
        if ([IO.File]::Exists($parentPath)) { [IO.File]::Delete($parentPath) }
        if ([IO.File]::Exists($markerPath)) { [IO.File]::Delete($markerPath) }
    }
}

function Test-RealChildProcessDoesNotReceiveReleaseTokenCanaries {
    $ghCanary = 'release-gh-token-canary-vW7sQ2nK9pL4'
    $glCanary = 'release-gl-token-canary-rT5mX8cJ3hD6'
    $previousGh = $env:GH_TOKEN; $previousGl = $env:GITLAB_TOKEN
    try {
        $env:GH_TOKEN = $ghCanary; $env:GITLAB_TOKEN = $glCanary
        $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken $ghCanary -GitLabToken $glCanary
        $child = "[Console]::Out.Write(('GH=' + `$env:GH_TOKEN + ';GL=' + `$env:GITLAB_TOKEN))"
        $result = Invoke-BoundedProcess -Context $context -FileName 'pwsh' -ArgumentList @('-NoLogo', '-NoProfile', '-Command', $child)
        Assert-Equal $result.StdOut.Trim() 'GH=;GL=' 'a real publisher child process must not inherit either release token'
        Assert-True (-not ($result.StdOut -match [regex]::Escape($ghCanary))) 'real child stdout must not expose the GitHub token canary'
        Assert-True (-not ($result.StdOut -match [regex]::Escape($glCanary))) 'real child stdout must not expose the GitLab token canary'
    } finally {
        $env:GH_TOKEN = $previousGh; $env:GITLAB_TOKEN = $previousGl
    }
}

function Test-WindowsJobLauncherPreservesArgumentListRoundTrip {
    if (-not [TeamKit.Release.WindowsJobProcess]::IsSupported) { return }
    $scriptPath = [IO.Path]::Combine([IO.Path]::GetTempPath(), "teamkit-bounded-argv-$([guid]::NewGuid().ToString('N')).ps1")
    try {
        [IO.File]::WriteAllText($scriptPath, "param([string]`$One, [string]`$Two, [string]`$Three); [Console]::Out.Write((`$One + [Environment]::NewLine + `$Two + [Environment]::NewLine + `$Three))")
        $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
        $result = Invoke-BoundedProcess -Context $context -FileName 'pwsh' -ArgumentList @('-NoLogo', '-NoProfile', '-File', $scriptPath, 'contains space', 'embedded"quote', 'trailing\')
        $received = $result.StdOut.TrimEnd([char[]]@("`r", "`n")) -split "`r?`n"
        Assert-Equal $received.Count 3 'the native launcher must preserve every argument boundary'
        Assert-Equal $received[0] 'contains space' 'the native launcher must preserve a spaced argument'
        Assert-Equal $received[1] 'embedded"quote' 'the native launcher must preserve an embedded quote'
        Assert-Equal $received[2] 'trailing\' 'the native launcher must preserve trailing backslashes'
    } finally {
        if ([IO.File]::Exists($scriptPath)) { [IO.File]::Delete($scriptPath) }
    }
}

function Test-WindowsJobAttachmentFailureNeverResumesChild {
    if (-not [TeamKit.Release.WindowsJobProcess]::IsSupported) { return }
    $markerPath = [IO.Path]::Combine([IO.Path]::GetTempPath(), "teamkit-bounded-attach-$([guid]::NewGuid().ToString('N')).marker")
    try {
        [TeamKit.Release.WindowsJobProcess]::ForceAssignmentFailureForTests = $true
        $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
        $failed = $false
        try { Invoke-BoundedProcess -Context $context -FileName 'pwsh' -ArgumentList @('-NoLogo', '-NoProfile', '-Command', "[IO.File]::WriteAllText('$markerPath', 'ran')") | Out-Null } catch [System.InvalidOperationException] { $failed = $true }
        Assert-True $failed 'a failed Job assignment must fail before the child resumes'
        Start-Sleep -Milliseconds 300
        Assert-True (-not [IO.File]::Exists($markerPath)) 'an uncontained suspended child must never create its marker'
    } finally {
        [TeamKit.Release.WindowsJobProcess]::ForceAssignmentFailureForTests = $false
        if ([IO.File]::Exists($markerPath)) { [IO.File]::Delete($markerPath) }
    }
}

function Test-DefaultProcessFailsClosedOutsideWindowsContainment {
    if ([TeamKit.Release.WindowsJobProcess]::IsSupported) { return }
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
    $reason = ''
    try { Invoke-BoundedProcess -Context $context -FileName 'pwsh' -ArgumentList @('-NoLogo', '-NoProfile', '-Command', 'exit 0') | Out-Null } catch { $reason = $_.Exception.Message }
    Assert-True ($reason -match 'requires Windows process containment') 'non-Windows production process execution must fail closed without tree containment'
}

function Test-ReserveRequiresTheFull1200SecondBudget {
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y -ClockAdapter { param($Context) 0.1 }
    $context.DeadlineSeconds = 1200
    $threw = $false; try { Assert-RemainingBudget -Context $context -MinimumSeconds 1200 | Out-Null } catch { $threw = $true }
    Assert-True $threw 'the tag reserve must reject 1199.9 remaining seconds rather than rounding it up to 1200'
}

function Test-BoundedProcessKillsContinualOutputOverflow {
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
    $context.DeadlineSeconds = 20
    $child = "`$chunk = 'x' * 8192; while (`$true) { [Console]::Out.Write(`$chunk); [Console]::Error.Write(`$chunk) }"
    $watch = [Diagnostics.Stopwatch]::StartNew()
    $overflowed = $false
    try { Invoke-BoundedProcess -Context $context -FileName 'pwsh' -ArgumentList @('-NoLogo', '-NoProfile', '-Command', $child) | Out-Null } catch [System.InvalidOperationException] { $overflowed = $true }
    $watch.Stop()
    Assert-True $overflowed 'continual output must fail as a bounded process overflow, not run until deadline'
    Assert-True ($watch.Elapsed.TotalSeconds -lt 8) 'overflow must terminate the process tree promptly'
}

function Test-PublishDurationUsesInjectedClock {
    $http = { param($Context, $Request) throw 'ordinary transport failure' }
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y -HttpAdapter $http -ClockAdapter { param($Context) 13 }
    $context.DeadlineSeconds = 100
    $result = Publish-TeamKitRelease -Context $context
    Assert-Equal $result.exit_code 1 'ordinary failure expected for duration test'
    Assert-Equal $result.duration_seconds 13 'terminal duration must use the injected global clock'
}

function Test-ContextCarriesTheEntryStopwatchIntoTerminalDuration {
    $entryWatch = [Diagnostics.Stopwatch]::StartNew()
    Start-Sleep -Milliseconds 1100
    $http = { param($Context, $Request) throw 'ordinary transport failure' }
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y -HttpAdapter $http -Stopwatch $entryWatch
    $result = Publish-TeamKitRelease -Context $context
    Assert-True ($result.duration_seconds -ge 1) 'context terminal duration must include work completed before context construction'
}

function Test-DefaultDownloadStreamsAndBoundsCompressedArtifactBytes {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    $start = $module.IndexOf('function Save-ReleaseDownload')
    $end = $module.IndexOf('function Expand-PublicationArchive')
    $download = $module.Substring($start, $end - $start)
    Assert-True $download.Contains('ResponseHeadersRead') 'default download must request headers before streaming bytes'
    Assert-True $download.Contains('ReadAsync') 'default download must stream, not buffer the artifact response'
    Assert-True $download.Contains('MaxPublicationArchiveCompressedBytes') 'default download must cap compressed artifact bytes'
    Assert-True $download.Contains('FileMode]::CreateNew') 'download must not overwrite a pre-existing artifact path'
}

function Test-SignedArtifactDownloadPreservesRedirectAllowanceWithoutAuth {
    $canary = 'release-token-canary-8qN4sV7zK2mP6xR9'
    $seen = [System.Collections.Generic.List[object]]::new()
    $http = {
        param($Context, $Request)
        $seen.Add($Request)
        switch ($Request.Url) {
            'https://artifact.invalid/origin' { return New-RawResponse 302 $null @{ Location = 'https://artifact.invalid/first-hop' } }
            'https://artifact.invalid/first-hop' { return New-RawResponse 302 $null @{ Location = 'https://artifact.invalid/final' } }
            'https://artifact.invalid/final' { return New-RawResponse 200 ([Text.Encoding]::UTF8.GetBytes('artifact-bytes')) }
            default { throw "unexpected download URL $($Request.Url)" }
        }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken $canary -GitLabToken $canary -HttpAdapter $http
    $path = Join-Path ([IO.Path]::GetTempPath()) ("bounded-release-redirect-" + [guid]::NewGuid().ToString('N') + '.zip')
    try {
        & (Get-Module BoundedRelease) {
            param($Context, $Path, $Canary)
            Save-ReleaseDownload $Context 'https://artifact.invalid/origin' $Path @{ Authorization = "Bearer $Canary" } -AllowRedirect | Out-Null
        } $context $path $canary
        Assert-Equal $seen.Count 3 'two signed redirect hops must remain allowed'
        Assert-True $seen[0].Headers.ContainsKey('Authorization') 'origin request must carry its provider authorization'
        Assert-True (-not $seen[1].Headers.ContainsKey('Authorization')) 'first signed redirect must strip provider authorization'
        Assert-True (-not $seen[2].Headers.ContainsKey('Authorization')) 'second signed redirect must keep authorization stripped'
        Assert-Equal ([Text.Encoding]::UTF8.GetString([IO.File]::ReadAllBytes($path))) 'artifact-bytes' 'final signed redirect must write the artifact'
    } finally {
        if (Test-Path -LiteralPath $path) { [IO.File]::Delete($path) }
    }
}

function Test-DefaultPairedCIProbeBoundsBothConcurrentResponses {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    $start = $module.IndexOf('function Get-PairedCIProbe')
    $end = $module.IndexOf('function Save-ReleaseDownload')
    $probe = $module.Substring($start, $end - $start)
    Assert-True $probe.Contains('ResponseHeadersRead') 'default paired CI probe must receive headers before bounded body reads'
    Assert-True $probe.Contains('ReadBoundedAsync') 'default paired CI probe must bound each provider response body'
    Assert-True $probe.Contains('$TimeoutSeconds') 'default paired CI jobs must receive the shared remaining timeout'
    Assert-True $probe.Contains('$failedJobs') 'failed paired CI jobs must be distinguished from unfinished deadline jobs'
    Assert-True $probe.Contains('parallel CI probe failed') 'ordinary paired CI transport failure must not become deadline exit 124'
    Assert-True $probe.Contains('$waitSeconds') 'paired CI coordinator wait must remain capped to the per-request timeout'
    Assert-True $probe.Contains('parallel CI probe failed') 'paired CI failure contract must remain redacted'
}

function Test-DefaultApiAndCIStreamsHaveCancelableDeadlineReads {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    $transportStart = $module.IndexOf('function Invoke-ReleaseTransport')
    $transportEnd = $module.IndexOf('function Invoke-ReleaseHttp')
    $transport = $module.Substring($transportStart, $transportEnd - $transportStart)
    Assert-True $transport.Contains('CancellationTokenSource') 'API transport must create a deadline cancellation source after ResponseHeadersRead'
    Assert-True $transport.Contains('ReadBoundedAsync($stream, $RequestShape.MaxResponseBytes, $cancellation.Token)') 'API body read must receive the deadline cancellation token'
    Assert-True $transport.Contains('catch [System.OperationCanceledException]') 'API transport must classify both cancellation exception shapes'
    Assert-True $transport.Contains('Get-RemainingBudgetSeconds $Context') 'API timeout classification must use the exact global budget'
    $probeStart = $module.IndexOf('function Get-PairedCIProbe')
    $probeEnd = $module.IndexOf('function Save-ReleaseDownload')
    $probe = $module.Substring($probeStart, $probeEnd - $probeStart)
    Assert-True $probe.Contains('ReadBoundedAsync($stream, 1048576, $cancellation.Token)') 'each concurrent CI response body must receive its bounded cancellation token'
}

function Test-DefaultTransportConstructsTheRequestedHttpMethodBeforeNetworkFailure {
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
    $request = [pscustomobject]@{ Method = 'GET'; Url = 'http://127.0.0.1:1/unreachable'; Headers = @{}; BodyUtf8 = [byte[]]@(); TimeoutSeconds = 1; MaxResponseBytes = 1024; Purpose = 'test'; AllowRedirect = $false }
    $failure = $null
    try { & (Get-Module BoundedRelease) { param($Context, $Request) Invoke-ReleaseTransport $Context $Request } $context $request | Out-Null } catch { $failure = $_.Exception }
    Assert-True ($failure -is [System.InvalidOperationException]) 'default transport must construct GET then redact its ordinary loopback transport failure'
    Assert-Equal $failure.Message 'HTTP transport failed' 'default transport must not fail while resolving the requested HTTP method'
}

function Test-GitMutationsDisableGlobalSigningPrompts {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    Assert-True $module.Contains("@('-c', 'tag.gpgSign=false', 'tag'") 'annotated tag creation must override global tag signing'
    Assert-True $module.Contains("@('-c', 'push.gpgSign=false', 'push'") 'every branch/tag push must override global push signing'
}

function Test-PreflightUsesBoundedNonForceGitDryRunProbes {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    Assert-True $module.Contains("@('-c', 'push.gpgSign=false', 'push', '--dry-run'") 'preflight must probe each configured Git credential helper with bounded non-force dry-run pushes'
}

function Test-ReadOnlyApiAuthorityFailsClosedWithoutClassicGitHubRepoScope {
    $requests = [System.Collections.Generic.List[object]]::new()
    $http = {
        param($Context, $Request)
        $requests.Add($Request)
        if ($Request.Url -match '/repos/mi1man-cmd/kit-all-team$') {
            return New-RawResponse 200 @{ full_name = 'mi1man-cmd/kit-all-team'; private = $true; permissions = @{ push = $true } } @{}
        }
        throw "unexpected authority request $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y -HttpAdapter $http
    $reason = ''; try { & (Get-Module BoundedRelease) { param($Context) Assert-ReadOnlyApiAuthority $Context | Out-Null } $context } catch { $reason = $_.Exception.Message }
    Assert-Equal $reason 'GitHub API authority probe lacks classic repo scope' 'an absent/unknown GitHub scope header must fail closed before any release mutation'
    Assert-Equal @($requests | Where-Object { $_.Method -eq 'POST' }).Count 0 'read-only authority proof must never send an API mutation'
}

function Test-ReadOnlyApiAuthorityAcceptsDocumentedGitHubAndGitLabProofs {
    $requests = [System.Collections.Generic.List[object]]::new()
    $http = {
        param($Context, $Request)
        $requests.Add($Request)
        if ($Request.Url -match '/repos/mi1man-cmd/kit-all-team$') { return New-RawResponse 200 @{ full_name = 'mi1man-cmd/kit-all-team'; private = $true; permissions = @{ push = $true } } @{ 'X-OAuth-Scopes' = 'repo, workflow' } }
        if ($Request.Url -match '/actions/workflows/release\.yml$') { return New-RawResponse 200 @{ path = '.github/workflows/release.yml'; state = 'active' } }
        if ($Request.Url -match '/actions/workflows/ci\.yml$') { return New-RawResponse 200 @{ path = '.github/workflows/ci.yml'; state = 'active' } }
        if ($Request.Url -match '/personal_access_tokens/self$') { return New-RawResponse 200 @{ user_id = 99; active = $true; revoked = $false; expires_at = $null; scopes = @('api') } }
        if ($Request.Url -match '/api/v4/user$') { return New-RawResponse 200 @{ id = 99 } }
        if ($Request.Url -match '/projects/12087$') { return New-RawResponse 200 @{ id = 12087; path_with_namespace = '1c/aisuz/ai'; archived = $false; permissions = @{ project_access = @{ access_level = 40 }; group_access = $null } } }
        throw "unexpected authority request $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y -HttpAdapter $http
    & (Get-Module BoundedRelease) { param($Context) Assert-ReadOnlyApiAuthority $Context | Out-Null } $context
    Assert-Equal @($requests | Where-Object { $_.Method -ne 'GET' }).Count 0 'documented API authority proof must stay read-only'
}

function Test-PreflightStopsBeforeGitDryRunWhenApiAuthorityIsUnproven {
    $candidate = 'b' * 40
    $state = @{ DryRuns = 0; ApiPosts = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -eq 'POST') { $state.ApiPosts++; throw 'API mutation must not be reached' }
        if ($Request.Url -match '/repos/mi1man-cmd/kit-all-team$') { return New-RawResponse 200 @{ full_name = 'mi1man-cmd/kit-all-team'; private = $true; permissions = @{ push = $true } } @{} }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/projects/12087$') { return New-RawResponse 200 @{ id = 12087; path_with_namespace = '1c/aisuz/ai'; archived = $false; permissions = @{ project_access = @{ access_level = 40 } } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$' -or $Request.Url -match '/protected_tags/v0\.1\.3$' -or $Request.Url -match '/releases/v0\.1\.3$') { return New-RawResponse 404 }
        throw "unexpected preflight authority HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $operation = if ($FileName -eq 'git' -and $Arguments[0] -eq '-c') { $Arguments[2] } elseif ($FileName -eq 'git') { $Arguments[0] } else { '' }
        if ($FileName -eq 'git' -and $operation -eq 'push' -and $Arguments -contains '--dry-run') { $state.DryRuns++; throw 'Git dry-run must not be reached' }
        if ($FileName -eq 'git' -and $operation -eq 'rev-parse') { return [pscustomobject]@{ ExitCode = 0; StdOut = $candidate; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'show') { return [pscustomobject]@{ ExitCode = 0; StdOut = '2026-08-18T00:00:00Z'; StdErr = '' } }
        if ($FileName -notin @('git', 'go')) { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
    $result = Publish-TeamKitRelease -Context $context
    Assert-Equal $result.exit_code 1 'unproven API authority must be an ordinary preflight failure'
    Assert-Equal $state.DryRuns 0 'unproven API authority must stop before either configured Git credential probe'
    Assert-Equal $state.ApiPosts 0 'unproven API authority must stop before dispatch, Keep, tag protection, or Release mutation'
}

function Test-PreflightRejectsMissingOrDisabledCandidateCIWorkflowBeforeMutation {
    $candidate = 'b' * 40
    foreach ($workflowCase in @(@{ Name = 'missing'; StatusCode = 404; Value = $null; Expected = 'GitHub API authority probe not-found' }, @{ Name = 'disabled'; StatusCode = 200; Value = @{ path = '.github/workflows/ci.yml'; state = 'disabled' }; Expected = 'GitHub API authority probe CI workflow mismatch' })) {
        $state = @{ DryRuns = 0; ApiPosts = 0; CIWorkflowReads = 0; GitLabAuthorityReads = 0 }
        $http = {
            param($Context, $Request)
            if ($Request.Method -eq 'POST') { $state.ApiPosts++; throw 'API mutation must not be reached' }
            if ($Request.Url -match '/repos/mi1man-cmd/kit-all-team$') { return New-RawResponse 200 @{ full_name = 'mi1man-cmd/kit-all-team'; private = $true; permissions = @{ push = $true } } @{ 'X-OAuth-Scopes' = 'repo' } }
            if ($Request.Url -match '/actions/workflows/release\.yml$') { return New-RawResponse 200 @{ path = '.github/workflows/release.yml'; state = 'active' } }
            if ($Request.Url -match '/actions/workflows/ci\.yml$') { $state.CIWorkflowReads++; return New-RawResponse $workflowCase.StatusCode $workflowCase.Value }
            if ($Request.Url -match '/personal_access_tokens/self$') { $state.GitLabAuthorityReads++; throw 'GitLab authority must not be reached after invalid candidate workflow' }
            throw "unexpected candidate-workflow preflight HTTP $($Request.Method) $($Request.Url)"
        }.GetNewClosure()
        $process = {
            param($Context, $FileName, $Arguments, $TimeoutSeconds)
            $operation = if ($FileName -eq 'git' -and $Arguments[0] -eq '-c') { $Arguments[2] } elseif ($FileName -eq 'git') { $Arguments[0] } else { '' }
            if ($FileName -eq 'git' -and $operation -eq 'push' -and $Arguments -contains '--dry-run') { $state.DryRuns++; throw 'Git dry-run must not be reached' }
            if ($FileName -eq 'git' -and $operation -eq 'rev-parse') { return [pscustomobject]@{ ExitCode = 0; StdOut = $candidate; StdErr = '' } }
            if ($FileName -eq 'git' -and $operation -eq 'show') { return [pscustomobject]@{ ExitCode = 0; StdOut = '2026-08-18T00:00:00Z'; StdErr = '' } }
            if ($FileName -notin @('git', 'go')) { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
            return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' }
        }.GetNewClosure()
        $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
        $reason = ''; try { Invoke-ReleasePreflight -Context $context | Out-Null } catch { $reason = $_.Exception.Message }
        Assert-Equal $reason $workflowCase.Expected "$($workflowCase.Name) ci.yml must be rejected by the read-only authority proof"
        Assert-Equal $state.CIWorkflowReads 1 "$($workflowCase.Name) ci.yml must be read exactly once before mutation"
        Assert-Equal $state.GitLabAuthorityReads 0 "$($workflowCase.Name) ci.yml must stop before later authority probes"
        Assert-Equal $state.DryRuns 0 "$($workflowCase.Name) ci.yml must stop before both Git dry-run probes"
        Assert-Equal $state.ApiPosts 0 "$($workflowCase.Name) ci.yml must stop before every API mutation"
    }
}

function Test-PreflightRejectsWrongLocalAnnotatedTagBeforeAnyRemoteMutation {
    $candidate = 'b' * 40
    $bytes = [Text.Encoding]::UTF8.GetBytes('local-tag-preflight-upload')
    $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
    $uploads = @(@{ Name = 'fixture.bin'; Url = 'https://fixture.invalid/local-tag'; ApiUrl = 'https://fixture.invalid/local-tag'; Size = $bytes.Length; Sha256 = $hash })
    $state = @{ DryRuns = 0; ApiPosts = 0; LocalTagChecks = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -eq 'POST') { $state.ApiPosts++; throw 'remote mutation must not be reached' }
        if ($Request.Purpose -eq 'download') { return New-RawResponse 200 $bytes }
        if ($Request.Url -match '/repos/mi1man-cmd/kit-all-team$') { return New-RawResponse 200 @{ full_name = 'mi1man-cmd/kit-all-team'; private = $true; permissions = @{ push = $true } } @{ 'X-OAuth-Scopes' = 'repo' } }
        if ($Request.Url -match '/actions/workflows/release\.yml$') { return New-RawResponse 200 @{ path = '.github/workflows/release.yml'; state = 'active' } }
        if ($Request.Url -match '/actions/workflows/ci\.yml$') { return New-RawResponse 200 @{ path = '.github/workflows/ci.yml'; state = 'active' } }
        if ($Request.Url -match '/personal_access_tokens/self$') { return New-RawResponse 200 @{ user_id = 99; active = $true; revoked = $false; expires_at = $null; scopes = @('api') } }
        if ($Request.Url -match '/api/v4/user$') { return New-RawResponse 200 @{ id = 99 } }
        if ($Request.Url -match '/projects/12087$') { return New-RawResponse 200 @{ id = 12087; path_with_namespace = '1c/aisuz/ai'; archived = $false; permissions = @{ project_access = @{ access_level = 40 }; group_access = $null } } }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$' -or $Request.Url -match '/protected_tags/v0\.1\.3$' -or $Request.Url -match '/releases/v0\.1\.3$') { return New-RawResponse 404 }
        throw "unexpected local-tag preflight HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $operation = if ($FileName -eq 'git' -and $Arguments[0] -eq '-c') { $Arguments[2] } elseif ($FileName -eq 'git') { $Arguments[0] } else { '' }
        if ($FileName -eq 'git' -and $operation -eq 'for-each-ref') {
            $state.LocalTagChecks++
            if (($Arguments -join ' ') -match 'contents') { return [pscustomobject]@{ ExitCode = 0; StdOut = 'wrong local message'; StdErr = '' } }
            return [pscustomobject]@{ ExitCode = 0; StdOut = ('a' * 40); StdErr = '' }
        }
        if ($FileName -eq 'git' -and $operation -eq 'rev-parse') { return [pscustomobject]@{ ExitCode = 0; StdOut = if ($Arguments[-1] -eq 'HEAD') { $candidate } elseif ($Arguments[-1] -like '*^{tag}') { 'a' * 40 } else { $candidate }; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'cat-file') { return [pscustomobject]@{ ExitCode = 0; StdOut = 'tag'; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'push' -and $Arguments -contains '--dry-run') { $state.DryRuns++; return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -in @('status', 'fetch', 'merge-base')) { return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'show') { return [pscustomobject]@{ ExitCode = 0; StdOut = '2026-08-18T00:00:00Z'; StdErr = '' } }
        if ($FileName -notin @('git', 'go')) { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process -UploadFiles $uploads
    $threw = $false
    try { Invoke-ReleasePreflight -Context $context | Out-Null } catch { $threw = $true }
    Assert-True $threw 'a wrong local v0.1.3 tag must reject preflight'
    Assert-True ($state.LocalTagChecks -gt 0) 'preflight must inspect the local v0.1.3 tag identity'
    Assert-Equal $state.DryRuns 0 'a wrong local tag must stop before either remote Git credential probe'
    Assert-Equal $state.ApiPosts 0 'a wrong local tag must stop before every API mutation'
}

function Test-ProductionCanarySinksAreNarrow {
    $canary = 'release-token-canary-8qN4sV7zK2mP6xR9'
    $candidate = 'b' * 40
    $seen = [System.Collections.Generic.List[string]]::new()
    $processObserved = [System.Collections.Generic.List[string]]::new()
    $headerState = @{ CanaryWasOnlyInHeaders = $false }
    $http = {
        param($Context, $Request)
        $body = if ($null -eq $Request.BodyUtf8) { '' } else { [Text.Encoding]::UTF8.GetString($Request.BodyUtf8) }
        if (@($Request.Headers.Values | Where-Object { $_ -match [regex]::Escape($canary) }).Count -gt 0) { $headerState.CanaryWasOnlyInHeaders = $true }
        [void]$seen.Add($Request.Url + "`n" + $body + "`nheaders=" + ($Request.Headers.Keys -join ','))
        throw 'simulated DNS failure'
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $stdout = if ($FileName -eq 'git' -and $Arguments[0] -eq 'rev-parse') { $candidate } elseif ($FileName -notin @('git', 'go')) { "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}" } else { '' }
        $stderr = 'process stderr without secret'
        [void]$processObserved.Add($FileName + "`n" + ($Arguments -join "`n") + "`nstdout=" + $stdout + "`nstderr=" + $stderr)
        return [pscustomobject]@{ ExitCode = 0; StdOut = $stdout; StdErr = $stderr }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken $canary -GitLabToken $canary -HttpAdapter $http -ProcessAdapter $process
    $result = Publish-TeamKitRelease -Context $context
    $observable = ($context.Events -join "`n") + "`n" + ($result | ConvertTo-Json -Depth 8 -Compress) + "`n" + ($seen -join "`n") + "`n" + ($processObserved -join "`n")
    Assert-True (-not $observable.Contains($canary)) 'canary must not reach events or result'
    Assert-True $headerState.CanaryWasOnlyInHeaders 'production HTTP seam must carry authentication only in-memory headers'
    Assert-True ($seen.Count -gt 0) ("production HTTP seam must execute events=" + ($context.Events -join ','))
    Assert-Equal $result.exit_code 1 'ordinary transport failure must not become deadline exit 124'
}

function Test-ProtectedTagRuleRejectsAnyAdditionalCreator {
    $candidate = 'b' * 40
    $tagCalls = [System.Collections.Generic.List[string]]::new()
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/protected_tags/v0\.1\.3$') {
            return @{ name = 'v0.1.3'; create_access_levels = @(@{ access_level = 40 }, @{ access_level = 30 }) }
        }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$') { throw 'HTTP operation failed status=404' }
        throw "unexpected HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $tagCalls.Add("$FileName $($Arguments -join ' ')")
        return [pscustomobject]@{ ExitCode = 0; StdOut = $Context.CandidateSha; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
    try { Publish-ProtectedTag -Context $context -CI @{} | Out-Null } catch { }
    Assert-Equal $tagCalls.Count 0 'invalid protected rule must prevent any tag mutation'
}

function Test-HttpSeamReceivesSerializedNestedPayload {
    $captured = [System.Collections.Generic.List[object]]::new()
    $http = {
        param($Context, $Request)
        $captured.Add($Request)
        return @{ StatusCode = 200; Headers = @{}; BodyUtf8 = [Text.Encoding]::UTF8.GetBytes('{}') }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y -HttpAdapter $http
    Invoke-GitHubApi -Context $context -Method POST -Path '/test' -Body @{ inputs = @{ nested = @{ value = 'preserved' } } } | Out-Null
    Assert-Equal $captured.Count 1 'one transport request'
    Assert-True ($null -ne $captured[0].BodyUtf8) 'transport seam must receive serialized body bytes'
    $body = [Text.Encoding]::UTF8.GetString($captured[0].BodyUtf8) | ConvertFrom-Json -AsHashtable
    Assert-Equal $body.inputs.nested.value 'preserved' 'nested JSON payload'
}

function Test-EntryWritesOneJsonWhenOutputPathIsUnwritable {
    $entry = Join-Path $PSScriptRoot '..\publish-v0.1.3.ps1'
    $lines = @(& pwsh -NoLogo -NoProfile -File $entry -CandidateSha ('b' * 40) -OutputPath 'Z:\not-writable\release.json' 2>&1)
    Assert-Equal $lines.Count 1 'entry must emit one stdout JSON object'
    $result = $lines[0] | ConvertFrom-Json -AsHashtable
    Assert-Equal $result.status 'failed' 'missing-token result remains redacted JSON'
}

function Test-EntryNeverPromptsAndAlwaysEmitsOneJsonForInvalidArguments {
    $entry = Join-Path $PSScriptRoot '..\publish-v0.1.3.ps1'
    $cases = @(
        @{ Name = 'missing candidate'; Arguments = @() },
        @{ Name = 'bad candidate'; Arguments = @('-CandidateSha', 'not-a-sha') },
        @{ Name = 'bad version'; Arguments = @('-CandidateSha', ('b' * 40), '-Version', 'v9.9.9') },
        @{ Name = 'bad maximum'; Arguments = @('-CandidateSha', ('b' * 40), '-MaxMinutes', '0') },
        @{ Name = 'unwritable output'; Arguments = @('-CandidateSha', ('b' * 40), '-OutputPath', 'Z:\not-writable\release.json') }
    )
    foreach ($case in $cases) {
        [string[]]$arguments = @($case.Arguments)
        $lines = @(& pwsh -NoLogo -NoProfile -File $entry @arguments 2>&1)
        Assert-Equal $lines.Count 1 "$($case.Name) must emit exactly one terminal JSON line"
        $result = $lines[0] | ConvertFrom-Json -AsHashtable
        Assert-Equal $result.exit_code 1 "$($case.Name) must fail safely"
        Assert-True ($lines[0] -notmatch 'Supply values|Enter values|Cannot process command') "$($case.Name) must not enter PowerShell parameter prompting"
    }
}

function Test-ProductionFixtureCanUseVerifiedFixedUploadInputs {
    $bytes = [Text.Encoding]::UTF8.GetBytes('fixture-upload')
    $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
    $uploads = @(@{ Name = 'fixture.bin'; Url = 'https://fixture.invalid/fixed'; ApiUrl = 'https://fixture.invalid/fixed'; Size = $bytes.Length; Sha256 = $hash })
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y -UploadFiles $uploads
    Assert-Equal $context.UploadFiles[0].Sha256 $hash 'fixture upload configuration'
}

function Test-FixedUploadsUseAuthenticatedGitLabApiButKeepBrowserReleaseLinks {
    $bytes = [Text.Encoding]::UTF8.GetBytes('fixed-upload-api-fixture')
    $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
    $browserUrl = 'https://gitlab.example.invalid/-/project/12087/uploads/0123456789abcdef0123456789abcdef/fixture.bin'
    $seen = [System.Collections.Generic.List[object]]::new()
    $http = {
        param($Context, $Request)
        $seen.Add($Request)
        if ($Request.Url -match '/api/v4/projects/12087/uploads/') {
            return New-RawResponse 302 $null @{ Location = 'https://object-storage.invalid/fixed-upload' }
        }
        if ($Request.Url -eq 'https://object-storage.invalid/fixed-upload') {
            return New-RawResponse 200 $bytes
        }
        throw "unexpected fixed-upload request $($Request.Url)"
    }.GetNewClosure()
    $uploads = @(@{ Name = 'fixture.bin'; Url = $browserUrl; Size = $bytes.Length; Sha256 = $hash })
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken 'release-token-canary-8qN4sV7zK2mP6xR9' -HttpAdapter $http -UploadFiles $uploads
    $records = & (Get-Module BoundedRelease) { param($Context) Test-FixedUploadSet $Context } $context
    Assert-Equal $seen.Count 2 'fixed upload must follow one bounded signed redirect'
    Assert-Equal $seen[0].Url 'https://gitlab.example.invalid/api/v4/projects/12087/uploads/0123456789abcdef0123456789abcdef/fixture.bin' 'fixed upload must not use the browser session route'
    Assert-True ($seen[0].Headers.ContainsKey('PRIVATE-TOKEN')) 'fixed upload API request must retain the in-memory GitLab token header'
    Assert-True (-not $seen[1].Headers.ContainsKey('PRIVATE-TOKEN')) 'fixed upload signed redirect must not receive the GitLab token header'
    Assert-Equal $records.Files[0].url $browserUrl 'Release asset links must retain the approved browser URL'
}

function Test-GitLabArtifactDownloadsPermitSafeSignedRedirects {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    $start = $module.IndexOf('function Get-ProductionPublicationSets')
    $end = $module.IndexOf('function Invoke-ProductionReleaseStep')
    $body = $module.Substring($start, $end - $start)
    $gitLabArtifactLine = @($body -split "`r?`n" | Where-Object { $_ -match '/jobs/\$\(\$CI\.gitlab_job_id\)/artifacts' })
    Assert-Equal $gitLabArtifactLine.Count 1 'production helper must have one GitLab job-artifact download'
    Assert-True $gitLabArtifactLine[0].Contains('-AllowRedirect') 'GitLab job artifact downloads must follow bounded signed redirects with stripped recursive headers'
}

function New-RawResponse([int]$StatusCode, $Value = $null, [hashtable]$Headers = @{}) {
    $bytes = if ($null -eq $Value) { [byte[]]@() } elseif ($Value -is [byte[]]) { $Value } else { [Text.Encoding]::UTF8.GetBytes(($Value | ConvertTo-Json -Depth 8 -Compress)) }
    return [pscustomobject]@{ StatusCode = $StatusCode; Headers = $Headers; BodyUtf8 = $bytes }
}

function Test-SignedArtifactDownloadRetriesTemporaryForbiddenSignedTarget {
    $canary = 'release-token-canary-8qN4sV7zK2mP6xR9'
    $seen = [System.Collections.Generic.List[object]]::new()
    $state = @{ OriginRequests = 0; Sleeps = [System.Collections.Generic.List[int]]::new() }
    $http = {
        param($Context, $Request)
        $seen.Add($Request)
        switch ($Request.Url) {
            'https://artifact.invalid/origin' {
                $state.OriginRequests++
                $target = switch ($state.OriginRequests) {
                    1 { 'https://artifact.invalid/not-yet-valid-first' }
                    2 { 'https://artifact.invalid/not-yet-valid-second' }
                    default { 'https://artifact.invalid/final' }
                }
                return New-RawResponse 302 $null @{ Location = $target }
            }
            'https://artifact.invalid/not-yet-valid-first' { return New-RawResponse 403 }
            'https://artifact.invalid/not-yet-valid-second' { return New-RawResponse 403 }
            'https://artifact.invalid/final' { return New-RawResponse 200 ([Text.Encoding]::UTF8.GetBytes('artifact-bytes')) }
            default { throw "unexpected download URL $($Request.Url)" }
        }
    }.GetNewClosure()
    $sleep = { param($Context, $Seconds) $state.Sleeps.Add($Seconds) }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken $canary -GitLabToken $canary -HttpAdapter $http -SleepAdapter $sleep
    $path = Join-Path ([IO.Path]::GetTempPath()) ("bounded-release-forbidden-retry-" + [guid]::NewGuid().ToString('N') + '.zip')
    try {
        & (Get-Module BoundedRelease) {
            param($Context, $Path, $Canary)
            Save-ReleaseDownload $Context 'https://artifact.invalid/origin' $Path @{ Authorization = "Bearer $Canary" } -AllowRedirect | Out-Null
        } $context $path $canary
        Assert-Equal $state.OriginRequests 3 'two temporary signed-target 403 responses must re-request the authorized origin twice'
        Assert-Equal $seen.Count 6 'retry must make three origin and three signed-target requests'
        Assert-Equal $state.Sleeps.Count 2 'each temporary signed-target 403 must wait before re-requesting the origin'
        Assert-Equal $state.Sleeps[0] 10 'temporary signed-target retry must use the bounded wait'
        Assert-Equal $state.Sleeps[1] 10 'temporary signed-target retry must use the bounded wait'
        Assert-True $seen[0].Headers.ContainsKey('Authorization') 'initial origin request must carry provider authorization'
        Assert-True (-not $seen[1].Headers.ContainsKey('Authorization')) 'initial signed target must not receive provider authorization'
        Assert-True $seen[2].Headers.ContainsKey('Authorization') 'retry origin request must restore provider authorization'
        Assert-True (-not $seen[3].Headers.ContainsKey('Authorization')) 'replacement signed target must not receive provider authorization'
        Assert-True $seen[4].Headers.ContainsKey('Authorization') 'second retry origin request must restore provider authorization'
        Assert-True (-not $seen[5].Headers.ContainsKey('Authorization')) 'second replacement signed target must not receive provider authorization'
        Assert-Equal ([Text.Encoding]::UTF8.GetString([IO.File]::ReadAllBytes($path))) 'artifact-bytes' 'retry must write the final artifact'
    } finally {
        if (Test-Path -LiteralPath $path) { [IO.File]::Delete($path) }
    }
}

function Test-SignedArtifactDownloadWaitsForSignedTargetNotBeforeTime {
    $canary = 'release-token-canary-8qN4sV7zK2mP6xR9'
    $start = [datetime]'2026-08-22T15:17:25Z'
    $state = @{ Now = [datetime]'2026-08-22T15:17:00Z'; Sleeps = [System.Collections.Generic.List[int]]::new(); Requests = [System.Collections.Generic.List[object]]::new() }
    $http = {
        param($Context, $Request)
        $state.Requests.Add($Request)
        switch -Regex ($Request.Url) {
            '^https://artifact\.invalid/origin$' { return New-RawResponse 302 $null @{ Location = 'https://artifact.invalid/signed?st=2026-08-22T15%3A17%3A25Z' } }
            '^https://artifact\.invalid/signed\?st=' {
                if ($state.Now -lt $start) { return New-RawResponse 403 }
                return New-RawResponse 200 ([Text.Encoding]::UTF8.GetBytes('artifact-bytes'))
            }
            default { throw "unexpected download URL $($Request.Url)" }
        }
    }.GetNewClosure()
    $sleep = { param($Context, $Seconds) $state.Sleeps.Add($Seconds); $state.Now = $state.Now.AddSeconds($Seconds) }.GetNewClosure()
    $now = { param($Context) $state.Now }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken $canary -GitLabToken $canary -HttpAdapter $http -SleepAdapter $sleep -UtcNowAdapter $now
    $path = Join-Path ([IO.Path]::GetTempPath()) ("bounded-release-not-before-" + [guid]::NewGuid().ToString('N') + '.zip')
    try {
        & (Get-Module BoundedRelease) {
            param($Context, $Path, $Canary)
            Save-ReleaseDownload $Context 'https://artifact.invalid/origin' $Path @{ Authorization = "Bearer $Canary" } -AllowRedirect | Out-Null
        } $context $path $canary
        Assert-Equal $state.Sleeps.Count 3 'signed target must wait until its advertised not-before time'
        Assert-Equal $state.Sleeps[0] 10 'not-before wait must stay bounded'
        Assert-Equal $state.Sleeps[1] 10 'not-before wait must stay bounded'
        Assert-Equal $state.Sleeps[2] 5 'not-before wait must stop exactly at the signed start time'
        Assert-Equal $state.Requests.Count 2 'not-before wait must avoid failed signed-target requests'
        Assert-True $state.Requests[0].Headers.ContainsKey('Authorization') 'provider origin request must keep authorization'
        Assert-True (-not $state.Requests[1].Headers.ContainsKey('Authorization')) 'signed storage request must not receive provider authorization'
        Assert-Equal ([Text.Encoding]::UTF8.GetString([IO.File]::ReadAllBytes($path))) 'artifact-bytes' 'signed target must download after its not-before time'
    } finally {
        if (Test-Path -LiteralPath $path) { [IO.File]::Delete($path) }
    }
}

function global:New-JsonArrayResponse([object[]]$Values, [hashtable]$Headers = @{}) {
    [object[]]$items = @()
    if ($null -ne $Values) { $items = @($Values) }
    $json = if ($items.Count -eq 0) { '[]' } else { ConvertTo-Json -InputObject $items -Depth 8 -Compress }
    return [pscustomobject]@{ StatusCode = 200; Headers = $Headers; BodyUtf8 = [Text.Encoding]::UTF8.GetBytes($json) }
}

function New-PublicationZip([string]$Candidate, [string]$WindowsContent = 'windows fixture', [ValidateSet('dist', 'root')][string]$Layout = 'dist', [switch]$IncludeDistDirectory, [string]$Version = 'v0.1.3') {
    $contents = @{
        "teamkit-$Version-windows-amd64.exe" = $WindowsContent
        "teamkit-$Version-linux-amd64" = 'linux fixture'
        "teamkit-$Version-darwin-amd64" = 'darwin intel fixture'
        "teamkit-$Version-darwin-arm64" = 'darwin arm fixture'
    }
    $hashes = @{}
    foreach ($name in @($contents.Keys)) { $hashes[$name] = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($contents[$name]))).ToLowerInvariant() }
    $manifest = ($contents.Keys | Sort-Object | ForEach-Object { "$($hashes[$_])  $_" }) -join "`n"
    $contents['SHA256SUMS'] = $manifest + "`n"
    $contents['SECURITY-AUDIT.json'] = (@{ commit = $Candidate; passed = $true; findings = @() } | ConvertTo-Json -Compress)
    $memory = [IO.MemoryStream]::new()
    $zip = [IO.Compression.ZipArchive]::new($memory, [IO.Compression.ZipArchiveMode]::Create, $true)
    if ($Layout -eq 'dist' -and $IncludeDistDirectory) { $zip.CreateEntry('dist/') | Out-Null }
    foreach ($name in $contents.Keys) {
        $entryName = if ($Layout -eq 'dist') { "dist/$name" } else { $name }
        $entry = $zip.CreateEntry($entryName)
        $writer = [IO.StreamWriter]::new($entry.Open(), [Text.UTF8Encoding]::new($false))
        $writer.Write($contents[$name]); $writer.Dispose()
    }
    $zip.Dispose()
    $allHashes = @{}
    foreach ($name in $contents.Keys) { $allHashes[$name] = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($contents[$name]))).ToLowerInvariant() }
    return @{ Bytes = $memory.ToArray(); Hashes = $hashes; FullHashes = $allHashes }
}

function Test-GitHubCandidateArchiveUsesRootFileLayout {
    $candidate = 'b' * 40
    $publication = New-PublicationZip $candidate 'windows fixture' root
    $path = Join-Path ([IO.Path]::GetTempPath()) ("bounded-release-github-root-" + [guid]::NewGuid().ToString('N') + '.zip')
    [IO.File]::WriteAllBytes($path, $publication.Bytes)
    try {
        $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y
        $files = & (Get-Module BoundedRelease) { param($Context, $Archive) Expand-PublicationArchive $Context $Archive 'GitHub' -Layout root } $context $path
        Assert-Equal @($files.Keys).Count 6 'GitHub root archive must yield the six publication files'
        Assert-True (Test-Path -LiteralPath $files['teamkit-v0.1.3-windows-amd64.exe']) 'GitHub root archive must extract the Windows candidate'
    } finally {
        if (Test-Path -LiteralPath $path) { [IO.File]::Delete($path) }
    }
}

function Test-GitLabArchiveAcceptsSafeDistDirectoryMarker {
    $candidate = 'b' * 40
    $publication = New-PublicationZip $candidate 'windows fixture' dist -IncludeDistDirectory
    $path = Join-Path ([IO.Path]::GetTempPath()) ("bounded-release-gitlab-directory-" + [guid]::NewGuid().ToString('N') + '.zip')
    [IO.File]::WriteAllBytes($path, $publication.Bytes)
    try {
        $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y
        $files = & (Get-Module BoundedRelease) { param($Context, $Archive) Expand-PublicationArchive $Context $Archive 'GitLab' } $context $path
        Assert-Equal @($files.Keys).Count 6 'a harmless dist/ directory marker must not count as a publication file'
        Assert-True (Test-Path -LiteralPath $files['SHA256SUMS']) 'safe directory marker archive must still extract every required regular file'
    } finally {
        if (Test-Path -LiteralPath $path) { [IO.File]::Delete($path) }
    }
}

function Test-GitLabExecutableWaitsForGitHubSixFileBinding {
    $candidate = 'b' * 40
    $gitlabPublication = New-PublicationZip $candidate 'untrusted GitLab executable bytes'
    $githubPublication = New-PublicationZip $candidate 'trusted GitHub executable bytes' root
    $state = @{ ExecutableCalls = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download' -and $Request.Url -match '/jobs/3/artifacts$') { return New-RawResponse 200 $gitlabPublication.Bytes }
        if ($Request.Purpose -eq 'download' -and $Request.Url -eq 'https://fixture.invalid/github.zip') { return New-RawResponse 200 $githubPublication.Bytes }
        if ($Request.Url -match '/actions/runs/1/artifacts') { return New-RawResponse 200 @{ artifacts = @(@{ name = 'candidate-binaries'; expired = $false; digest = ('sha256:' + ('a' * 64)); archive_download_url = 'https://fixture.invalid/github.zip' }) } }
        throw "unexpected comparison HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        if ($FileName -like '*teamkit-v0.1.3-windows-amd64.exe') { $state.ExecutableCalls++; return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        throw "unexpected process $FileName"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
    $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3 }
    $threw = $false
    try { & (Get-Module BoundedRelease) { param($Context, $CI) Get-ProductionPublicationSets $Context $CI | Out-Null } $context $ci } catch { $threw = $true }
    Assert-True $threw 'mismatched GitLab and GitHub publication sets must fail'
    Assert-Equal $state.ExecutableCalls 0 'unbound GitLab executable must not be launched before six-file comparison'
}

function Invoke-ProductionSuccessFixture {
    param([switch]$ExistingTag, [string]$FailAt)
    $candidate = 'b' * 40
    $publication = New-PublicationZip $candidate
    $githubPublication = New-PublicationZip $candidate 'windows fixture' root
    $uploadBytes = [Text.Encoding]::UTF8.GetBytes('fixture-upload-one')
    $uploadTwoBytes = [Text.Encoding]::UTF8.GetBytes('fixture-upload-two')
    $uploadHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadBytes)).ToLowerInvariant()
    $uploadTwoHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadTwoBytes)).ToLowerInvariant()
    $uploads = @(
        @{ Name = 'fixture-one.bin'; Url = 'https://fixture.invalid/fixed-one'; ApiUrl = 'https://fixture.invalid/fixed-one'; Size = $uploadBytes.Length; Sha256 = $uploadHash },
        @{ Name = 'fixture-two.bin'; Url = 'https://fixture.invalid/fixed-two'; ApiUrl = 'https://fixture.invalid/fixed-two'; Size = $uploadTwoBytes.Length; Sha256 = $uploadTwoHash }
    )
    $mutations = [System.Collections.Generic.List[string]]::new()
    $processCalls = [System.Collections.Generic.List[string]]::new()
    $state = @{ Protected = [bool]$ExistingTag; TagExists = [bool]$ExistingTag; LocalTagExists = $false; ReleaseExists = $false; FinalDispatched = $false; GitHubArtifactReads = 0; GitHubAuthorityReads = 0; GitLabAuthorityReads = 0 }
    $http = {
        param($Context, $Request)
        $url = $Request.Url
        if ($Request.Purpose -eq 'download') {
            if ($url -eq 'https://fixture.invalid/fixed-one') { return New-RawResponse 200 $uploadBytes }
            if ($url -eq 'https://fixture.invalid/fixed-two') { return New-RawResponse 200 $uploadTwoBytes }
            if ($url -eq 'https://fixture.invalid/github.zip') { return New-RawResponse 200 $githubPublication.Bytes }
            return New-RawResponse 200 $publication.Bytes
        }
        if ($url -match '/actions/workflows/ci\.yml/runs') { return New-RawResponse 200 @{ workflow_runs = @() } }
        if ($url -match '/projects/12087/pipelines\?sha=') { return [pscustomobject]@{ StatusCode = 200; Headers = @{}; BodyUtf8 = [Text.Encoding]::UTF8.GetBytes('[]') } }
        if ($url -match '/workflows/ci\.yml/dispatches$' -and $Request.Method -eq 'POST') { $mutations.Add('ci-dispatch'); return New-RawResponse 204 }
        if ($url -match '/projects/12087/pipeline$' -and $Request.Method -eq 'POST') { $mutations.Add('pipeline-dispatch'); return New-RawResponse 201 @{ id = 2; sha = $candidate; ref = 'master'; source = 'api' } }
        if ($url -match '/actions/runs/1/artifacts') { $state.GitHubArtifactReads++; return New-RawResponse 200 @{ artifacts = @(@{ name = 'candidate-binaries'; expired = $false; digest = ('sha256:' + ('a' * 64)); archive_download_url = 'https://fixture.invalid/github.zip' }) } }
        if ($url -match '/pipelines/2/jobs') { return New-RawResponse 200 @(@{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate } }) }
        if ($url -match '/jobs/3$') { return New-RawResponse 200 @{ id = 3; artifacts_expire_at = $null } }
        if ($url -match '/protected_tags/v0\.1\.3$') {
            if (-not $state.Protected) { return New-RawResponse 404 }
            return New-RawResponse 200 @{ name = 'v0.1.3'; create_access_levels = @(@{ access_level = 40 }) }
        }
        if ($url -match '/protected_tags$') { $mutations.Add('protect'); $state.Protected = $true; return New-RawResponse 201 @{} }
        if ($url -match '/repository/tags/v0\.1\.3$') {
            if (-not $state.TagExists) { return New-RawResponse 404 }
            return New-RawResponse 200 @{ name = 'v0.1.3'; target = ('a' * 40); commit = @{ id = $candidate }; message = '1C Team Kit v0.1.3'; created_at = '2026-08-18T00:00:00Z' }
        }
        if ($url -match '/releases/v0\.1\.3$') {
            if (-not $state.ReleaseExists) { return New-RawResponse 404 }
            return New-RawResponse 200 @{ name = '1C Team Kit v0.1.3'; tag_name = 'v0.1.3'; commit = @{ id = $candidate }; description = $state.ReleaseDescription; assets = @{ links = $state.ReleaseLinks }; _links = @{ self = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3' } }
        }
        if ($url -match '/releases$' -and $Request.Method -eq 'POST') { $body = [Text.Encoding]::UTF8.GetString($Request.BodyUtf8) | ConvertFrom-Json -AsHashtable; $mutations.Add('release'); $state.ReleaseDescription = $body.description; $state.ReleaseLinks = $body.assets.links; $state.ReleaseExists = $true; return New-RawResponse 201 @{ _links = @{ self = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3' } } }
        if ($Request.Method -eq 'POST' -and $url -match '/artifacts/keep$') { $mutations.Add('keep'); return New-RawResponse 200 @{} }
        if ($url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($url -match '/repos/mi1man-cmd/kit-all-team$') { $state.GitHubAuthorityReads++; return New-RawResponse 200 @{ full_name = 'mi1man-cmd/kit-all-team'; private = $true; permissions = @{ push = $true } } @{ 'X-OAuth-Scopes' = 'repo' } }
        if ($url -match '/actions/workflows/release\.yml$') { return New-RawResponse 200 @{ path = '.github/workflows/release.yml'; state = 'active' } }
        if ($url -match '/actions/workflows/ci\.yml$') { return New-RawResponse 200 @{ path = '.github/workflows/ci.yml'; state = 'active' } }
        if ($url -match '/personal_access_tokens/self$') { $state.GitLabAuthorityReads++; return New-RawResponse 200 @{ user_id = 99; active = $true; revoked = $false; expires_at = $null; scopes = @('api') } }
        if ($url -match '/api/v4/user$') { return New-RawResponse 200 @{ id = 99 } }
        if ($url -match '/projects/12087$') { return New-RawResponse 200 @{ id = 12087; path_with_namespace = '1c/aisuz/ai'; archived = $false; permissions = @{ project_access = $null; group_access = @{ access_level = 40 } } } }
        if ($url -match '/workflows/release\.yml/dispatches$') { $state.DispatchBody = [Text.Encoding]::UTF8.GetString($Request.BodyUtf8) | ConvertFrom-Json -AsHashtable; $mutations.Add('dispatch'); $state.FinalDispatched = $true; return New-RawResponse 204 }
        if ($url -match '/workflows/release\.yml/runs') {
            $id = if ($state.FinalDispatched) { 9 } else { 8 }
            return New-RawResponse 200 @{ workflow_runs = @(@{ id = $id; path = '.github/workflows/release.yml'; head_sha = $candidate; head_branch = 'main'; event = 'workflow_dispatch'; status = 'completed'; conclusion = 'success'; created_at = '2026-08-18T00:00:00Z' }) }
        }
        throw "unexpected HTTP $($Request.Method) $url"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $processCalls.Add($FileName + ' ' + ($Arguments -join ' '))
        $operation = if ($Arguments[0] -eq '-c') { $Arguments[2] } else { $Arguments[0] }
        if ($FileName -eq 'git' -and $operation -eq 'status') { return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'for-each-ref') {
            if (-not $state.LocalTagExists) { return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
            if (($Arguments -join ' ') -match 'contents') { return [pscustomobject]@{ ExitCode = 0; StdOut = '1C Team Kit v0.1.3'; StdErr = '' } }
            return [pscustomobject]@{ ExitCode = 0; StdOut = ('a' * 40); StdErr = '' }
        }
        if ($FileName -eq 'git' -and $operation -eq 'rev-parse') { $out = if ($Arguments[-1] -like '*^{tag}') { 'a' * 40 } else { $candidate }; return [pscustomobject]@{ ExitCode = 0; StdOut = $out; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'cat-file') { return [pscustomobject]@{ ExitCode = 0; StdOut = 'tag'; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'tag') { $state.LocalTagExists = $true; $mutations.Add('tag'); return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'push' -and $Arguments -contains '--dry-run') { return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'push') { if ($Arguments[-1] -like 'refs/tags/*') { $state.TagExists = $true; $mutations.Add('tag-push') } else { $mutations.Add('branch-push') }; return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'ls-remote') { return [pscustomobject]@{ ExitCode = 0; StdOut = (('a' * 40) + "`trefs/tags/v0.1.3`n" + $candidate + "`trefs/tags/v0.1.3^{}"); StdErr = '' } }
        if ($FileName -notin @('git', 'go')) { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        return [pscustomobject]@{ ExitCode = 0; StdOut = if ($Arguments[0] -eq 'show') { '2026-08-18T00:00:00Z' } else { '' }; StdErr = '' }
    }.GetNewClosure()
    $paired = { param($Context, $GitHubRequest, $GitLabRequest) @{ github = (New-RawResponse 200 @{ workflow_runs = @(@{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = $Context.State.ExpectedGitHubCIEvent; conclusion = 'success'; created_at = '2026-08-18T00:00:00Z' }) }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = $Context.State.ExpectedGitLabCISource; status = 'success'; created_at = '2026-08-18T00:00:00Z' })) } }.GetNewClosure()
    $order = if ([string]::IsNullOrWhiteSpace($FailAt)) { $null } else {
        {
            param($Context, $Name, $Data)
            if ($Name -eq $FailAt) { throw 'simulated deterministic stage failure' }
        }.GetNewClosure()
    }
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process -PairedCIAdapter $paired -OrderAdapter $order -UtcNowAdapter { param($Context) [datetime]'2026-08-18T00:00:00Z' } -UploadFiles $uploads
    $result = Publish-TeamKitRelease -Context $context
    return @{ Result = $result; State = $state; Mutations = $mutations; ProcessCalls = $processCalls; Context = $context; Candidate = $candidate }
}

function Test-ProductionSuccessUsesNarrowSeams {
    $fixture = Invoke-ProductionSuccessFixture
    $result = $fixture.Result; $state = $fixture.State; $mutations = $fixture.Mutations; $processCalls = $fixture.ProcessCalls; $context = $fixture.Context; $candidate = $fixture.Candidate
    Assert-Equal $result.status 'published' ("production state machine success events=" + ($context.Events -join ','))
    Assert-Equal $result.commit $candidate 'terminal result commit'
    Assert-Equal @($result.files).Count 8 'terminal result has eight file records'
    Assert-Equal @($result.files | Select-Object -ExpandProperty name -Unique).Count 8 'terminal result filenames are distinct'
    foreach ($file in $result.files) {
        Assert-True ($file.size -gt 0 -and $file.sha256 -match '^[0-9a-f]{64}$' -and $file.url -match '^https://') 'terminal file record must be complete'
    }
    Assert-Equal $state.DispatchBody.inputs.candidate_digest ('sha256:' + ('a' * 64)) 'dispatch uses exact GitHub artifact digest'
    $rows = @($state.DispatchBody.inputs.gitlab_sha256s -split "`n")
    Assert-Equal $rows.Count 6 'dispatch has exactly six GitLab checksum rows'
    foreach ($row in $rows) { Assert-True ($row -match '^[0-9a-f]{64}  [^ ]+$') 'dispatch checksum row format' }
    Assert-True ($mutations -contains 'keep') 'real keep step must run'
    Assert-True ($mutations -contains 'protect') 'real protection step must run'
    Assert-True ($mutations -contains 'release') 'real release step must run'
    Assert-Equal $state.GitHubArtifactReads 1 'kept and post-verification downloads must bind the initial GitLab six hashes rather than rereading GitHub artifacts'
    Assert-True (@($processCalls | Where-Object { $_ -match 'scripts/build\.ps1' }).Count -eq 1) 'preflight must rerun the local release build gate'
    Assert-True (@($processCalls | Where-Object { $_ -match 'teamkit-security-audit' }).Count -eq 1) 'preflight must rerun the local security-audit gate'
}

function Test-TerminalFailureUsesStableStageReasonAndRemoteTagForwardRecovery {
    $fixture = Invoke-ProductionSuccessFixture -ExistingTag -FailAt 'sync-refs'
    $result = $fixture.Result
    Assert-Equal $result.status 'failed' 'an ordinary post-preflight failure must remain an ordinary terminal failure'
    Assert-Equal $result.exit_code 1 'ordinary failure exit code'
    Assert-Equal $result.stage 'sync-refs' 'ordinary failure must identify the current stable stage'
    Assert-Equal $result.reason_code 'SYNC_REFS_FAILED' 'ordinary failure must expose a deterministic stage reason code'
    Assert-Equal $result.error 'release operation failed' 'ordinary failure must keep the redacted generic error message'
    Assert-True ([bool]$result.recovery.forward_only) 'an exact remote tag proven during preflight must make later recovery forward-only'
}

function Test-TerminalDeadlineUsesDistinctReasonCodeAndStage {
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y -OrderAdapter {
        param($Context, $Name, $Data)
        if ($Name -eq 'preflight') { throw [System.TimeoutException]::new('simulated deadline') }
    }
    $result = Publish-TeamKitRelease -Context $context
    Assert-Equal $result.status 'deadline_exceeded' 'timeout must use the deadline terminal status'
    Assert-Equal $result.exit_code 124 'timeout must retain its distinct terminal exit code'
    Assert-Equal $result.stage 'preflight' 'timeout must identify the stable current stage'
    Assert-Equal $result.reason_code 'DEADLINE_EXCEEDED' 'timeout must not reuse an ordinary failure reason code'
    Assert-True (-not [bool]$result.recovery.forward_only) 'a timeout before a remote tag proof must not claim forward-only recovery'
}

function Test-PublisherRechecksReadOnlyApiAuthorityBeforeTagAndRelease {
    $fixture = Invoke-ProductionSuccessFixture
    Assert-Equal $fixture.Result.status 'published' 'authority recheck fixture must reach a successful terminal result'
    Assert-Equal $fixture.State.GitHubAuthorityReads 3 'GitHub authority must be proved at preflight and rechecked before tag and Release mutation'
    Assert-Equal $fixture.State.GitLabAuthorityReads 3 'GitLab authority must be proved at preflight and rechecked before tag and Release mutation'
    Assert-Equal @($fixture.Context.Events | Where-Object { $_ -eq 'revalidate-authority' }).Count 2 'two explicit read-only authority rechecks must bracket the tag and Release mutations'
}

function Test-ExactTagForwardRecoveryCreatesMissingReleaseWithoutRetagging {
    $fixture = Invoke-ProductionSuccessFixture -ExistingTag
    $result = $fixture.Result; $mutations = $fixture.Mutations; $processCalls = $fixture.ProcessCalls; $context = $fixture.Context
    Assert-Equal $result.status 'published' ("exact-tag forward recovery must create the missing Release events=" + ($context.Events -join ','))
    Assert-True ($mutations -contains 'ci-dispatch' -and $mutations -contains 'pipeline-dispatch') 'candidate refs with an exact tag must dispatch fresh correlated CI instead of no-op pushes'
    Assert-True ($mutations -contains 'release') 'exact annotated tag recovery must create the missing GitLab Release'
    Assert-True (-not ($mutations -contains 'tag') -and -not ($mutations -contains 'tag-push')) 'exact annotated tag recovery must never recreate or push the tag'
    Assert-True (@($processCalls | Where-Object { $_ -match 'tag.gpgSign=false' }).Count -eq 0) 'exact remote tag recovery must use no local tag command'
}

function Test-SyncStopsBeforeSecondPushOnGitHubReadbackMismatch {
    $candidate = 'b' * 40
    $pushes = [System.Collections.Generic.List[string]]::new()
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/actions/workflows/ci\.yml/runs') { return New-RawResponse 200 @{ workflow_runs = @() } }
        if ($Request.Url -match '/projects/12087/pipelines\?sha=') { return [pscustomobject]@{ StatusCode = 200; Headers = @{}; BodyUtf8 = [Text.Encoding]::UTF8.GetBytes('[]') } }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = ('c' * 40) } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        throw "unexpected HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $operation = if ($Arguments[0] -eq '-c') { $Arguments[2] } else { $Arguments[0] }
        if ($operation -eq 'push') { $pushes.Add($Arguments[-1]) }
        return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
    try { Sync-ReleaseRefs -Context $context | Out-Null } catch { }
    Assert-Equal $pushes.Count 1 'GitHub readback mismatch must stop before GitLab push'
}

function Test-NoopCandidateRefsDispatchFreshCIOnBothProviders {
    $candidate = 'b' * 40
    $state = @{ GitHubDispatches = 0; GitLabDispatches = 0; Pushes = 0; GitHubDispatchBody = $null }
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/actions/workflows/ci\.yml/runs') { return New-RawResponse 200 @{ workflow_runs = @() } }
        if ($Request.Url -match '/projects/12087/pipelines\?sha=') { return [pscustomobject]@{ StatusCode = 200; Headers = @{}; BodyUtf8 = [Text.Encoding]::UTF8.GetBytes('[]') } }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/workflows/ci\.yml/dispatches$' -and $Request.Method -eq 'POST') { $state.GitHubDispatches++; $state.GitHubDispatchBody = [Text.Encoding]::UTF8.GetString($Request.BodyUtf8) | ConvertFrom-Json -AsHashtable; return New-RawResponse 204 }
        if ($Request.Url -match '/projects/12087/pipeline$' -and $Request.Method -eq 'POST') { $state.GitLabDispatches++; return New-RawResponse 201 @{ id = 22; sha = $candidate; ref = 'master'; status = 'pending' } }
        throw "unexpected no-op sync HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $operation = if ($Arguments[0] -eq '-c') { $Arguments[2] } else { $Arguments[0] }
        if ($FileName -eq 'git' -and $operation -eq 'push') { $state.Pushes++ }
        return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process -UtcNowAdapter { param($Context) [datetime]'2026-08-18T00:00:00Z' }
    Sync-ReleaseRefs -Context $context | Out-Null
    Assert-Equal $state.Pushes 0 'candidate refs must not use no-op branch pushes to manufacture CI'
    Assert-Equal $state.GitHubDispatches 1 'candidate GitHub ref must dispatch one fresh CI workflow'
    Assert-Equal $state.GitHubDispatchBody.inputs.expected_sha $candidate 'GitHub CI dispatch must bind workflow input expected_sha to CandidateSha'
    Assert-Equal $state.GitLabDispatches 1 'candidate GitLab ref must create one fresh API pipeline'
    Assert-Equal $context.State.ExpectedGitHubCIEvent 'workflow_dispatch' 'GitHub dispatch correlation must require the dispatched event'
    Assert-Equal $context.State.ExpectedGitLabCISource 'api' 'GitLab dispatch correlation must require the API source'
    Assert-Equal $context.State.ExpectedGitLabPipelineId 22 'GitLab dispatch correlation must bind the created pipeline ID'
}

function Test-V015ComparePreparationDoesNotDownloadGitHubArchive {
    $candidate = 'b' * 40
    $publication = New-PublicationZip $candidate -Version v0.1.5
    $state = @{ GitHubArchiveDownloads = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download' -and $Request.Url -match '/jobs/202/artifacts$') { return New-RawResponse 200 $publication.Bytes }
        if ($Request.Purpose -eq 'download' -and $Request.Url -match 'github\.zip') { $state.GitHubArchiveDownloads++; throw 'v0.1.5 must not download GitHub archive locally' }
        if ($Request.Url -match '/actions/artifacts/102$') { return New-RawResponse 200 @{ id = 102; name = 'candidate-binaries'; expired = $false; digest = ('sha256:' + ('a' * 64)); archive_download_url = 'https://fixture.invalid/github.zip'; workflow_run = @{ id = 101; head_sha = $candidate } } }
        throw "unexpected v0.1.5 comparison HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        if ($FileName -like '*teamkit-v0.1.5-windows-amd64.exe') { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.5`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        throw "unexpected process $FileName"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter $http -ProcessAdapter $process
    $ci = @{ github_run_id = 101; github_artifact_id = 102; GitHubArtifactDigest = ('sha256:' + ('a' * 64)); gitlab_pipeline_id = 201; gitlab_job_id = 202 }
    $result = & (Get-Module BoundedRelease) { param($Context, $CI) Get-ProductionPublicationSets $Context $CI } $context $ci
    Assert-Equal $state.GitHubArchiveDownloads 0 'v0.1.5 preparation must defer GitHub bytes to final-release-validation'
    Assert-Equal $result.GitHubArtifactDigest ('sha256:' + ('a' * 64)) 'v0.1.5 preparation must retain immutable GitHub artifact digest'
    Assert-Equal $result.Hashes.Count 6 'v0.1.5 preparation must produce the exact six GitLab hashes'
}

function Test-V015ContextBindsPackageAndVerifiedCIInputs {
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabHandoffJobId 202 -GitLabVerifyJobId 203
    Assert-Equal $context.GitHubRepository 'i437918/kit-all-team' 'v0.1.5 GitHub authority'
    Assert-Equal $context.PackageName 'teamkit' 'v0.1.5 package name'
    Assert-Equal $context.PackageVersion 'v0.1.5' 'v0.1.5 package version'
    Assert-Equal $context.ReleaseFiles.Count 6 'v0.1.5 exact package file count'
    Assert-True ($context.ReleaseFiles -contains 'teamkit-v0.1.5-windows-amd64.exe') 'v0.1.5 Windows package filename'
    Assert-True (-not (($context.ReleaseFiles -join "`n") -match 'v0\.1\.4')) 'v0.1.5 context must not use legacy v0.1.4 evidence'
    Assert-Equal $context.VerifiedCI.github_run_id 101 'verified GitHub run ID'
    Assert-Equal $context.VerifiedCI.github_artifact_id 102 'verified GitHub artifact ID'
    Assert-Equal $context.VerifiedCI.gitlab_pipeline_id 201 'verified GitLab pipeline ID'
    Assert-Equal $context.VerifiedCI.gitlab_handoff_job_id 202 'verified GitLab handoff job ID'
    Assert-Equal $context.VerifiedCI.gitlab_job_id 203 'verified GitLab verify job ID'
}

function Test-V015ReleaseAssetsUseHumanReadablePlatformLabels {
    $candidate = 'b' * 40
    $context = New-ReleaseContext -CandidateSha $candidate -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202
    $hashes = @{}
    foreach ($name in $context.ReleaseFiles) { $hashes[$name] = 'a' * 64 }
    $labels = @{
        'teamkit-v0.1.5-windows-amd64.exe' = 'Windows amd64'
        'teamkit-v0.1.5-linux-amd64'       = 'Linux amd64'
        'teamkit-v0.1.5-darwin-amd64'      = 'macOS amd64'
        'teamkit-v0.1.5-darwin-arm64'      = 'macOS arm64'
        'SHA256SUMS'                       = 'SHA256SUMS'
        'SECURITY-AUDIT.json'              = 'Отчёт аудита безопасности'
    }
    $ci = @{ github_run_id = 101; gitlab_pipeline_id = 201; gitlab_job_id = 202; Hashes = $hashes }
    $links = & (Get-Module BoundedRelease) {
        param($Context, $Labels)
        @($Context.ReleaseFiles | ForEach-Object {
            @{ name = $Labels[$_]; url = (Get-GenericPackageUrl $Context $_); link_type = 'other' }
        })
    } $context $labels
    $release = @{
        name = '1C Team Kit v0.1.5'
        tag_name = 'v0.1.5'
        commit = @{ id = $candidate }
        description = (& (Get-Module BoundedRelease) { param($Context, $CI) New-ReleaseNotes $Context $CI } $context $ci)
        assets = @{ links = $links }
        _links = @{ self = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.5' }
    }
    Assert-True (& (Get-Module BoundedRelease) { param($Release, $Context, $CI) Test-ExactGitLabRelease $Release $Context $CI } $release $context $ci) 'v0.1.5 Release must show platform labels while retaining exact package URLs'
}

function Test-V015VerifyOnlyFlowRevalidatesProvidedCIWithoutDispatch {
    $candidate = 'b' * 40
    $state = @{ RemoteMutations = 0; GitPushes = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -in @('POST', 'PUT', 'DELETE')) { $state.RemoteMutations++; throw 'verify-only flow attempted a remote mutation' }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/pipelines/201$') { return New-RawResponse 200 @{ id = 201; sha = $candidate; ref = 'master'; source = 'push'; status = 'success' } }
        if ($Request.Url -match '/jobs/203$') { return New-RawResponse 200 @{ id = 203; name = 'release-handoff'; status = 'success'; commit = @{ id = $candidate }; pipeline = @{ id = 201 }; artifacts_file = @{ filename = 'artifacts.zip' } } }
        if ($Request.Url -match '/jobs/202$') { return New-RawResponse 200 @{ id = 202; name = 'verify'; status = 'success'; commit = @{ id = $candidate }; pipeline = @{ id = 201 }; artifacts_file = @{ filename = 'artifacts.zip' } } }
        throw "unexpected verify-only HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $operation = if ($Arguments.Count -gt 2 -and $Arguments[0] -eq '-c') { $Arguments[2] } else { $Arguments[0] }
        if ($FileName -eq 'git' -and $operation -eq 'push') { $state.GitPushes++; throw 'verify-only flow attempted git push' }
        return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabHandoffJobId 203 -GitLabVerifyJobId 202 -HttpAdapter $http -ProcessAdapter $process
    $sync = Sync-ReleaseRefs -Context $context
    $ci = Wait-ExactShaCI -Context $context
    Assert-True ([bool]$sync.verify_only) 'v0.1.5 ref step must be verify-only'
    Assert-Equal $state.RemoteMutations 0 'v0.1.5 optimized flow must not dispatch duplicate CI'
    Assert-Equal $state.GitPushes 0 'v0.1.5 optimized flow must not push either branch'
    Assert-Equal $ci.gitlab_pipeline_id 201 'v0.1.5 exact GitLab pipeline revalidation'
    Assert-Equal $ci.gitlab_handoff_job_id 203 'v0.1.5 exact GitLab handoff job revalidation'
    Assert-Equal $ci.gitlab_job_id 202 'v0.1.5 exact GitLab verify job revalidation'
}

function Test-V015RejectsPushRunBeforeAnyMutation {
    $candidate = 'b' * 40
    $state = @{ RemoteMutations = 0; GitPushes = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -in @('POST', 'PUT', 'DELETE')) { $state.RemoteMutations++; throw 'push evidence reached a remote mutation' }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/actions/runs/101$') { return New-RawResponse 200 @{ id = 101; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; conclusion = 'success' } }
        if ($Request.Url -match '/actions/artifacts/102$') { return New-RawResponse 200 @{ id = 102; name = 'candidate-binaries'; expired = $false; digest = ('sha256:' + ('a' * 64)); archive_download_url = 'https://fixture.invalid/github.zip'; workflow_run = @{ id = 101; head_sha = $candidate } } }
        if ($Request.Url -match '/pipelines/201$') { return New-RawResponse 200 @{ id = 201; sha = $candidate; ref = 'master'; source = 'push'; status = 'success' } }
        if ($Request.Url -match '/jobs/202$') { return New-RawResponse 200 @{ id = 202; name = 'verify'; status = 'success'; commit = @{ id = $candidate }; pipeline = @{ id = 201 } } }
        throw "unexpected push-evidence HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $operation = if ($Arguments.Count -gt 2 -and $Arguments[0] -eq '-c') { $Arguments[2] } else { $Arguments[0] }
        if ($FileName -eq 'git' -and $operation -eq 'push') { $state.GitPushes++; throw 'push evidence reached git push' }
        return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter $http -ProcessAdapter $process
    Sync-ReleaseRefs -Context $context | Out-Null
    $failed = $false
    try { Wait-ExactShaCI -Context $context | Out-Null } catch { $failed = $true }
    Assert-True $failed 'v0.1.5 must reject an otherwise exact successful push run'
    Assert-Equal $state.RemoteMutations 0 'push CI evidence must fail before any remote mutation'
    Assert-Equal $state.GitPushes 0 'push CI evidence must fail before any git push'
}

function New-V015PackageInputFixture {
    $root = Join-Path ([System.IO.Path]::GetTempPath()) ('teamkit-v015-package-test-' + [Guid]::NewGuid().ToString('N'))
    [void][System.IO.Directory]::CreateDirectory($root)
    $names = @('teamkit-v0.1.5-windows-amd64.exe', 'teamkit-v0.1.5-linux-amd64', 'teamkit-v0.1.5-darwin-amd64', 'teamkit-v0.1.5-darwin-arm64', 'SHA256SUMS', 'SECURITY-AUDIT.json')
    $files = @{}; $hashes = @{}; $bytes = @{}
    foreach ($name in $names) {
        $path = Join-Path $root $name
        $bytes[$name] = [Text.Encoding]::UTF8.GetBytes("fixture-$name")
        [System.IO.File]::WriteAllBytes($path, $bytes[$name])
        $files[$name] = $path
        $hashes[$name] = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    return @{ Root = $root; Names = $names; Files = $files; Hashes = $hashes; Bytes = $bytes }
}

function New-V015PackageRecord([int]$Id = 501) {
    return @{ id = $Id; name = 'teamkit'; version = 'v0.1.5'; package_type = 'generic'; status = 'default' }
}

function Test-V015GenericPackageUploadsThenAuthenticatedRedownloadsExactSix {
    $candidate = 'b' * 40
    $fixture = New-V015PackageInputFixture
    $state = @{ Uploads = @{}; UploadedNames = [System.Collections.Generic.List[string]]::new(); PackageQueries = 0; FileQueries = 0; Gets = 0; Authenticated = $true; Deletes = 0; Puts = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -eq 'DELETE') { $state.Deletes++; throw 'deletion is forbidden' }
        if (-not $Request.Headers.ContainsKey('PRIVATE-TOKEN') -or $Request.Headers['PRIVATE-TOKEN'] -ne 'y') { $state.Authenticated = $false }
        if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages\?.*package_type=generic.*package_name=teamkit.*package_version=v0\.1\.5.*status=([^&]+).*page=([0-9]+)') {
            $state.PackageQueries++
            $status = [Uri]::UnescapeDataString($Matches[1])
            if ([int]$Matches[2] -ne 1) { throw 'unexpected package inventory page' }
            $records = if ($state.UploadedNames.Count -gt 0 -and $status -eq 'default') { @((New-V015PackageRecord)) } else { @() }
            return New-JsonArrayResponse $records @{ 'X-Next-Page' = '' }
        }
        if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages/501/package_files\?.*page=([0-9]+)') {
            $state.FileQueries++
            if ([int]$Matches[1] -ne 1) { throw 'unexpected package-file inventory page' }
            $records = @()
            for ($index = 0; $index -lt $state.UploadedNames.Count; $index++) {
                $name = $state.UploadedNames[$index]
                $records += @{ id = 700 + $index; package_id = 501; file_name = $name; size = $state.Uploads[$name].Length; file_sha256 = $fixture.Hashes[$name] }
            }
            return New-JsonArrayResponse $records @{ 'X-Next-Page' = '' }
        }
        if ($Request.Url -match '/packages/generic/teamkit/v0\.1\.5/([^/?]+)$') {
            $name = [Uri]::UnescapeDataString($Matches[1])
            if ($Request.Method -eq 'GET') {
                $state.Gets++
                return New-RawResponse 200 $state.Uploads[$name]
            }
            if ($Request.Method -eq 'PUT') {
                $state.Puts++
                if ($state.Uploads.ContainsKey($name)) { throw "overwrite attempted: $name" }
                $state.Uploads[$name] = [byte[]]$Request.BodyUtf8
                $state.UploadedNames.Add($name)
                return New-RawResponse 201 @{}
            }
        }
        throw "unexpected package HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    try {
        $context = New-ReleaseContext -CandidateSha $candidate -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter $http
        $records = & (Get-Module BoundedRelease) { param($Context, $Files, $Hashes) Publish-GenericPackageSet $Context $Files $Hashes } $context $fixture.Files $fixture.Hashes
        Assert-Equal $state.Uploads.Count 6 'package publisher must upload exactly six files'
        Assert-Equal $state.Puts 6 'package publisher must PUT every file exactly once'
        Assert-Equal $state.PackageQueries 42 'package publisher must inventory all six statuses before upload and after every PUT'
        Assert-Equal $state.FileQueries 6 'package publisher must inventory the exact uploaded prefix after every PUT'
        Assert-Equal $state.Gets 6 'package publisher must re-download every uploaded file'
        Assert-True $state.Authenticated 'package inventory, upload, and re-download must use the authenticated GitLab API'
        Assert-Equal $state.Deletes 0 'package publisher must never delete package files'
        Assert-Equal @($records).Count 6 'package publisher must return six verified records'
        foreach ($record in @($records)) {
            Assert-True ($record.url -match '/api/v4/projects/12087/packages/generic/teamkit/v0\.1\.5/') 'Release link must be a Generic Package URL'
            Assert-Equal $record.sha256 $fixture.Hashes[$record.name] 're-downloaded package hash must match candidate file'
        }
    } finally {
        [System.IO.Directory]::Delete($fixture.Root, $true)
    }
}

function Test-V015UnexpectedExistingPackageOnLaterPageStopsBeforeAnyUpload {
    $fixture = New-V015PackageInputFixture
    $state = @{ Puts = 0; Deletes = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -eq 'DELETE') { $state.Deletes++; throw 'delete is forbidden' }
        if ($Request.Method -eq 'PUT') { $state.Puts++; return New-RawResponse 201 @{} }
        if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages\?.*status=([^&]+).*page=1') {
            $status = [Uri]::UnescapeDataString($Matches[1])
            $next = if ($status -eq 'hidden') { '2' } else { '' }
            return New-JsonArrayResponse @() @{ 'X-Next-Page' = $next }
        }
        if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages\?.*status=hidden.*page=2') {
            $record = New-V015PackageRecord
            $record.status = 'hidden'
            return New-JsonArrayResponse @($record) @{ 'X-Next-Page' = '' }
        }
        if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages/501/package_files') {
            return New-JsonArrayResponse @(@{ id = 701; package_id = 501; file_name = 'unexpected.bin'; size = 1; file_sha256 = ('c' * 64) }) @{ 'X-Next-Page' = '' }
        }
        if ($Request.Method -eq 'HEAD' -and $Request.Url -match '/packages/generic/teamkit/v0\.1\.5/') { return New-RawResponse 404 }
        throw "unexpected existing-package HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    try {
        $context = New-ReleaseContext -CandidateSha ('b' * 40) -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter $http
        $failed = $false
        try { & (Get-Module BoundedRelease) { param($Context, $Files, $Hashes) Publish-GenericPackageSet $Context $Files $Hashes } $context $fixture.Files $fixture.Hashes | Out-Null } catch { $failed = $true }
        Assert-True $failed 'an existing package containing only an unexpected filename must fail closed'
        Assert-Equal $state.Puts 0 'an existing package on any page must stop before the first upload'
        Assert-Equal $state.Deletes 0 'existing package state must never be deleted'
    } finally { [System.IO.Directory]::Delete($fixture.Root, $true) }
}

function Test-V015DuplicatePackageRecordsStopBeforeAnyUpload {
    $fixture = New-V015PackageInputFixture
    $state = @{ Puts = 0; Deletes = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -eq 'DELETE') { $state.Deletes++; throw 'delete is forbidden' }
        if ($Request.Method -eq 'PUT') { $state.Puts++; return New-RawResponse 201 @{} }
        if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages\?.*status=([^&]+).*page=1') {
            $status = [Uri]::UnescapeDataString($Matches[1])
            $records = if ($status -eq 'default') { @((New-V015PackageRecord 501), (New-V015PackageRecord 502)) } else { @() }
            return New-JsonArrayResponse $records @{ 'X-Next-Page' = '' }
        }
        if ($Request.Method -eq 'HEAD' -and $Request.Url -match '/packages/generic/teamkit/v0\.1\.5/') { return New-RawResponse 404 }
        throw "unexpected duplicate-package HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    try {
        $context = New-ReleaseContext -CandidateSha ('b' * 40) -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter $http
        $failed = $false
        try { & (Get-Module BoundedRelease) { param($Context, $Files, $Hashes) Publish-GenericPackageSet $Context $Files $Hashes } $context $fixture.Files $fixture.Hashes | Out-Null } catch { $failed = $true }
        Assert-True $failed 'duplicate exact package records must fail closed'
        Assert-Equal $state.Puts 0 'duplicate package records must stop before the first upload'
        Assert-Equal $state.Deletes 0 'duplicate package records must never be deleted'
    } finally { [System.IO.Directory]::Delete($fixture.Root, $true) }
}

function Test-V015ConcurrentExtraDuplicateOrWrongApiHashStopsAfterOnePut {
    foreach ($mode in @('extra', 'duplicate', 'api-hash')) {
        $fixture = New-V015PackageInputFixture
        $state = @{ Puts = 0; PackageQueries = 0; TagMutations = 0; Deletes = 0 }
        $http = {
            param($Context, $Request)
            if ($Request.Method -eq 'DELETE') { $state.Deletes++; throw 'delete is forbidden' }
            if ($Request.Method -eq 'POST' -and $Request.Url -match '/(protected_tags|repository/tags|releases)') { $state.TagMutations++; throw 'publication mutation is forbidden after package ambiguity' }
            if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages\?.*status=([^&]+).*page=1') {
                $state.PackageQueries++
                $status = [Uri]::UnescapeDataString($Matches[1])
                $records = if ($state.Puts -gt 0 -and $status -eq 'default') { @((New-V015PackageRecord)) } else { @() }
                return New-JsonArrayResponse $records @{ 'X-Next-Page' = '' }
            }
            if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages/501/package_files\?.*page=1') {
                $first = $fixture.Names[0]
                $firstHash = if ($mode -eq 'api-hash') { 'c' * 64 } else { $fixture.Hashes[$first] }
                $records = @(@{ id = 701; package_id = 501; file_name = $first; size = $fixture.Bytes[$first].Length; file_sha256 = $firstHash })
                if ($mode -eq 'extra') { $records += @{ id = 702; package_id = 501; file_name = 'unexpected.bin'; size = 1; file_sha256 = ('c' * 64) } }
                if ($mode -eq 'duplicate') { $records += @{ id = 702; package_id = 501; file_name = $first; size = $fixture.Bytes[$first].Length; file_sha256 = $fixture.Hashes[$first] } }
                return New-JsonArrayResponse $records @{ 'X-Next-Page' = '' }
            }
            if ($Request.Method -eq 'PUT' -and $Request.Url -match '/packages/generic/teamkit/v0\.1\.5/') { $state.Puts++; return New-RawResponse 201 @{} }
            if ($Request.Method -eq 'HEAD' -and $Request.Url -match '/packages/generic/teamkit/v0\.1\.5/') { return New-RawResponse 404 }
            throw "unexpected concurrent-$mode HTTP $($Request.Method) $($Request.Url)"
        }.GetNewClosure()
        try {
            $context = New-ReleaseContext -CandidateSha ('b' * 40) -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter $http
            $failed = $false
            try { & (Get-Module BoundedRelease) { param($Context, $Files, $Hashes) Publish-GenericPackageSet $Context $Files $Hashes } $context $fixture.Files $fixture.Hashes | Out-Null } catch { $failed = $true }
            Assert-True $failed "concurrent $mode package file must fail closed"
            Assert-Equal $state.Puts 1 "concurrent $mode file must stop after the first one-shot PUT"
            Assert-Equal $state.TagMutations 0 "concurrent $mode file must stop before tag or Release mutation"
            Assert-Equal $state.Deletes 0 "concurrent $mode file must leave partial state for manual intervention"
        } finally { [System.IO.Directory]::Delete($fixture.Root, $true) }
    }
}

function Test-V015PackageUploadRequiresExact201WithoutRetry {
    $fixture = New-V015PackageInputFixture
    $state = @{ Puts = 0; Deletes = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -eq 'DELETE') { $state.Deletes++; throw 'delete is forbidden' }
        if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages\?.*status=[^&]+.*page=1') { return New-JsonArrayResponse @() @{ 'X-Next-Page' = '' } }
        if ($Request.Method -eq 'PUT' -and $Request.Url -match '/packages/generic/teamkit/v0\.1\.5/') { $state.Puts++; return New-RawResponse 200 @{} }
        if ($Request.Method -eq 'HEAD' -and $Request.Url -match '/packages/generic/teamkit/v0\.1\.5/') { return New-RawResponse 404 }
        throw "unexpected non-201 HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    try {
        $context = New-ReleaseContext -CandidateSha ('b' * 40) -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter $http
        $failed = $false
        try { & (Get-Module BoundedRelease) { param($Context, $Files, $Hashes) Publish-GenericPackageSet $Context $Files $Hashes } $context $fixture.Files $fixture.Hashes | Out-Null } catch { $failed = $true }
        Assert-True $failed 'a non-201 package response is ambiguous and must fail closed'
        Assert-Equal $state.Puts 1 'an ambiguous package PUT must never be retried'
        Assert-Equal $state.Deletes 0 'an ambiguous package response must leave partial state for manual intervention'
    } finally { [System.IO.Directory]::Delete($fixture.Root, $true) }
}

function Test-V015PostPackageRefMoveStopsBeforeTagMutation {
    $candidate = 'b' * 40
    $fixture = New-V015PackageInputFixture
    $state = @{ RefReads = 0; TagMutations = 0; Deletes = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -eq 'DELETE') { $state.Deletes++; throw 'delete is forbidden' }
        if ($Request.Method -in @('POST', 'PUT') -and $Request.Url -match '/(protected_tags|repository/tags|releases)') { $state.TagMutations++; throw 'tag or Release mutation was reached' }
        if ($Request.Url -match '/git/ref/heads/main$') { $state.RefReads++; return New-RawResponse 200 @{ object = @{ sha = ('c' * 40) } } }
        if ($Request.Url -match '/repository/branches/master$') { $state.RefReads++; return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        throw "unexpected post-package ref-move HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    try {
        $context = New-ReleaseContext -CandidateSha $candidate -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter $http
        $data = @{ gitlab_job_id = 202; Hashes = $fixture.Hashes; PackageRecords = @() }
        $failed = $false; $failure = ''
        try { & (Get-Module BoundedRelease) { param($Context, $Data) Invoke-ReleaseStep $Context 'post-package-reserve' $Data } $context $data | Out-Null } catch { $failed = $true; $failure = $_.Exception.Message }
        Assert-True $failed 'a branch move after package upload must fail the immediate pre-tag reserve'
        Assert-Equal $state.RefReads 2 'post-package reserve must reread both production refs'
        Assert-True ($failure -match 'branch ref changed') 'post-package reserve must fail for the moved ref, not an unknown stage'
        Assert-Equal $state.TagMutations 0 'a post-package ref move must stop before protected-tag, tag, or Release mutation'
        Assert-Equal $state.Deletes 0 'a post-package ref move must leave the partial package for manual intervention'
    } finally { [System.IO.Directory]::Delete($fixture.Root, $true) }
}

function Test-V015PostPackageLateExtraFileStopsBeforeTagMutation {
    $candidate = 'b' * 40
    $fixture = New-V015PackageInputFixture
    $state = @{ TagMutations = 0; Deletes = 0; InventoryReads = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Method -eq 'DELETE') { $state.Deletes++; throw 'delete is forbidden' }
        if ($Request.Method -in @('POST', 'PUT') -and $Request.Url -match '/(protected_tags|repository/tags|releases)') { $state.TagMutations++; throw 'tag or Release mutation was reached' }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/jobs/202$') { return New-RawResponse 200 @{ id = 202; name = 'verify'; status = 'success'; commit = @{ id = $candidate }; pipeline = @{ id = 201 }; artifacts_expire_at = $null } }
        if ($Request.Url -match '/protected_tags/v0\.1\.5$' -or $Request.Url -match '/repository/tags/v0\.1\.5$' -or $Request.Url -match '/releases/v0\.1\.5$') { return New-RawResponse 404 }
        if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages\?.*status=([^&]+).*page=1') {
            $state.InventoryReads++
            $status = [Uri]::UnescapeDataString($Matches[1])
            $records = if ($status -eq 'default') { @((New-V015PackageRecord)) } else { @() }
            return New-JsonArrayResponse $records @{ 'X-Next-Page' = '' }
        }
        if ($Request.Method -eq 'GET' -and $Request.Url -match '/api/v4/projects/12087/packages/501/package_files\?.*page=1') {
            $state.InventoryReads++
            $records = @()
            for ($index = 0; $index -lt $fixture.Names.Count; $index++) {
                $name = $fixture.Names[$index]
                $records += @{ id = 701 + $index; package_id = 501; file_name = $name; size = $fixture.Bytes[$name].Length; file_sha256 = $fixture.Hashes[$name] }
            }
            $records += @{ id = 799; package_id = 501; file_name = 'late-extra.bin'; size = 1; file_sha256 = ('c' * 64) }
            return New-JsonArrayResponse $records @{ 'X-Next-Page' = '' }
        }
        throw "unexpected post-package extra-file HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    try {
        $context = New-ReleaseContext -CandidateSha $candidate -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter $http
        $data = @{ github_run_id = 101; github_artifact_id = 102; gitlab_pipeline_id = 201; gitlab_job_id = 202; Hashes = $fixture.Hashes; PackageRecords = @() }
        $failed = $false
        try { & (Get-Module BoundedRelease) { param($Context, $Data) Invoke-ReleaseStep $Context 'post-package-reserve' $Data } $context $data | Out-Null } catch { $failed = $true }
        Assert-True $failed 'a late extra package file must fail the immediate pre-tag inventory'
        Assert-True ($state.InventoryReads -gt 0) 'post-package reserve must repeat the exact-six inventory'
        Assert-Equal $state.TagMutations 0 'a late extra package file must stop before protected-tag, tag, or Release mutation'
        Assert-Equal $state.Deletes 0 'a late extra package file must leave partial package state for manual intervention'
    } finally { [System.IO.Directory]::Delete($fixture.Root, $true) }
}

function Test-ExactReleaseRerunShortCircuitsWithZeroMutations {
    $candidate = 'b' * 40
    $gitlabPublication = New-PublicationZip $candidate
    $githubPublication = New-PublicationZip $candidate 'windows fixture' root
    $uploadBytes = [Text.Encoding]::UTF8.GetBytes('existing-release-upload-one')
    $uploadTwoBytes = [Text.Encoding]::UTF8.GetBytes('existing-release-upload-two')
    $uploadHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadBytes)).ToLowerInvariant()
    $uploadTwoHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadTwoBytes)).ToLowerInvariant()
    $uploads = @(@{ Name = 'fixture-one.bin'; Url = 'https://fixture.invalid/existing-one'; ApiUrl = 'https://fixture.invalid/existing-one'; Size = $uploadBytes.Length; Sha256 = $uploadHash }, @{ Name = 'fixture-two.bin'; Url = 'https://fixture.invalid/existing-two'; ApiUrl = 'https://fixture.invalid/existing-two'; Size = $uploadTwoBytes.Length; Sha256 = $uploadTwoHash })
    $state = @{ Release = $null; Mutations = [System.Collections.Generic.List[string]]::new() }
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download' -and $Request.Url -match '/jobs/3/artifacts$') { return New-RawResponse 200 $gitlabPublication.Bytes }
        if ($Request.Purpose -eq 'download' -and $Request.Url -eq 'https://fixture.invalid/github.zip') { return New-RawResponse 200 $githubPublication.Bytes }
        if ($Request.Purpose -eq 'download' -and $Request.Url -eq 'https://fixture.invalid/existing-one') { return New-RawResponse 200 $uploadBytes }
        if ($Request.Purpose -eq 'download' -and $Request.Url -eq 'https://fixture.invalid/existing-two') { return New-RawResponse 200 $uploadTwoBytes }
        if ($Request.Method -eq 'POST') { $state.Mutations.Add($Request.Url); throw 'an exact published rerun must not mutate' }
        if ($Request.Url -match '/actions/runs/1/artifacts') { return New-RawResponse 200 @{ artifacts = @(@{ name = 'candidate-binaries'; expired = $false; digest = ('sha256:' + ('a' * 64)); archive_download_url = 'https://fixture.invalid/github.zip' }) } }
        if ($Request.Url -match '/actions/runs/1$') { return New-RawResponse 200 @{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; conclusion = 'success' } }
        if ($Request.Url -match '/pipelines/2$') { return New-RawResponse 200 @{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'success' } }
        if ($Request.Url -match '/jobs/3$') { return New-RawResponse 200 @{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate }; pipeline = @{ id = 2 }; artifacts_expire_at = $null } }
        if ($Request.Url -match '/repos/mi1man-cmd/kit-all-team$') { return New-RawResponse 200 @{ full_name = 'mi1man-cmd/kit-all-team'; private = $true; permissions = @{ push = $true } } @{ 'X-OAuth-Scopes' = 'repo' } }
        if ($Request.Url -match '/actions/workflows/release\.yml$') { return New-RawResponse 200 @{ path = '.github/workflows/release.yml'; state = 'active' } }
        if ($Request.Url -match '/actions/workflows/ci\.yml$') { return New-RawResponse 200 @{ path = '.github/workflows/ci.yml'; state = 'active' } }
        if ($Request.Url -match '/personal_access_tokens/self$') { return New-RawResponse 200 @{ user_id = 99; active = $true; revoked = $false; expires_at = $null; scopes = @('api') } }
        if ($Request.Url -match '/api/v4/user$') { return New-RawResponse 200 @{ id = 99 } }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/projects/12087$') { return New-RawResponse 200 @{ id = 12087; path_with_namespace = '1c/aisuz/ai'; archived = $false; permissions = @{ project_access = @{ access_level = 40 }; group_access = $null } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/protected_tags/v0\.1\.3$') { return New-RawResponse 200 @{ name = 'v0.1.3'; create_access_levels = @(@{ access_level = 40 }) } }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$') { return New-RawResponse 200 @{ name = 'v0.1.3'; target = ('a' * 40); commit = @{ id = $candidate }; message = '1C Team Kit v0.1.3'; created_at = '2026-08-18T00:00:00Z' } }
        if ($Request.Url -match '/releases/v0\.1\.3$') { return New-RawResponse 200 $state.Release }
        throw "unexpected exact-rerun HTTP $($Request.Method) $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $operation = if ($FileName -eq 'git' -and $Arguments[0] -eq '-c') { $Arguments[2] } elseif ($FileName -eq 'git') { $Arguments[0] } else { '' }
        if ($FileName -eq 'git' -and $operation -eq 'push' -and $Arguments -contains '--dry-run') { return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'push') { $state.Mutations.Add('git push'); throw 'an exact published rerun must not push' }
        if ($FileName -eq 'git' -and $operation -eq 'status') { return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'rev-parse') { return [pscustomobject]@{ ExitCode = 0; StdOut = $candidate; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'show') { return [pscustomobject]@{ ExitCode = 0; StdOut = '2026-08-18T00:00:00Z'; StdErr = '' } }
        if ($FileName -notin @('git', 'go')) { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process -UploadFiles $uploads
    $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; GitHubArtifactDigest = ('sha256:' + ('a' * 64)); Hashes = $gitlabPublication.FullHashes }
    $state.Release = @{ name = '1C Team Kit v0.1.3'; tag_name = 'v0.1.3'; commit = @{ id = $candidate }; description = (& (Get-Module BoundedRelease) { param($Context, $CI) New-ReleaseNotes $Context $CI } $context $ci); assets = @{ links = @($uploads | ForEach-Object { @{ name = $_.Name; url = $_.Url; link_type = 'other' } }) }; _links = @{ self = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3' } }
    $result = Publish-TeamKitRelease -Context $context
    Assert-Equal $result.status 'published' 'an already exact and fully proven Release must short-circuit to published'
    Assert-Equal @($state.Mutations).Count 0 'an exact published rerun must perform zero remote or Git mutations'
    Assert-Equal @($result.files).Count 8 'idempotent exact Release verification must still return all eight records'
}

function Test-ExactReleaseShortCircuitRequiresCurrentRefsAndKeptJob {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    $proofStart = $module.IndexOf('function Assert-ExistingReleasePublication')
    $proofEnd = $module.IndexOf('function Assert-BranchFastForward')
    $proof = $module.Substring($proofStart, $proofEnd - $proofStart)
    Assert-True $proof.Contains('artifacts_expire_at') 'existing Release proof must reject an expiring job before idempotent success'
    Assert-True $module.Contains('existing GitLab Release requires both branch refs at candidate') 'existing Release short-circuit must require both current branch refs to equal the candidate'
}

function Test-ExactLocalAnnotatedTagIsReusedAfterPriorRemotePushFailure {
    $candidate = 'b' * 40
    $tagObject = 'a' * 40
    $state = @{ RemoteTagExists = $false; TagCommands = 0; Pushes = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/protected_tags/v0\.1\.3$') { return New-RawResponse 200 @{ name = 'v0.1.3'; create_access_levels = @(@{ access_level = 40 }) } }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$') {
            if (-not $state.RemoteTagExists) { return New-RawResponse 404 }
            return New-RawResponse 200 @{ name = 'v0.1.3'; target = $tagObject; commit = @{ id = $candidate }; message = '1C Team Kit v0.1.3'; created_at = '2026-08-18T00:00:00Z' }
        }
        throw "unexpected local-tag recovery HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $operation = if ($Arguments[0] -eq '-c') { $Arguments[2] } else { $Arguments[0] }
        if ($operation -eq 'for-each-ref') {
            if ($Arguments -match 'contents') { return [pscustomobject]@{ ExitCode = 0; StdOut = '1C Team Kit v0.1.3'; StdErr = '' } }
            return [pscustomobject]@{ ExitCode = 0; StdOut = $tagObject; StdErr = '' }
        }
        if ($operation -eq 'rev-parse') { return [pscustomobject]@{ ExitCode = 0; StdOut = if ($Arguments[-1] -like '*^{tag}') { $tagObject } else { $candidate }; StdErr = '' } }
        if ($operation -eq 'cat-file') { return [pscustomobject]@{ ExitCode = 0; StdOut = 'tag'; StdErr = '' } }
        if ($operation -eq 'tag') { $state.TagCommands++; throw 'existing exact local tag must not be recreated' }
        if ($operation -eq 'push') { $state.Pushes++; $state.RemoteTagExists = $true; return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        throw "unexpected local-tag recovery process $operation"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
    $tag = Publish-ProtectedTag -Context $context -CI @{}
    Assert-Equal $tag.tag_object_sha $tagObject 'exact local annotated tag object must be reused for the remote retry'
    Assert-Equal $state.TagCommands 0 'remote retry must not invoke a second local tag creation'
    Assert-Equal $state.Pushes 1 'exact local annotated tag must receive one non-force retry push'
}

function Test-ExactRemoteAnnotatedTagRecoversWithoutLocalTag {
    $candidate = 'b' * 40
    $object = 'a' * 40
    $processCalls = [System.Collections.Generic.List[string]]::new()
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/protected_tags/v0\.1\.3$') { return New-RawResponse 200 @{ name = 'v0.1.3'; create_access_levels = @(@{ access_level = 40 }) } }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$') { return New-RawResponse 200 @{ name = 'v0.1.3'; target = $object; commit = @{ id = $candidate }; message = '1C Team Kit v0.1.3'; created_at = '2026-08-18T00:00:00Z' } }
        throw "unexpected HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = { param($Context, $FileName, $Arguments, $TimeoutSeconds) $processCalls.Add("$FileName $($Arguments -join ' ')"); throw 'fresh clone must not require local tag' }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
    $tag = Publish-ProtectedTag -Context $context -CI @{}
    Assert-Equal $tag.tag_object_sha $object 'remote annotated tag object'
    Assert-Equal $tag.peeled_commit_sha $candidate 'remote annotated tag peeled commit'
    Assert-Equal $processCalls.Count 0 'exact remote tag recovery uses no local tag'
}

function Test-ExactCIRejectsAmbiguousGitHubRuns {
    $candidate = 'b' * 40
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/pipelines/2/jobs') { return New-RawResponse 200 @(@{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate } }) }
        throw "unexpected HTTP $($Request.Url)"
    }.GetNewClosure()
    $paired = {
        param($Context, $GitHubRequest, $GitLabRequest)
        $run = @{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; conclusion = 'success'; created_at = '2026-08-18T00:00:00Z' }
        return @{ github = (New-RawResponse 200 @{ workflow_runs = @($run, ($run.Clone() | ForEach-Object { $_.id = 2; $_ })) }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'success' })) }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -PairedCIAdapter $paired -SleepAdapter { param($c,$s) throw 'must not poll an ambiguity' }
    $threw = $false; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $threw = $true }
    Assert-True $threw 'ambiguous exact-SHA GitHub runs must be rejected'
}

function Test-ExactCIRejectsAmbiguousGitLabVerifyJobs {
    $candidate = 'b' * 40
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/pipelines/2/jobs') { return New-RawResponse 200 @(@{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate } }, @{ id = 4; name = 'verify'; status = 'success'; commit = @{ id = $candidate } }) }
        throw "unexpected ambiguous-job HTTP $($Request.Url)"
    }.GetNewClosure()
    $paired = {
        param($Context, $GitHubRequest, $GitLabRequest)
        return @{ github = (New-RawResponse 200 @{ workflow_runs = @(@{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; conclusion = 'success'; created_at = '2026-08-18T00:00:00Z' }) }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'success'; created_at = '2026-08-18T00:00:00Z' })) }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -PairedCIAdapter $paired -SleepAdapter { param($c,$s) throw 'ambiguous verify jobs must not poll' }
    $threw = $false; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $threw = $true }
    Assert-True $threw 'ambiguous successful GitLab verify jobs must be rejected rather than selecting one arbitrarily'
}

function Test-ExactCIRejectsAmbiguousFreshRunsBeforeStatusSelection {
    $candidate = 'b' * 40
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/pipelines/2/jobs') { return New-RawResponse 200 @(@{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate; sha = $candidate } }) }
        throw "unexpected CI ambiguity HTTP $($Request.Url)"
    }.GetNewClosure()
    $paired = {
        param($Context, $GitHubRequest, $GitLabRequest)
        return @{ github = (New-RawResponse 200 @{ workflow_runs = @(
            @{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; status = 'queued'; conclusion = $null; created_at = '2026-08-18T00:00:00Z' },
            @{ id = 2; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; status = 'completed'; conclusion = 'success'; created_at = '2026-08-18T00:00:01Z' }
        ) }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'success'; created_at = '2026-08-18T00:00:00Z' })) }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -PairedCIAdapter $paired -SleepAdapter { param($c,$s) throw 'ambiguous fresh CI must not poll' }
    $reason = ''; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $reason = $_.Exception.Message }
    Assert-Equal $reason 'ambiguous exact-SHA GitHub CI runs' 'all fresh correlated GitHub runs, including queued ones, must be checked for ambiguity before status selection'
}

function Test-ExactCIFailsImmediatelyForTerminalProviderFailures {
    $candidate = 'b' * 40
    $githubFailed = {
        param($Context, $GitHubRequest, $GitLabRequest)
        return @{ github = (New-RawResponse 200 @{ workflow_runs = @(@{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; status = 'completed'; conclusion = 'failure'; created_at = '2026-08-18T00:00:00Z' }) }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'success'; created_at = '2026-08-18T00:00:00Z' })) }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -PairedCIAdapter $githubFailed -SleepAdapter { param($c,$s) throw 'terminal GitHub failure must not poll' }
    $reason = ''; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $reason = $_.Exception.Message }
    Assert-Equal $reason 'exact GitHub CI concluded failure' 'a correlated terminal GitHub failure must be an ordinary immediate failure'

    $gitlabCancelled = {
        param($Context, $GitHubRequest, $GitLabRequest)
        return @{ github = (New-RawResponse 200 @{ workflow_runs = @(@{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; status = 'completed'; conclusion = 'success'; created_at = '2026-08-18T00:00:00Z' }) }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'canceled'; created_at = '2026-08-18T00:00:00Z' })) }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -PairedCIAdapter $gitlabCancelled -SleepAdapter { param($c,$s) throw 'terminal GitLab failure must not poll' }
    $reason = ''; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $reason = $_.Exception.Message }
    Assert-Equal $reason 'exact GitLab CI concluded canceled' 'a correlated terminal GitLab cancellation must be an ordinary immediate failure'
}

function Test-ExactCIFailsTerminalCandidateEvenBeforeTheOtherProviderAppears {
    $candidate = 'b' * 40
    $githubFailedWithoutGitLab = {
        param($Context, $GitHubRequest, $GitLabRequest)
        return @{ github = (New-RawResponse 200 @{ workflow_runs = @(@{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; status = 'completed'; conclusion = 'failure'; created_at = '2026-08-18T00:00:00Z' }) }); gitlab = (New-RawResponse 200 @{ id = 99; sha = ('a' * 40); ref = 'master'; source = 'push'; status = 'success'; created_at = '2026-08-18T00:00:00Z' }) }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -PairedCIAdapter $githubFailedWithoutGitLab -SleepAdapter { param($c,$s) throw 'terminal GitHub failure must not wait for GitLab' }
    $reason = ''; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $reason = $_.Exception.Message }
    Assert-Equal $reason 'exact GitHub CI concluded failure' 'a terminal exact GitHub failure must fail before waiting for GitLab correlation'

    $gitlabCancelledWithoutGitHub = {
        param($Context, $GitHubRequest, $GitLabRequest)
        return @{ github = (New-RawResponse 200 @{ workflow_runs = @() }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'canceled'; created_at = '2026-08-18T00:00:00Z' })) }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -PairedCIAdapter $gitlabCancelledWithoutGitHub -SleepAdapter { param($c,$s) throw 'terminal GitLab cancellation must not wait for GitHub' }
    $reason = ''; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $reason = $_.Exception.Message }
    Assert-Equal $reason 'exact GitLab CI concluded canceled' 'a terminal exact GitLab cancellation must fail before waiting for GitHub correlation'
}

function Test-ExactCIEvaluatesBothTerminalStatesBeforeAnyPendingSleep {
    $candidate = 'b' * 40
    foreach ($terminalStatus in @('failed', 'canceled')) {
        $githubPendingGitLabTerminal = {
            param($Context, $GitHubRequest, $GitLabRequest)
            return @{ github = (New-RawResponse 200 @{ workflow_runs = @(@{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; status = 'queued'; conclusion = $null; created_at = '2026-08-18T00:00:00Z' }) }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = $terminalStatus; created_at = '2026-08-18T00:00:00Z' })) }
        }.GetNewClosure()
        $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -PairedCIAdapter $githubPendingGitLabTerminal -SleepAdapter { param($c,$s) throw 'a GitLab terminal pipeline must win before a GitHub pending sleep' }
        $reason = ''; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $reason = $_.Exception.Message }
        Assert-Equal $reason ("exact GitLab CI concluded $terminalStatus") "a GitLab $terminalStatus pipeline must fail before a pending GitHub run sleeps"
    }

    $githubTerminalGitLabPending = {
        param($Context, $GitHubRequest, $GitLabRequest)
        return @{ github = (New-RawResponse 200 @{ workflow_runs = @(@{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; status = 'completed'; conclusion = 'failure'; created_at = '2026-08-18T00:00:00Z' }) }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'running'; created_at = '2026-08-18T00:00:00Z' })) }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -PairedCIAdapter $githubTerminalGitLabPending -SleepAdapter { param($c,$s) throw 'a GitHub terminal run must win before a GitLab pending sleep' }
    $reason = ''; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $reason = $_.Exception.Message }
    Assert-Equal $reason 'exact GitHub CI concluded failure' 'a terminal GitHub failure must fail before a pending GitLab pipeline sleeps'
}

function Test-ExactCIUsesGitLabJobCommitIdShape {
    $candidate = 'b' * 40
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/pipelines/2/jobs') { return New-RawResponse 200 @(@{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate } }) }
        throw "unexpected GitLab real-shape CI HTTP $($Request.Url)"
    }.GetNewClosure()
    $paired = {
        param($Context, $GitHubRequest, $GitLabRequest)
        return @{ github = (New-RawResponse 200 @{ workflow_runs = @(@{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; status = 'completed'; conclusion = 'success'; created_at = '2026-08-18T00:00:00Z' }) }); gitlab = (New-RawResponse 200 @(@{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'success'; created_at = '2026-08-18T00:00:00Z' })) }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -PairedCIAdapter $paired -SleepAdapter { param($c,$s) throw 'real GitLab commit.id verify job must be accepted without polling' }
    $ci = Wait-ExactShaCI -Context $context
    Assert-Equal $ci.gitlab_job_id 3 'fresh CI correlation must use GitLab job commit.id rather than an undocumented commit.sha field'
}

function Test-ExactCIRejectsPreSyncBaselineRun {
    $candidate = 'b' * 40
    $paired = {
        param($Context, $GitHubRequest, $GitLabRequest)
        return @{ github = (New-RawResponse -StatusCode 200 -Value @{ workflow_runs = @(@{ id = 7; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; conclusion = 'success'; created_at = '2099-01-01T00:00:00Z' }) }); gitlab = (New-RawResponse -StatusCode 200 -Value @(@{ id = 8; sha = $candidate; ref = 'master'; source = 'push'; status = 'success'; created_at = '2099-01-01T00:00:00Z' })) }
    }.GetNewClosure()
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/pipelines/8/jobs') { return New-RawResponse 200 @(@{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate } }) }
        throw "unexpected HTTP $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -PairedCIAdapter $paired -SleepAdapter { param($c,$s) throw 'baseline run must not be accepted' } -UtcNowAdapter { param($Context) [datetime]'2026-08-18T00:00:00Z' }
    $context.State.RefSyncStartedAt = [datetime]'2026-08-18T00:00:00Z'
    $context.State.CIGitHubBaselineIds = @{ '7' = $true }
    $context.State.CIGitLabBaselineIds = @{ '8' = $true }
    $threw = $false; $reason = ''; try { Wait-ExactShaCI -Context $context | Out-Null } catch { $reason = $_.Exception.Message; $threw = $true }
    Assert-True $threw "initial CI must reject an exact-SHA run already observed before ref sync: $reason"
    Assert-Equal $reason 'baseline run must not be accepted' 'baseline run must be excluded before the GitLab verify-job lookup'
}

function Test-PublicationArchiveRejectsTraversalEntry {
    $archive = Join-Path ([IO.Path]::GetTempPath()) ('teamkit-traversal-' + [Guid]::NewGuid().ToString('N') + '.zip')
    $stream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew)
    $zip = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Create, $false)
    foreach ($name in @('teamkit-v0.1.3-windows-amd64.exe', 'teamkit-v0.1.3-linux-amd64', 'teamkit-v0.1.3-darwin-amd64', 'teamkit-v0.1.3-darwin-arm64', 'SHA256SUMS', 'SECURITY-AUDIT.json')) {
        $entryName = if ($name -eq 'teamkit-v0.1.3-windows-amd64.exe') { "dist/../dist/$name" } else { "dist/$name" }
        $writer = [IO.StreamWriter]::new($zip.CreateEntry($entryName).Open(), [Text.UTF8Encoding]::new($false)); $writer.Write('x'); $writer.Dispose()
    }
    $zip.Dispose(); $stream.Dispose()
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
    $threw = $false
    try { & (Get-Module BoundedRelease) { param($Context, $Archive) Expand-PublicationArchive $Context $Archive 'malicious' } $context $archive | Out-Null } catch { $threw = $true }
    Assert-True $threw 'traversal-like ZIP entry must be rejected before extraction'
}

function Test-PublicationArchiveRejectsUncompressedBombBeforeExtraction {
    $archive = Join-Path ([IO.Path]::GetTempPath()) ('teamkit-bomb-' + [Guid]::NewGuid().ToString('N') + '.zip')
    $stream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew)
    $zip = [IO.Compression.ZipArchive]::new($stream, [IO.Compression.ZipArchiveMode]::Create, $false)
    foreach ($name in @('teamkit-v0.1.3-windows-amd64.exe', 'teamkit-v0.1.3-linux-amd64', 'teamkit-v0.1.3-darwin-amd64', 'teamkit-v0.1.3-darwin-arm64', 'SHA256SUMS', 'SECURITY-AUDIT.json')) {
        $bytes = if ($name -eq 'teamkit-v0.1.3-windows-amd64.exe') { 'z' * 2048 } else { 'x' }
        $writer = [IO.StreamWriter]::new($zip.CreateEntry("dist/$name").Open(), [Text.UTF8Encoding]::new($false)); $writer.Write($bytes); $writer.Dispose()
    }
    $zip.Dispose(); $stream.Dispose()
    $context = New-ReleaseContext -CandidateSha ('b' * 40) -GitHubToken x -GitLabToken y
    $threw = $false
    try {
        & (Get-Module BoundedRelease) {
            param($Context, $Archive)
            $old = $script:MaxPublicationArchiveUncompressedBytes
            $script:MaxPublicationArchiveUncompressedBytes = 1024
            try { Expand-PublicationArchive $Context $Archive 'bomb' | Out-Null } finally { $script:MaxPublicationArchiveUncompressedBytes = $old }
        } $context $archive
    } catch { $threw = $true }
    Assert-True $threw 'ZIP uncompressed-size bomb must be rejected before extraction'
}

function Test-PublicationArchiveBoundsActualExtractionBytesAsWellAsCentralDirectoryMetadata {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    $start = $module.IndexOf('function Expand-PublicationArchive')
    $end = $module.IndexOf('function Get-PublicationHashMap')
    $archive = $module.Substring($start, $end - $start)
    Assert-True $archive.Contains('$actualEntryBytes') 'ZIP extraction must count bytes actually emitted by each entry stream'
    Assert-True $archive.Contains('$actualUncompressed') 'ZIP extraction must count aggregate bytes actually emitted by streams'
    Assert-True $archive.Contains('actual extracted size') 'ZIP extraction must reject a stream that exceeds declared or aggregate bounds before writing more bytes'
}

function Test-PreflightRejectsConflictingRemoteTagBeforeMutation {
    $candidate = 'b' * 40
    $uploadBytes = [Text.Encoding]::UTF8.GetBytes('fixture-upload')
    $uploadHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadBytes)).ToLowerInvariant()
    $uploads = @(@{ Name = 'fixture.bin'; Url = 'https://fixture.invalid/fixed'; ApiUrl = 'https://fixture.invalid/fixed'; Size = $uploadBytes.Length; Sha256 = $uploadHash })
    $mutations = [System.Collections.Generic.List[string]]::new()
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download') { return New-RawResponse 200 $uploadBytes }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$') { return New-RawResponse 200 @{ name = 'v0.1.3'; target = ('a' * 40); commit = @{ id = ('c' * 40) }; message = 'other tag'; created_at = '2026-08-18T00:00:00Z' } }
        if ($Request.Url -match '/releases/v0\.1\.3$') { return New-RawResponse 404 }
        if ($Request.Url -match '/protected_tags/v0\.1\.3$') { return New-RawResponse 404 }
        if ($Request.Url -match '/repos/mi1man-cmd/kit-all-team$') { return New-RawResponse 200 @{ permissions = @{ push = $true } } }
        if ($Request.Url -match '/projects/12087$') { return New-RawResponse 200 @{ permissions = @{ project_access = @{ access_level = 40 } } } }
        if ($Request.Method -eq 'POST') { $mutations.Add($Request.Url); return New-RawResponse 201 @{} }
        throw "unexpected HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        if ($FileName -eq 'git' -and $Arguments[0] -eq 'rev-parse') { return [pscustomobject]@{ ExitCode = 0; StdOut = $candidate; StdErr = '' } }
        if ($FileName -notin @('git', 'go')) { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        return [pscustomobject]@{ ExitCode = 0; StdOut = if ($Arguments[0] -eq 'show') { '2026-08-18T00:00:00Z' } else { '' }; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process -UploadFiles $uploads
    $threw = $false; try { Invoke-ReleasePreflight -Context $context | Out-Null } catch { $threw = $true }
    Assert-True $threw 'preflight must reject a conflicting existing remote tag'
    Assert-Equal $mutations.Count 0 'preflight conflict must have zero API mutations'
}

function Test-ReserveRechecksExactProtectedTagRuleBeforeTag {
    $candidate = 'b' * 40
    $uploadBytes = [Text.Encoding]::UTF8.GetBytes('fixture-upload')
    $uploadHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadBytes)).ToLowerInvariant()
    $uploads = @(@{ Name = 'fixture.bin'; Url = 'https://fixture.invalid/fixed'; ApiUrl = 'https://fixture.invalid/fixed'; Size = $uploadBytes.Length; Sha256 = $uploadHash })
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download') { return New-RawResponse 200 $uploadBytes }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/jobs/3$') { return New-RawResponse 200 @{ id = 3; artifacts_expire_at = $null } }
        if ($Request.Url -match '/protected_tags/v0\.1\.3$') { return New-RawResponse 200 @{ name = 'v0.1.3'; create_access_levels = @(@{ access_level = 40 }, @{ access_level = 30 }) } }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$' -or $Request.Url -match '/releases/v0\.1\.3$') { return New-RawResponse 404 }
        throw "unexpected reserve HTTP $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -UploadFiles $uploads
    $data = @{ gitlab_job_id = 3; github_run_id = 1; gitlab_pipeline_id = 2; Hashes = @{} }
    $threw = $false
    try { & (Get-Module BoundedRelease) { param($Context, $Data) Invoke-ProductionReleaseStep $Context 'reserve' $Data } $context $data | Out-Null } catch { $threw = $true }
    Assert-True $threw 'reserve must reject a changed protected-tag rule before any tag mutation'
}

function Test-PreflightRejectsTamperedSameCommitReleaseBeforeBranchPush {
    $candidate = 'b' * 40
    $uploadBytes = [Text.Encoding]::UTF8.GetBytes('fixture-upload')
    $uploadHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadBytes)).ToLowerInvariant()
    $uploads = @(@{ Name = 'fixture.bin'; Url = 'https://fixture.invalid/fixed'; ApiUrl = 'https://fixture.invalid/fixed'; Size = $uploadBytes.Length; Sha256 = $uploadHash })
    $branchPushes = [System.Collections.Generic.List[string]]::new()
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download') { return New-RawResponse 200 $uploadBytes }
        if ($Request.Url -match '/repos/mi1man-cmd/kit-all-team$') { return New-RawResponse 200 @{ permissions = @{ push = $true } } }
        if ($Request.Url -match '/git/ref/heads/main$') { return New-RawResponse 200 @{ object = @{ sha = $candidate } } }
        if ($Request.Url -match '/projects/12087$') { return New-RawResponse 200 @{ permissions = @{ project_access = @{ access_level = 40 } } } }
        if ($Request.Url -match '/repository/branches/master$') { return New-RawResponse 200 @{ commit = @{ id = $candidate } } }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$' -or $Request.Url -match '/protected_tags/v0\.1\.3$') { return New-RawResponse 404 }
        if ($Request.Url -match '/releases/v0\.1\.3$') { return New-RawResponse 200 @{ name = '1C Team Kit v0.1.3'; tag_name = 'v0.1.3'; commit = @{ id = $candidate }; description = 'tampered release notes'; assets = @{ links = @() }; _links = @{ self = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3' } } }
        if ($Request.Url -match '/actions/workflows/ci\.yml/runs') { return New-RawResponse 200 @{ workflow_runs = @() } }
        if ($Request.Url -match '/projects/12087/pipelines\?sha=') { return [pscustomobject]@{ StatusCode = 200; Headers = @{}; BodyUtf8 = [Text.Encoding]::UTF8.GetBytes('[]') } }
        throw "unexpected preflight HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        $operation = if ($Arguments[0] -eq '-c') { $Arguments[2] } else { $Arguments[0] }
        if ($FileName -eq 'git' -and $operation -eq 'push' -and $Arguments -contains '--dry-run') { return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' } }
        if ($FileName -eq 'git' -and $operation -eq 'push') { $branchPushes.Add($Arguments[-1]); throw 'branch mutation must not occur' }
        if ($FileName -eq 'git' -and $Arguments[0] -eq 'rev-parse') { return [pscustomobject]@{ ExitCode = 0; StdOut = $candidate; StdErr = '' } }
        if ($FileName -notin @('git', 'go')) { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        return [pscustomobject]@{ ExitCode = 0; StdOut = if ($Arguments[0] -eq 'show') { '2026-08-18T00:00:00Z' } else { '' }; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process -UploadFiles $uploads
    $result = Publish-TeamKitRelease -Context $context
    Assert-Equal $result.exit_code 1 'tampered pre-existing release must be a safe failure'
    Assert-Equal $branchPushes.Count 0 'preflight must reject tampered same-commit release before any branch push'
}

function Test-PreflightExistingReleaseRequiresReadOnlyArtifactProof {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    Assert-True $module.Contains('Assert-ExistingReleasePublication') 'preflight must have a dedicated exact existing-Release proof helper'
    Assert-True $module.Contains('Get-ProductionPublicationSets $Context $releaseCI') 'existing Release proof must redownload and bind its described artifacts before branch sync'
    Assert-True $module.Contains('/actions/runs/$($releaseCI.github_run_id)') 'existing Release proof must bind the described GitHub run to the candidate'
    Assert-True $module.Contains('/pipelines/$($releaseCI.gitlab_pipeline_id)') 'existing Release proof must bind the described GitLab pipeline to the candidate'
}

function Test-ExistingReleaseProofBindsEveryDescribedArtifactHash {
    $candidate = 'b' * 40
    $publication = New-PublicationZip $candidate
    $githubPublication = New-PublicationZip $candidate 'windows fixture' root
    $hashes = @{}
    foreach ($name in $publication.Hashes.Keys) { $hashes[$name] = $publication.Hashes[$name] }
    $manifest = ($publication.Hashes.Keys | Sort-Object | ForEach-Object { "$($publication.Hashes[$_])  $_" }) -join "`n"
    $hashes['SHA256SUMS'] = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($manifest + "`n"))).ToLowerInvariant()
    $audit = (@{ commit = $candidate; passed = $true; findings = @() } | ConvertTo-Json -Compress)
    $hashes['SECURITY-AUDIT.json'] = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($audit))).ToLowerInvariant()
    $state = @{ Release = $null }
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download' -and $Request.Url -eq 'https://fixture.invalid/github.zip') { return New-RawResponse 200 $githubPublication.Bytes }
        if ($Request.Purpose -eq 'download') { return New-RawResponse 200 $publication.Bytes }
        if ($Request.Url -match '/actions/runs/1/artifacts') { return New-RawResponse 200 @{ artifacts = @(@{ name = 'candidate-binaries'; expired = $false; digest = ('sha256:' + ('a' * 64)); archive_download_url = 'https://fixture.invalid/github.zip' }) } }
        if ($Request.Url -match '/actions/runs/1$') { return New-RawResponse 200 @{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; conclusion = 'success' } }
        if ($Request.Url -match '/pipelines/2$') { return New-RawResponse 200 @{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'success' } }
        if ($Request.Url -match '/jobs/3$') { return New-RawResponse 200 @{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate }; pipeline = @{ id = 2 }; artifacts_expire_at = $null } }
        throw "unexpected existing-release proof HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        if ($FileName -notin @('git', 'go')) { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        return [pscustomobject]@{ ExitCode = 0; StdOut = ''; StdErr = '' }
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
    $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; Hashes = $hashes }
    $notes = & (Get-Module BoundedRelease) { param($Context, $CI) New-ReleaseNotes $Context $CI } $context $ci
    $links = @($context.UploadFiles | ForEach-Object { @{ name = $_.Name; url = $_.Url; link_type = 'other' } })
    $state.Release = @{ name = '1C Team Kit v0.1.3'; tag_name = 'v0.1.3'; commit = @{ id = $candidate }; description = $notes; assets = @{ links = $links }; _links = @{ self = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3' } }
    $proof = & (Get-Module BoundedRelease) { param($Context, $Release) Assert-ExistingReleasePublication $Context $Release } $context $state.Release
    Assert-Equal $proof.github_run_id 1 'existing Release proof must preserve the described GitHub run'
    foreach ($name in $hashes.Keys) { Assert-Equal $proof.Hashes[$name] $hashes[$name] "existing Release proof must bind hash $name" }
}

function Test-ExistingReleaseProofBindsReleaseHashesBeforeExecutingArtifact {
    $candidate = 'b' * 40
    $trusted = New-PublicationZip $candidate
    $tamperedGitLab = New-PublicationZip $candidate 'tampered historical GitLab candidate'
    $tamperedGitHub = New-PublicationZip $candidate 'tampered historical GitLab candidate' root
    $state = @{ ExecutableCalls = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download' -and $Request.Url -match '/jobs/3/artifacts$') { return New-RawResponse 200 $tamperedGitLab.Bytes }
        if ($Request.Purpose -eq 'download' -and $Request.Url -eq 'https://fixture.invalid/github.zip') { return New-RawResponse 200 $tamperedGitHub.Bytes }
        if ($Request.Url -match '/actions/runs/1/artifacts') { return New-RawResponse 200 @{ artifacts = @(@{ name = 'candidate-binaries'; expired = $false; digest = ('sha256:' + ('a' * 64)); archive_download_url = 'https://fixture.invalid/github.zip' }) } }
        if ($Request.Url -match '/actions/runs/1$') { return New-RawResponse 200 @{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; conclusion = 'success' } }
        if ($Request.Url -match '/pipelines/2$') { return New-RawResponse 200 @{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'success' } }
        if ($Request.Url -match '/jobs/3$') { return New-RawResponse 200 @{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate }; pipeline = @{ id = 2 }; artifacts_expire_at = $null } }
        throw "unexpected historical release proof HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        if ($FileName -like '*teamkit-v0.1.3-windows-amd64.exe') { $state.ExecutableCalls++; return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        throw "unexpected historical proof process $FileName"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
    $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; Hashes = $trusted.FullHashes }
    $release = @{ name = '1C Team Kit v0.1.3'; tag_name = 'v0.1.3'; commit = @{ id = $candidate }; description = (& (Get-Module BoundedRelease) { param($Context, $CI) New-ReleaseNotes $Context $CI } $context $ci); assets = @{ links = @($context.UploadFiles | ForEach-Object { @{ name = $_.Name; url = $_.Url; link_type = 'other' } }) }; _links = @{ self = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3' } }
    $threw = $false
    try { & (Get-Module BoundedRelease) { param($Context, $Release) Assert-ExistingReleasePublication $Context $Release | Out-Null } $context $release } catch { $threw = $true }
    Assert-True $threw 'existing Release proof must reject changed historical artifact bytes'
    Assert-Equal $state.ExecutableCalls 0 'historical Release hash mismatch must fail before executing the downloaded candidate'
}

function Test-ExistingReleaseProofRejectsJobFromAnotherPipelineBeforeDownload {
    $candidate = 'b' * 40
    $state = @{ Downloads = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download') { $state.Downloads++; return New-RawResponse 200 ([byte[]]@(0)) }
        if ($Request.Url -match '/actions/runs/1$') { return New-RawResponse 200 @{ id = 1; path = '.github/workflows/ci.yml'; head_sha = $candidate; head_branch = 'main'; event = 'push'; conclusion = 'success' } }
        if ($Request.Url -match '/pipelines/2$') { return New-RawResponse 200 @{ id = 2; sha = $candidate; ref = 'master'; source = 'push'; status = 'success' } }
        if ($Request.Url -match '/jobs/3$') { return New-RawResponse 200 @{ id = 3; name = 'verify'; status = 'success'; commit = @{ id = $candidate }; pipeline = @{ id = 99 }; artifacts_expire_at = $null } }
        throw "unexpected existing-release proof HTTP $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http
    $names = @('teamkit-v0.1.3-windows-amd64.exe', 'teamkit-v0.1.3-linux-amd64', 'teamkit-v0.1.3-darwin-amd64', 'teamkit-v0.1.3-darwin-arm64', 'SHA256SUMS', 'SECURITY-AUDIT.json')
    $hashes = @{}
    $letters = @('a', 'b', 'c', 'd', 'e', 'f')
    for ($index = 0; $index -lt 6; $index++) { $hashes[$names[$index]] = $letters[$index] * 64 }
    $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; Hashes = $hashes }
    $release = @{ name = '1C Team Kit v0.1.3'; tag_name = 'v0.1.3'; commit = @{ id = $candidate }; description = (& (Get-Module BoundedRelease) { param($Context, $CI) New-ReleaseNotes $Context $CI } $context $ci); assets = @{ links = @($context.UploadFiles | ForEach-Object { @{ name = $_.Name; url = $_.Url; link_type = 'other' } }) }; _links = @{ self = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3' } }
    $threw = $false
    try { & (Get-Module BoundedRelease) { param($Context, $Release) Assert-ExistingReleasePublication $Context $Release | Out-Null } $context $release } catch { $threw = $true }
    Assert-True $threw 'existing Release proof must reject a verify job owned by another pipeline'
    Assert-Equal $state.Downloads 0 'job/pipeline mismatch must fail before any artifact download'
}

function Test-ExistingIncompleteReleaseIsConflict {
    $candidate = 'b' * 40
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/releases/v0\.1\.3$') {
            return New-RawResponse 200 @{ name = '1C Team Kit v0.1.3'; tag_name = 'v0.1.3'; commit = @{ id = $candidate }; description = 'tampered'; assets = @{ links = @() }; _links = @{ self = 'https://fixture.invalid/release' } }
        }
        throw "unexpected HTTP $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http
    $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; Hashes = @{ 'teamkit-v0.1.3-windows-amd64.exe' = ('a' * 64); 'teamkit-v0.1.3-linux-amd64' = ('b' * 64); 'teamkit-v0.1.3-darwin-amd64' = ('c' * 64); 'teamkit-v0.1.3-darwin-arm64' = ('d' * 64); SHA256SUMS = ('e' * 64); 'SECURITY-AUDIT.json' = ('f' * 64) } }
    $threw = $false; try { Publish-GitLabRelease -Context $context -CI $ci -Tag @{ tag_object_sha = ('a' * 40); peeled_commit_sha = $candidate } | Out-Null } catch { $threw = $true }
    Assert-True $threw 'incomplete existing release must be a conflict'
}

function Test-ReleaseRejectsUnexpectedTerminalUrl {
    $candidate = 'b' * 40
    $uploadBytes = [Text.Encoding]::UTF8.GetBytes('fixture-upload')
    $uploadHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadBytes)).ToLowerInvariant()
    $uploads = @(@{ Name = 'fixture.bin'; Url = 'https://fixture.invalid/fixed'; ApiUrl = 'https://fixture.invalid/fixed'; Size = $uploadBytes.Length; Sha256 = $uploadHash })
    $hashes = @{ 'teamkit-v0.1.3-windows-amd64.exe' = ('a' * 64); 'teamkit-v0.1.3-linux-amd64' = ('b' * 64); 'teamkit-v0.1.3-darwin-amd64' = ('c' * 64); 'teamkit-v0.1.3-darwin-arm64' = ('d' * 64); SHA256SUMS = ('e' * 64); 'SECURITY-AUDIT.json' = ('f' * 64) }
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/releases/v0\.1\.3$') {
            $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; Hashes = $hashes }
            $notes = & (Get-Module BoundedRelease) { param($Context, $CI) New-ReleaseNotes $Context $CI } $Context $ci
            return New-RawResponse 200 @{ name = '1C Team Kit v0.1.3'; tag_name = 'v0.1.3'; commit = @{ id = $candidate }; description = $notes; assets = @{ links = @(@{ name = 'fixture.bin'; url = 'https://fixture.invalid/fixed'; link_type = 'other' }) }; _links = @{ self = 'https://fixture.invalid/untrusted-release' } }
        }
        throw "unexpected HTTP $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -UploadFiles $uploads
    $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; Hashes = $hashes }
    $threw = $false; try { Publish-GitLabRelease -Context $context -CI $ci -Tag @{ tag_object_sha = ('a' * 40); peeled_commit_sha = $candidate } | Out-Null } catch { $threw = $true }
    Assert-True $threw 'release with an unexpected terminal URL must be rejected'
}

function Test-KeptReverificationBindsInitialGitLabHashes {
    $module = Get-Content -LiteralPath $modulePath -Raw -Encoding UTF8
    Assert-True $module.Contains('InitialGitLabHashes') 'kept artifacts must bind to initial GitLab hashes'
}

function Test-PostverifyRejectsAlteredKeptHashesBeforePublicationSuccess {
    $candidate = 'b' * 40
    $publication = New-PublicationZip $candidate 'altered after publication'
    $uploadBytes = [Text.Encoding]::UTF8.GetBytes('postverify upload')
    $uploadTwoBytes = [Text.Encoding]::UTF8.GetBytes('postverify upload two')
    $uploadHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadBytes)).ToLowerInvariant()
    $uploadTwoHash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($uploadTwoBytes)).ToLowerInvariant()
    $uploads = @(@{ Name = 'fixture.bin'; Url = 'https://fixture.invalid/postverify'; ApiUrl = 'https://fixture.invalid/postverify'; Size = $uploadBytes.Length; Sha256 = $uploadHash }, @{ Name = 'fixture-two.bin'; Url = 'https://fixture.invalid/postverify-two'; ApiUrl = 'https://fixture.invalid/postverify-two'; Size = $uploadTwoBytes.Length; Sha256 = $uploadTwoHash })
    $names = @('teamkit-v0.1.3-windows-amd64.exe', 'teamkit-v0.1.3-linux-amd64', 'teamkit-v0.1.3-darwin-amd64', 'teamkit-v0.1.3-darwin-arm64', 'SHA256SUMS', 'SECURITY-AUDIT.json')
    $hashes = @{}
    $state = @{ Release = $null }
    foreach ($name in $names) { $hashes[$name] = 'a' * 64 }
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download' -and $Request.Url -match '/jobs/3/artifacts$') { return New-RawResponse 200 $publication.Bytes }
        if ($Request.Purpose -eq 'download' -and $Request.Url -eq 'https://fixture.invalid/postverify') { return New-RawResponse 200 $uploadBytes }
        if ($Request.Purpose -eq 'download' -and $Request.Url -eq 'https://fixture.invalid/postverify-two') { return New-RawResponse 200 $uploadTwoBytes }
        if ($Request.Url -match '/releases/v0\.1\.3$') { return New-RawResponse 200 $state.Release }
        throw "unexpected postverify HTTP $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -UploadFiles $uploads
    $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; GitHubArtifactDigest = ('sha256:' + ('a' * 64)); Hashes = $hashes }
    $state.Release = @{ name = '1C Team Kit v0.1.3'; tag_name = 'v0.1.3'; commit = @{ id = $candidate }; description = (& (Get-Module BoundedRelease) { param($Context, $CI) New-ReleaseNotes $Context $CI } $context $ci); assets = @{ links = @(@{ name = 'fixture.bin'; url = 'https://fixture.invalid/postverify'; link_type = 'other' }, @{ name = 'fixture-two.bin'; url = 'https://fixture.invalid/postverify-two'; link_type = 'other' }) }; _links = @{ self = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3' } }
    $data = @{ CI = $ci; Release = @{ url = $state.Release._links.self }; Tag = @{ tag_object_sha = ('c' * 40); peeled_commit_sha = $candidate } }
    $threw = $false; $failure = ''
    try { & (Get-Module BoundedRelease) { param($Context, $Data) Invoke-ProductionReleaseStep $Context 'verify-eight' $Data | Out-Null } $context $data } catch { $threw = $true; $failure = $_.Exception.Message }
    Assert-True $threw 'postverification must reject a kept artifact whose hash changed after Release creation'
    Assert-True ($failure -match 'kept GitLab artifact changed') "postverification rejection must be the hash binding rather than an unrelated fixture error: $failure"
}

function Test-PostverifyRereadsExactProtectedRuleAndAnnotatedTag {
    $candidate = 'b' * 40
    $publication = New-PublicationZip $candidate
    $state = @{ RuleReads = 0; TagReads = 0 }
    $http = {
        param($Context, $Request)
        if ($Request.Purpose -eq 'download' -and $Request.Url -match '/jobs/3/artifacts$') { return New-RawResponse 200 $publication.Bytes }
        if ($Request.Url -match '/protected_tags/v0\.1\.3$') { $state.RuleReads++; return New-RawResponse 200 @{ name = 'v0.1.3'; create_access_levels = @(@{ access_level = 40 }) } }
        if ($Request.Url -match '/repository/tags/v0\.1\.3$') { $state.TagReads++; return New-RawResponse 200 @{ name = 'v0.1.3'; target = ('d' * 40); commit = @{ id = $candidate }; message = '1C Team Kit v0.1.3'; created_at = '2026-08-18T00:00:00Z' } }
        throw "unexpected postverify rule/tag HTTP $($Request.Url)"
    }.GetNewClosure()
    $process = {
        param($Context, $FileName, $Arguments, $TimeoutSeconds)
        if ($FileName -like '*teamkit-v0.1.3-windows-amd64.exe') { return [pscustomobject]@{ ExitCode = 0; StdOut = "{`"version`":`"v0.1.3`",`"commit`":`"$candidate`"}"; StdErr = '' } }
        throw "unexpected process $FileName"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -ProcessAdapter $process
    $ci = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; GitHubArtifactDigest = ('sha256:' + ('a' * 64)); Hashes = $publication.FullHashes }
    $data = @{ CI = $ci; Release = @{ url = 'https://gitlab.example.invalid/1c/aisuz/ai/-/releases/v0.1.3' }; Tag = @{ tag_object_sha = ('a' * 40); peeled_commit_sha = $candidate } }
    $failure = ''
    try { & (Get-Module BoundedRelease) { param($Context, $Data) Invoke-ProductionReleaseStep $Context 'verify-eight' $Data | Out-Null } $context $data } catch { $failure = $_.Exception.Message }
    Assert-Equal $state.RuleReads 1 'postverification must reread the protected tag rule'
    Assert-Equal $state.TagReads 1 'postverification must reread the remote annotated tag'
    Assert-True ($failure -match 'annotated tag identity changed') "postverification must reject a tag whose object changed after publication: $failure"
}

function Test-FinalValidationRejectsOldSameShaSuccess {
    $candidate = 'b' * 40
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/dispatches$') { return New-RawResponse 204 }
        if ($Request.Url -match '/workflows/release\.yml/runs') {
            return New-RawResponse 200 @{ workflow_runs = @(@{ id = 7; path = '.github/workflows/release.yml'; head_sha = $candidate; head_branch = 'main'; event = 'workflow_dispatch'; conclusion = 'success'; created_at = '2026-08-18T00:00:00Z' }) }
        }
        throw "unexpected HTTP $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -SleepAdapter { param($c,$s) throw 'old success must not be accepted' }
    $data = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; GitHubArtifactDigest = ('sha256:' + ('a' * 64)); Hashes = @{ 'teamkit-v0.1.3-windows-amd64.exe' = ('a' * 64); 'teamkit-v0.1.3-linux-amd64' = ('b' * 64); 'teamkit-v0.1.3-darwin-amd64' = ('c' * 64); 'teamkit-v0.1.3-darwin-arm64' = ('d' * 64); SHA256SUMS = ('e' * 64); 'SECURITY-AUDIT.json' = ('f' * 64) } }
    $threw = $false; try { & (Get-Module BoundedRelease) { param($Context, $Data) Invoke-ProductionReleaseStep $Context 'final-validation' $Data } $context $data | Out-Null } catch { $threw = $true }
    Assert-True $threw 'pre-dispatch final-validation run must be rejected'
}

function Test-FinalValidationRejectsNewIdCreatedBeforeDispatch {
    $candidate = 'b' * 40
    $state = @{ Dispatched = $false }
    $http = {
        param($Context, $Request)
        if ($Request.Url -match '/dispatches$') { $state.Dispatched = $true; return New-RawResponse 204 }
        if ($Request.Url -match '/workflows/release\.yml/runs') {
            $runs = if ($state.Dispatched) { @(@{ id = 9; path = '.github/workflows/release.yml'; head_sha = $candidate; head_branch = 'main'; event = 'workflow_dispatch'; conclusion = 'success'; created_at = '2000-01-01T00:00:00Z' }) } else { @() }
            return New-RawResponse 200 @{ workflow_runs = $runs }
        }
        throw "unexpected HTTP $($Request.Url)"
    }.GetNewClosure()
    $context = New-ReleaseContext -CandidateSha $candidate -GitHubToken x -GitLabToken y -HttpAdapter $http -SleepAdapter { param($c,$s) throw 'pre-dispatch timestamp must not be accepted' } -UtcNowAdapter { param($Context) [datetime]'2026-08-18T00:00:00Z' }
    $data = @{ github_run_id = 1; gitlab_pipeline_id = 2; gitlab_job_id = 3; GitHubArtifactDigest = ('sha256:' + ('a' * 64)); Hashes = @{ 'teamkit-v0.1.3-windows-amd64.exe' = ('a' * 64); 'teamkit-v0.1.3-linux-amd64' = ('b' * 64); 'teamkit-v0.1.3-darwin-amd64' = ('c' * 64); 'teamkit-v0.1.3-darwin-arm64' = ('d' * 64); SHA256SUMS = ('e' * 64); 'SECURITY-AUDIT.json' = ('f' * 64) } }
    $threw = $false; $failure = ''; try { & (Get-Module BoundedRelease) { param($Context, $Data) Invoke-ProductionReleaseStep $Context 'final-validation' $Data } $context $data | Out-Null } catch { $failure = $_.Exception.Message; $threw = $true }
    Assert-True $state.Dispatched "test must reach the real final-validation dispatch: $failure"
    Assert-True $threw 'a post-dispatch ID with a pre-dispatch timestamp must be rejected'
}

function Test-V015FinalValidationUsesNoGitHubDispatch {
    $candidate = 'b' * 40
    $context = New-ReleaseContext -CandidateSha $candidate -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabVerifyJobId 202 -HttpAdapter { param($Context, $Request) throw "unexpected GitHub request $($Request.Method) $($Request.Url)" }
    $hashes = @{}
    $index = 0
    foreach ($name in $context.ReleaseFiles) { $hashes[$name] = ('{0:x64}' -f (++$index)) }
    $data = @{ GitHubArtifactDigest = ('sha256:' + ('a' * 64)); Hashes = $hashes; github_run_id = 101; gitlab_pipeline_id = 201; gitlab_job_id = 202 }
    $result = & (Get-Module BoundedRelease) { param($Context, $Data) Invoke-ProductionReleaseStep $Context 'final-validation' $Data } $context $data
    Assert-Equal $result $data 'v0.1.5 final validation must complete from GitLab evidence without GitHub dispatch'
}

function Test-V015HandoffArchiveExtractsExactSixFilesAndManifest {
    $candidate = 'b' * 40
    $fixture = New-PublicationZip -Candidate $candidate -Layout root -Version v0.1.5
    $archive = Join-Path ([IO.Path]::GetTempPath()) ('teamkit-handoff-' + [Guid]::NewGuid().ToString('N') + '.zip')
    $source = [IO.MemoryStream]::new($fixture.Bytes)
    $sourceZip = [IO.Compression.ZipArchive]::new($source, [IO.Compression.ZipArchiveMode]::Read, $false)
    $target = [IO.File]::Open($archive, [IO.FileMode]::CreateNew)
    $targetZip = [IO.Compression.ZipArchive]::new($target, [IO.Compression.ZipArchiveMode]::Create, $false)
    foreach ($entry in $sourceZip.Entries) {
        $copy = $targetZip.CreateEntry("handoff/$($entry.FullName)")
        $input = $entry.Open(); $output = $copy.Open(); $input.CopyTo($output); $output.Dispose(); $input.Dispose()
    }
    $manifest = @{ commit = $candidate; version = 'v0.1.5'; github_run_id = 101; github_artifact_id = 102; github_artifact_digest = ('sha256:' + ('a' * 64)); sha256_file = 'SHA256SUMS.handoff' } | ConvertTo-Json -Compress
    $writer = [IO.StreamWriter]::new($targetZip.CreateEntry('handoff/MANIFEST.json').Open(), [Text.UTF8Encoding]::new($false)); $writer.Write($manifest); $writer.Dispose()
    $hashRows = ($fixture.FullHashes.Keys | Sort-Object | ForEach-Object { "$($fixture.FullHashes[$_])  $_" }) -join "`n"
    $writer = [IO.StreamWriter]::new($targetZip.CreateEntry('handoff/SHA256SUMS.handoff').Open(), [Text.UTF8Encoding]::new($false)); $writer.Write($hashRows + "`n"); $writer.Dispose()
    $targetZip.Dispose(); $target.Dispose(); $sourceZip.Dispose(); $source.Dispose()
    $context = New-ReleaseContext -CandidateSha $candidate -Version v0.1.5 -GitHubToken x -GitLabToken y -GitHubRunId 101 -GitHubArtifactId 102 -GitLabPipelineId 201 -GitLabHandoffJobId 202 -GitLabVerifyJobId 203
    $result = & (Get-Module BoundedRelease) { param($Context, $Archive) Expand-HandoffPublicationArchive $Context $Archive } $context $archive
    Assert-Equal $result.Files.Count 6 'handoff parser must return exactly six release files'
    [IO.File]::Delete($archive)
}

if (-not [string]::IsNullOrWhiteSpace($Only)) {
    $testCommand = Get-Command -Name $Only -CommandType Function -ErrorAction Stop
    & $testCommand
    Write-Output "bounded release simulation: PASS ($Only)"
    return
}

Test-NarrowProductionSeamsAreAccepted
Test-NullSecurityAuditFindingsMeansZeroFindings
Test-FinalValidationPayloadContract
Test-BoundedProcessDoesNotDeadlockOrLeakUnboundedOutput
Test-BoundedProcessKillsHangingChildAtGlobalDeadline
Test-BoundedProcessDeadlineDoesNotWaitForDescendantHeldPipe
Test-RealChildProcessDoesNotReceiveReleaseTokenCanaries
Test-WindowsJobLauncherPreservesArgumentListRoundTrip
Test-WindowsJobAttachmentFailureNeverResumesChild
Test-DefaultProcessFailsClosedOutsideWindowsContainment
Test-BoundedProcessKillsContinualOutputOverflow
Test-ReserveRequiresTheFull1200SecondBudget
Test-PublishDurationUsesInjectedClock
Test-ContextCarriesTheEntryStopwatchIntoTerminalDuration
Test-DefaultDownloadStreamsAndBoundsCompressedArtifactBytes
Test-SignedArtifactDownloadPreservesRedirectAllowanceWithoutAuth
Test-SignedArtifactDownloadRetriesTemporaryForbiddenSignedTarget
Test-SignedArtifactDownloadWaitsForSignedTargetNotBeforeTime
Test-DefaultPairedCIProbeBoundsBothConcurrentResponses
Test-DefaultApiAndCIStreamsHaveCancelableDeadlineReads
Test-DefaultTransportConstructsTheRequestedHttpMethodBeforeNetworkFailure
Test-GitMutationsDisableGlobalSigningPrompts
Test-PreflightUsesBoundedNonForceGitDryRunProbes
Test-ReadOnlyApiAuthorityFailsClosedWithoutClassicGitHubRepoScope
Test-ReadOnlyApiAuthorityAcceptsDocumentedGitHubAndGitLabProofs
Test-PreflightStopsBeforeGitDryRunWhenApiAuthorityIsUnproven
Test-PreflightRejectsMissingOrDisabledCandidateCIWorkflowBeforeMutation
Test-PreflightRejectsWrongLocalAnnotatedTagBeforeAnyRemoteMutation
Test-ProductionCanarySinksAreNarrow
Test-ProtectedTagRuleRejectsAnyAdditionalCreator
Test-HttpSeamReceivesSerializedNestedPayload
Test-EntryWritesOneJsonWhenOutputPathIsUnwritable
Test-EntryNeverPromptsAndAlwaysEmitsOneJsonForInvalidArguments
Test-ProductionFixtureCanUseVerifiedFixedUploadInputs
Test-FixedUploadsUseAuthenticatedGitLabApiButKeepBrowserReleaseLinks
Test-GitLabArtifactDownloadsPermitSafeSignedRedirects
Test-GitHubCandidateArchiveUsesRootFileLayout
Test-GitLabArchiveAcceptsSafeDistDirectoryMarker
Test-GitLabExecutableWaitsForGitHubSixFileBinding
Test-V015ComparePreparationDoesNotDownloadGitHubArchive
Test-ProductionSuccessUsesNarrowSeams
Test-TerminalFailureUsesStableStageReasonAndRemoteTagForwardRecovery
Test-TerminalDeadlineUsesDistinctReasonCodeAndStage
Test-PublisherRechecksReadOnlyApiAuthorityBeforeTagAndRelease
Test-ExactTagForwardRecoveryCreatesMissingReleaseWithoutRetagging
Test-SyncStopsBeforeSecondPushOnGitHubReadbackMismatch
Test-NoopCandidateRefsDispatchFreshCIOnBothProviders
Test-V015ContextBindsPackageAndVerifiedCIInputs
Test-V015ReleaseAssetsUseHumanReadablePlatformLabels
Test-V015VerifyOnlyFlowRevalidatesProvidedCIWithoutDispatch
Test-V015RejectsPushRunBeforeAnyMutation
Test-V015GenericPackageUploadsThenAuthenticatedRedownloadsExactSix
Test-V015UnexpectedExistingPackageOnLaterPageStopsBeforeAnyUpload
Test-V015DuplicatePackageRecordsStopBeforeAnyUpload
Test-V015ConcurrentExtraDuplicateOrWrongApiHashStopsAfterOnePut
Test-V015PackageUploadRequiresExact201WithoutRetry
Test-V015PostPackageRefMoveStopsBeforeTagMutation
Test-V015PostPackageLateExtraFileStopsBeforeTagMutation
Test-ExactReleaseRerunShortCircuitsWithZeroMutations
Test-ExactReleaseShortCircuitRequiresCurrentRefsAndKeptJob
Test-ExactLocalAnnotatedTagIsReusedAfterPriorRemotePushFailure
Test-ExactRemoteAnnotatedTagRecoversWithoutLocalTag
Test-ExactCIRejectsAmbiguousGitHubRuns
Test-ExactCIRejectsAmbiguousGitLabVerifyJobs
Test-ExactCIRejectsAmbiguousFreshRunsBeforeStatusSelection
Test-ExactCIFailsImmediatelyForTerminalProviderFailures
Test-ExactCIFailsTerminalCandidateEvenBeforeTheOtherProviderAppears
Test-ExactCIEvaluatesBothTerminalStatesBeforeAnyPendingSleep
Test-ExactCIUsesGitLabJobCommitIdShape
Test-ExactCIRejectsPreSyncBaselineRun
Test-PublicationArchiveRejectsTraversalEntry
Test-PublicationArchiveRejectsUncompressedBombBeforeExtraction
Test-PublicationArchiveBoundsActualExtractionBytesAsWellAsCentralDirectoryMetadata
Test-PreflightRejectsConflictingRemoteTagBeforeMutation
Test-ReserveRechecksExactProtectedTagRuleBeforeTag
Test-PreflightRejectsTamperedSameCommitReleaseBeforeBranchPush
Test-PreflightExistingReleaseRequiresReadOnlyArtifactProof
Test-ExistingReleaseProofBindsEveryDescribedArtifactHash
Test-ExistingReleaseProofBindsReleaseHashesBeforeExecutingArtifact
Test-ExistingReleaseProofRejectsJobFromAnotherPipelineBeforeDownload
Test-ExistingIncompleteReleaseIsConflict
Test-ReleaseRejectsUnexpectedTerminalUrl
Test-KeptReverificationBindsInitialGitLabHashes
Test-PostverifyRejectsAlteredKeptHashesBeforePublicationSuccess
Test-PostverifyRereadsExactProtectedRuleAndAnnotatedTag
Test-FinalValidationRejectsOldSameShaSuccess
Test-FinalValidationRejectsNewIdCreatedBeforeDispatch
Test-V015FinalValidationUsesNoGitHubDispatch
Test-V015HandoffArchiveExtractsExactSixFilesAndManifest
Write-Output 'bounded release simulations: PASS'
