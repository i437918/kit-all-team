[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$InstallerPath,

    [string]$EvidencePath = 'C:\Output\teamkit-prototype-sandbox.json'
)

$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $InstallerPath -PathType Leaf)) {
    throw "SANDBOX_INSTALLER_NOT_FOUND: $InstallerPath"
}

$install = Start-Process -FilePath $InstallerPath -ArgumentList @(
    '/VERYSILENT',
    '/SUPPRESSMSGBOXES',
    '/NORESTART'
) -Wait -PassThru

if ($install.ExitCode -ne 0) {
    throw "SANDBOX_INSTALLER_FAILED: exit code $($install.ExitCode)"
}

$programHome = Join-Path $env:LOCALAPPDATA 'Programs\1C Team Kit'
$wizard = Join-Path $programHome 'TeamKitGUI.ps1'
if (-not (Test-Path -LiteralPath $wizard -PathType Leaf)) {
    throw "SANDBOX_WIZARD_NOT_INSTALLED: $wizard"
}

$evidenceDirectory = Split-Path -Parent $EvidencePath
$wizardStandardOutput = Join-Path $evidenceDirectory 'teamkit-prototype-wizard.stdout.log'
$wizardStandardError = Join-Path $evidenceDirectory 'teamkit-prototype-wizard.stderr.log'
$wizardArguments = "-NoProfile -ExecutionPolicy RemoteSigned -File `"$wizard`" -ProgramHome `"$programHome`" -BackendMode Prototype"
$wizardProcess = Start-Process -FilePath "$env:WINDIR\System32\WindowsPowerShell\v1.0\powershell.exe" -ArgumentList $wizardArguments -PassThru -RedirectStandardOutput $wizardStandardOutput -RedirectStandardError $wizardStandardError

Start-Sleep -Seconds 10
$wizardProcess.Refresh()

$screenshotPath = Join-Path $evidenceDirectory 'teamkit-prototype-sandbox.png'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Windows.Forms
$bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
$bitmap = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
try {
    $graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size)
    $bitmap.Save($screenshotPath, [System.Drawing.Imaging.ImageFormat]::Png)
} finally {
    $graphics.Dispose()
    $bitmap.Dispose()
}

[ordered]@{
    backend = 'Prototype'
    installerExitCode = $install.ExitCode
    programHome = $programHome
    wizard = $wizard
    wizardProcessId = $wizardProcess.Id
    wizardRunningAfterStart = -not $wizardProcess.HasExited
    wizardExitCode = if ($wizardProcess.HasExited) { $wizardProcess.ExitCode } else { $null }
    wizardStandardOutput = $wizardStandardOutput
    wizardStandardError = $wizardStandardError
    screenshot = $screenshotPath
    screenshotExists = Test-Path -LiteralPath $screenshotPath -PathType Leaf
    startedAtUtc = [DateTime]::UtcNow.ToString('o')
} | ConvertTo-Json | Set-Content -LiteralPath $EvidencePath -Encoding UTF8
