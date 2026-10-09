# Installs Aurelia, or updates it, on Windows:
#
#   irm https://raw.githubusercontent.com/j4ckxyz/Aurelia/main/install.ps1 | iex
#
# It downloads the installer of the latest release and runs it without
# questions: Aurelia goes to %LOCALAPPDATA%\Programs\Aurelia, for you
# alone, without administrator rights, with its entry in the Start menu
# and in "Installed apps", where it is removed. A file fetched this way
# is not marked as downloaded from the internet, so SmartScreen does not
# stop it. Aurelia then updates itself (Settings > Updates).
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue' # downloads are many times faster without the bar

$repo = 'j4ckxyz/Aurelia'
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'amd64' }

# The list of the latest version tells which one it is.
try {
    $manifest = Invoke-RestMethod "https://github.com/$repo/releases/latest/download/update-windows-$arch.json"
} catch {
    throw "Could not read the latest release of ${repo}: $($_.Exception.Message)"
}
$version = $manifest.version
$name = "Aurelia-Setup-$version-windows-$arch.exe"
$url = "https://github.com/$repo/releases/download/v$version/$name"
$installer = Join-Path ([IO.Path]::GetTempPath()) $name

Write-Host "Downloading Aurelia $version..."
Invoke-WebRequest $url -OutFile $installer -UseBasicParsing

# A running Aurelia would keep its files from being replaced.
Get-Process Aurelia -ErrorAction SilentlyContinue | Stop-Process -Force
Write-Host 'Installing...'
$setup = Start-Process -FilePath $installer -ArgumentList '/S' -Wait -PassThru
Remove-Item $installer -ErrorAction SilentlyContinue
if ($setup.ExitCode -ne 0) {
    throw "The installer ended with code $($setup.ExitCode)."
}

$app = Join-Path $env:LOCALAPPDATA 'Programs\Aurelia\Aurelia.exe'
if (Test-Path $app) {
    Write-Host "Aurelia $version is installed. It is in the Start menu."
    if (-not $env:AURELIA_NO_OPEN) { Start-Process $app }
} else {
    Write-Host "Aurelia $version is installed."
}
