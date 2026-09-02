[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$')]
    [string]$Version,

    [string]$OutputDir = "dist\windows-installer",
    [string]$IsccPath,
    [string]$CertificateThumbprint,
    [ValidateSet('Signed', 'InternalUnsigned')]
    [string]$SigningMode = 'Signed',
    [string]$TimestampServer = "https://timestamp.digicert.com"
)

$ErrorActionPreference = "Stop"
$RepositoryRoot = Split-Path -Parent $PSScriptRoot
$VersionNumber = $Version.TrimStart('v')
$Destination = [System.IO.Path]::GetFullPath((Join-Path $RepositoryRoot $OutputDir))
$Staging = Join-Path $Destination "staging"
$Binary = Join-Path $Staging "teamkit.exe"
$TeamKitGuiSource = Join-Path $RepositoryRoot "packaging\windows\TeamKitGUI.ps1"
$TeamKitBundleSource = Join-Path $RepositoryRoot "packaging\windows\Invoke-TeamKitBundle.ps1"
$TeamKitGui = Join-Path $Staging "TeamKitGUI.ps1"
$TeamKitBundle = Join-Path $Staging "Invoke-TeamKitBundle.ps1"
$Installer = Join-Path $Destination "TeamKitSetup-$VersionNumber.exe"
$Manifest = Join-Path $Destination "WINDOWS-INSTALLER-MANIFEST.json"
$InnoScript = Join-Path $RepositoryRoot "packaging\windows\TeamKitSetup.iss"
$Verifier = Join-Path $PSScriptRoot "verify-windows-installer.ps1"

function Resolve-Iscc {
    param([string]$ExplicitPath)

    if ($ExplicitPath) {
        if (-not (Test-Path -LiteralPath $ExplicitPath -PathType Leaf)) {
            throw "ISCC_NOT_FOUND: $ExplicitPath"
        }
        return (Resolve-Path -LiteralPath $ExplicitPath).Path
    }

    $command = Get-Command "ISCC.exe" -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }

    foreach ($candidate in @(
        (Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 6\ISCC.exe"),
        (Join-Path $env:ProgramFiles "Inno Setup 6\ISCC.exe"),
        (Join-Path ${env:ProgramFiles(x86)} "Inno Setup 6\ISCC.exe")
    )) {
        if ($candidate -and (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            return $candidate
        }
    }
    throw "ISCC_NOT_FOUND: install Inno Setup 6 or pass -IsccPath"
}

function Sign-ReleaseFile {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][System.Security.Cryptography.X509Certificates.X509Certificate2]$Certificate
    )

    $signed = Set-AuthenticodeSignature -LiteralPath $Path -Certificate $Certificate -TimestampServer $TimestampServer -HashAlgorithm SHA256
    if ($signed.Status -ne "Valid") { throw "SIGNING_FAILED: path=$Path status=$($signed.Status) message=$($signed.StatusMessage)" }

    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne "Valid") { throw "SIGNATURE_INVALID: path=$Path status=$($signature.Status)" }
    if ($signature.SignerCertificate.Thumbprint -ne $Certificate.Thumbprint) {
        throw "SIGNER_THUMBPRINT_MISMATCH: path=$Path expected=$($Certificate.Thumbprint) actual=$($signature.SignerCertificate.Thumbprint)"
    }
    return $signature
}

Push-Location -LiteralPath $RepositoryRoot
$OldCgo = $env:CGO_ENABLED
$OldGoos = $env:GOOS
$OldGoarch = $env:GOARCH
try {
    $Dirty = & git status --porcelain --untracked-files=all
    if ($LASTEXITCODE -ne 0) { throw "git status failed" }
    if ($Dirty) { throw "SOURCE_TREE_DIRTY" }

    $Commit = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw "git rev-parse failed" }
    $BuildDate = (& git show -s --format=%cI $Commit).Trim()
    if ($LASTEXITCODE -ne 0) { throw "git show failed" }

    $Certificate = $null
    if ($SigningMode -eq 'Signed') {
        if ([string]::IsNullOrWhiteSpace($CertificateThumbprint)) { throw "SIGNING_CERTIFICATE_THUMBPRINT_REQUIRED" }
        if ($CertificateThumbprint -notmatch '^[0-9A-Fa-f]{40}$') { throw "SIGNING_CERTIFICATE_THUMBPRINT_INVALID" }
        $Certificate = Get-Item -LiteralPath "Cert:\CurrentUser\My\$CertificateThumbprint" -ErrorAction Stop
        if (-not $Certificate.HasPrivateKey) { throw "SIGNING_CERTIFICATE_PRIVATE_KEY_REQUIRED" }
    }

    New-Item -ItemType Directory -Path $Staging -Force | Out-Null
    Copy-Item -LiteralPath $TeamKitGuiSource -Destination $TeamKitGui -Force
    Copy-Item -LiteralPath $TeamKitBundleSource -Destination $TeamKitBundle -Force
    if ($SigningMode -eq 'Signed') {
        $null = Sign-ReleaseFile -Path $TeamKitGui -Certificate $Certificate
        $null = Sign-ReleaseFile -Path $TeamKitBundle -Certificate $Certificate
    }
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $LdFlags = "-s -w -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.version=v$VersionNumber -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.commit=$Commit -X github.com/mi1man-cmd/kit-all-team/internal/buildinfo.buildDate=$BuildDate"

    & go build -buildvcs=false -trimpath -ldflags $LdFlags -o $Binary ./cmd/teamkit
    if ($LASTEXITCODE -ne 0) { throw "GO_BUILD_FAILED" }

    $Iscc = Resolve-Iscc -ExplicitPath $IsccPath
    $InnoVersion = (Get-Item -LiteralPath $Iscc).VersionInfo.ProductVersion.Trim()
    $GoVersion = (& go version).Trim()
    if ($LASTEXITCODE -ne 0) { throw "GO_VERSION_FAILED" }
    & $Iscc "/DAppVersion=$VersionNumber" "/DTeamKitExe=$Binary" "/DTeamKitGui=$TeamKitGui" "/DTeamKitBundle=$TeamKitBundle" "/DOutputDir=$Destination" $InnoScript
    if ($LASTEXITCODE -ne 0) { throw "INNO_BUILD_FAILED" }
    if (-not (Test-Path -LiteralPath $Installer -PathType Leaf)) { throw "INSTALLER_NOT_CREATED" }

    $Signature = $null
    $Signer = [ordered]@{
        status = 'NotSigned'
        policy = 'internal-unsigned'
    }
    if ($SigningMode -eq 'Signed') {
        $Signature = Sign-ReleaseFile -Path $Installer -Certificate $Certificate
        $Signer = [ordered]@{
            thumbprint = $Signature.SignerCertificate.Thumbprint
            subject = $Signature.SignerCertificate.Subject
            status = [string]$Signature.Status
        }
    }
    $Hash = (Get-FileHash -LiteralPath $Installer -Algorithm SHA256).Hash.ToLowerInvariant()
    "$Hash  $(Split-Path -Leaf $Installer)" | Set-Content -LiteralPath (Join-Path $Destination "SHA256SUMS") -Encoding ascii

    [ordered]@{
        schemaVersion = "1"
        teamKitVersion = "v$VersionNumber"
        sourceCommit = $Commit
        installer = [ordered]@{
            filename = Split-Path -Leaf $Installer
            sizeBytes = (Get-Item -LiteralPath $Installer).Length
            sha256 = $Hash
        }
        signer = $Signer
        innoVersion = $InnoVersion
        goVersion = $GoVersion
    } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $Manifest -Encoding utf8

    $ChecksumPath = Join-Path $Destination "SHA256SUMS"
    if ($SigningMode -eq 'Signed') {
        & $Verifier -InstallerPath $Installer -Version $VersionNumber -ChecksumPath $ChecksumPath -ManifestPath $Manifest -ExpectedSignerThumbprint $Certificate.Thumbprint -RequireSignature
    } else {
        & $Verifier -InstallerPath $Installer -Version $VersionNumber -ChecksumPath $ChecksumPath -ManifestPath $Manifest -RequireUnsigned
    }
    if ($LASTEXITCODE -ne 0) { throw "INSTALLER_VERIFICATION_FAILED" }

    Remove-Item -LiteralPath $Staging -Recurse -Force
    Write-Output "INSTALLER_BUILT path=$Installer sha256=$Hash signature=$($Signer.status)"
}
finally {
    $env:CGO_ENABLED = $OldCgo
    $env:GOOS = $OldGoos
    $env:GOARCH = $OldGoarch
    Pop-Location
}
