[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateScript({ Test-Path -LiteralPath $_ -PathType Leaf })]
    [string]$InstallerPath,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$')]
    [string]$Version,

    [string]$ChecksumPath,
    [string]$ManifestPath,
    [string]$ExpectedSignerThumbprint,
    [switch]$RequireSignature
)

$ErrorActionPreference = "Stop"
$Installer = (Resolve-Path -LiteralPath $InstallerPath).Path
$VersionNumber = $Version.TrimStart('v')
if (-not $ChecksumPath) {
    $ChecksumPath = Join-Path (Split-Path -Parent $Installer) "SHA256SUMS"
}
if (-not (Test-Path -LiteralPath $ChecksumPath -PathType Leaf)) {
    throw "CHECKSUM_FILE_NOT_FOUND: $ChecksumPath"
}
if (-not $ManifestPath) {
    $ManifestPath = Join-Path (Split-Path -Parent $Installer) "WINDOWS-INSTALLER-MANIFEST.json"
}
if (-not (Test-Path -LiteralPath $ManifestPath -PathType Leaf)) {
    throw "MANIFEST_FILE_NOT_FOUND: $ManifestPath"
}

$FileName = Split-Path -Leaf $Installer
$EscapedName = [regex]::Escape($FileName)
$ExpectedLine = Get-Content -LiteralPath $ChecksumPath -Encoding ascii |
    Where-Object { $_ -match "^([0-9a-fA-F]{64})  $EscapedName$" } |
    Select-Object -First 1
if (-not $ExpectedLine) { throw "CHECKSUM_ENTRY_NOT_FOUND: $FileName" }
$ExpectedHash = ([regex]::Match($ExpectedLine, '^[0-9a-fA-F]{64}')).Value.ToLowerInvariant()
$ActualHash = (Get-FileHash -LiteralPath $Installer -Algorithm SHA256).Hash.ToLowerInvariant()
if ($ActualHash -ne $ExpectedHash) { throw "SHA256_MISMATCH: expected=$ExpectedHash actual=$ActualHash" }

$Manifest = Get-Content -LiteralPath $ManifestPath -Raw -Encoding utf8 | ConvertFrom-Json
if ($Manifest.schemaVersion -ne "1") { throw "MANIFEST_SCHEMA_MISMATCH: $($Manifest.schemaVersion)" }
if ($Manifest.teamKitVersion -ne "v$VersionNumber") { throw "MANIFEST_VERSION_MISMATCH: $($Manifest.teamKitVersion)" }
if ($Manifest.sourceCommit -notmatch '^[0-9a-f]{40}$') { throw "MANIFEST_SOURCE_COMMIT_INVALID" }
if ($Manifest.installer.filename -ne $FileName) { throw "MANIFEST_FILENAME_MISMATCH" }
if ([int64]$Manifest.installer.sizeBytes -ne (Get-Item -LiteralPath $Installer).Length) { throw "MANIFEST_SIZE_MISMATCH" }
if ($Manifest.installer.sha256 -ne $ActualHash) { throw "MANIFEST_SHA256_MISMATCH" }
$InnoVersion = [string]$Manifest.innoVersion
if ([string]::IsNullOrWhiteSpace($InnoVersion) -or $InnoVersion -notmatch '^6\.[0-9]+(?:\.[0-9]+){0,2}\z') {
    throw "MANIFEST_INNO_VERSION_INVALID: $InnoVersion"
}
$GoVersion = [string]$Manifest.goVersion
if ([string]::IsNullOrWhiteSpace($GoVersion) -or $GoVersion -notmatch '^go version go[0-9]+\.[0-9]+(?:\.[0-9]+)? \S+/\S+\z') {
    throw "MANIFEST_GO_VERSION_INVALID: $GoVersion"
}

$VersionInfo = (Get-Item -LiteralPath $Installer).VersionInfo
$ActualProductVersion = $VersionInfo.ProductVersion.Trim()
if ($ActualProductVersion -ne $VersionNumber) {
    throw "PRODUCT_VERSION_MISMATCH: expected=$VersionNumber actual=$ActualProductVersion"
}
$ActualProductName = $VersionInfo.ProductName.Trim()
if ($ActualProductName -ne "1C Team Kit") {
    throw "PRODUCT_NAME_MISMATCH: $ActualProductName"
}

$Signature = Get-AuthenticodeSignature -LiteralPath $Installer
if (($RequireSignature -or $ExpectedSignerThumbprint) -and $Signature.Status -ne "Valid") {
    throw "SIGNATURE_REQUIRED: status=$($Signature.Status)"
}
if ($RequireSignature -and -not $ExpectedSignerThumbprint) { throw "SIGNER_THUMBPRINT_REQUIRED" }
if ($ExpectedSignerThumbprint) {
    $ExpectedSignerThumbprint = $ExpectedSignerThumbprint.Replace(" ", "").ToUpperInvariant()
    $ActualSignerThumbprint = $Signature.SignerCertificate.Thumbprint.Replace(" ", "").ToUpperInvariant()
    if ($ActualSignerThumbprint -ne $ExpectedSignerThumbprint) {
        throw "SIGNER_THUMBPRINT_MISMATCH: expected=$ExpectedSignerThumbprint actual=$ActualSignerThumbprint"
    }
    if ($Manifest.signer.thumbprint -ne $Signature.SignerCertificate.Thumbprint) { throw "MANIFEST_SIGNER_THUMBPRINT_MISMATCH" }
    if ($Manifest.signer.subject -ne $Signature.SignerCertificate.Subject) { throw "MANIFEST_SIGNER_SUBJECT_MISMATCH" }
    if ($Manifest.signer.status -ne "Valid") { throw "MANIFEST_SIGNER_STATUS_MISMATCH" }
}

Write-Output "INSTALLER_VERIFIED path=$Installer sha256=$ActualHash signature=$($Signature.Status)"
