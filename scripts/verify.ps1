[CmdletBinding()]
param([switch]$SkipRace)

$ErrorActionPreference = "Stop"
$RepositoryRoot = Split-Path -Parent $PSScriptRoot
Push-Location $RepositoryRoot
try {
    $GoFiles = & git ls-files --cached --others --exclude-standard -- "*.go"
    if ($LASTEXITCODE -ne 0) { throw "git ls-files failed" }
    $Unformatted = if ($GoFiles) { & gofmt -l @GoFiles } else { @() }
    if ($LASTEXITCODE -ne 0) { throw "gofmt failed" }
    if ($Unformatted) { throw "gofmt required: $($Unformatted -join ', ')" }

    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test failed" }
    if (-not $SkipRace) {
        & go test -race ./...
        if ($LASTEXITCODE -ne 0) { throw "go test -race failed" }
    }
} finally {
    Pop-Location
}
