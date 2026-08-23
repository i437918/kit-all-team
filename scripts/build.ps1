[CmdletBinding()]
param(
    [string]$Version = "v0.1.5",
    [string]$OutputDir = "dist"
)

$ErrorActionPreference = "Stop"
$RepositoryRoot = Split-Path -Parent $PSScriptRoot
$Destination = Join-Path $RepositoryRoot $OutputDir
$Go = (Get-Command go -ErrorAction Stop).Source

Push-Location -LiteralPath $RepositoryRoot
try {
    $Dirty = & git status --porcelain --untracked-files=all
    if ($LASTEXITCODE -ne 0) { throw "git status failed" }
    if ($Dirty) { throw "SOURCE_TREE_DIRTY" }
    $SourceRevision = [Environment]::GetEnvironmentVariable("TEAMKIT_SOURCE_REVISION")
    $SourceCommitTime = [Environment]::GetEnvironmentVariable("TEAMKIT_SOURCE_COMMIT_TIME")
    if (-not [string]::IsNullOrEmpty($SourceRevision) -or -not [string]::IsNullOrEmpty($SourceCommitTime)) {
        if ([string]::IsNullOrEmpty($SourceRevision) -or [string]::IsNullOrEmpty($SourceCommitTime) -or $SourceRevision -notcmatch "^[0-9a-f]{40}$") {
            throw "SOURCE_IDENTITY_INVALID"
        }
        [DateTimeOffset]$ParsedCommitTime = [DateTimeOffset]::MinValue
        $CommitTimeFormats = @("yyyy-MM-dd'T'HH:mm:ssK", "yyyy-MM-dd'T'HH:mm:ss.FFFFFFFK")
        if (-not [DateTimeOffset]::TryParseExact($SourceCommitTime, $CommitTimeFormats, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::None, [ref]$ParsedCommitTime)) {
            throw "SOURCE_IDENTITY_INVALID"
        }
        $Commit = $SourceRevision
        $BuildDate = $SourceCommitTime
    }
    else {
        $Commit = (& git rev-parse HEAD).Trim()
        $BuildDate = (& git show -s --format=%cI $Commit).Trim()
    }

    New-Item -ItemType Directory -Path $Destination -Force | Out-Null
    $env:CGO_ENABLED = "0"
    $LdFlags = "-s -w -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.version=$Version -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.commit=$Commit -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.buildDate=$BuildDate"

    $Targets = @(
        @{ OS = "windows"; Arch = "amd64"; File = "teamkit-${Version}-windows-amd64.exe" },
        @{ OS = "linux"; Arch = "amd64"; File = "teamkit-${Version}-linux-amd64" },
        @{ OS = "darwin"; Arch = "amd64"; File = "teamkit-${Version}-darwin-amd64" },
        @{ OS = "darwin"; Arch = "arm64"; File = "teamkit-${Version}-darwin-arm64" }
    )

    foreach ($Target in $Targets) {
        $env:GOOS = $Target.OS
        $env:GOARCH = $Target.Arch
        $Output = Join-Path $Destination $Target.File
        & $Go build -buildvcs=false -trimpath -ldflags $LdFlags -o $Output ./cmd/teamkit
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $($Target.OS)/$($Target.Arch)" }
    }

    $HashLines = $Targets |
        ForEach-Object {
            $Hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $Destination $_.File)).Hash.ToLowerInvariant()
            "$Hash  $($_.File)"
        } |
        Sort-Object

    $HashLines | Set-Content -LiteralPath (Join-Path $Destination "SHA256SUMS") -Encoding ascii
    Write-Output "Built $($Targets.Count) artifacts in $Destination"
}
finally {
    Pop-Location
}
