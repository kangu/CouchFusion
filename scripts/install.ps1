$ErrorActionPreference = "Stop"

$repo = "kangu/CouchFusion"
$installDir = Join-Path $env:USERPROFILE ".couchfusion\bin"
$binaryName = "couchfusion.exe"

function Write-Log($message) {
    Write-Host "[couchfusion installer] $message"
}

function Get-Architecture {
    if ([Environment]::Is64BitOperatingSystem) {
        return "amd64"
    }
    throw "Unsupported architecture. couchfusion releases are provided for 64-bit Windows only."
}

function Get-LatestVersion {
    $release = Invoke-WebRequest -Uri "https://api.github.com/repos/$repo/releases/latest" -UseBasicParsing
    $json = $release.Content | ConvertFrom-Json
    return $json.tag_name
}

$version = $env:COUCHFUSION_VERSION
if (-not $version) {
    Write-Log "Discovering latest release..."
    $version = Get-LatestVersion
}

if (-not $version) {
    throw "Unable to determine release version. Set COUCHFUSION_VERSION and retry."
}

$arch = Get-Architecture
$assetName = "couchfusion_windows_$arch.zip"
$downloadUrl = "https://github.com/$repo/releases/download/$version/$assetName"

Write-Log "Installing couchfusion $version for windows/$arch"

New-Item -ItemType Directory -Force -Path $installDir | Out-Null
$tempZip = Join-Path ([IO.Path]::GetTempPath()) "couchfusion.zip"

Invoke-WebRequest -Uri $downloadUrl -OutFile $tempZip -UseBasicParsing
Expand-Archive -Path $tempZip -DestinationPath $installDir -Force
Remove-Item $tempZip

$binaryPath = Join-Path $installDir $binaryName
Write-Log "Binary installed to $binaryPath"

$currentPath = [Environment]::GetEnvironmentVariable("PATH", [EnvironmentVariableTarget]::User)
if (-not $currentPath.Split(";") -contains $installDir) {
    $newPath = if ($currentPath) { "$currentPath;$installDir" } else { $installDir }
    [Environment]::SetEnvironmentVariable("PATH", $newPath, [EnvironmentVariableTarget]::User)
    Write-Log "Added $installDir to user PATH. Restart your terminal to use couchfusion."
}
else {
    Write-Log "$installDir already present in PATH."
}

Write-Log "Done!"
