[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$SourceDirectory,
    [string]$WorkspaceRoot = (Split-Path -Parent $PSScriptRoot),
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{40}$')][string]$CanonicalSha,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{40}$')][string]$PublicSha,
    [Parameter(Mandatory)][ValidateRange(1,[long]::MaxValue)][long]$GitHubRunId,
    [Parameter(Mandatory)][ValidateRange(1,[long]::MaxValue)][long]$GitHubArtifactId
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
try {
    Import-Module (Join-Path $PSScriptRoot 'release/LocalStaging.psm1') -Force
    $result = Import-V016LocalCandidate -SourceDirectory $SourceDirectory -WorkspaceRoot $WorkspaceRoot -CanonicalSha $CanonicalSha -PublicSha $PublicSha -GitHubRunId $GitHubRunId -GitHubArtifactId $GitHubArtifactId
    [ordered]@{ status = 'staged'; version = 'v0.1.6'; canonical_sha = $CanonicalSha; staging_path = $result.StagingPath; file_count = 6 } | ConvertTo-Json -Compress
} catch {
    [Console]::Error.WriteLine('V016_LOCAL_STAGING_FAILED')
    exit 1
}
