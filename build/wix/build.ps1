param(
    [string]$Version,
    [string]$OutDir = "$(Split-Path $PSScriptRoot)\dist"
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command wix.exe -ErrorAction SilentlyContinue)) {
    Write-Error "wix.exe not found. Install WiX Toolset 4.x and ensure it's on PATH."
}

if (-not $Version) {
    $versionPath = Resolve-Path "$PSScriptRoot\..\version.txt"
    if (Test-Path $versionPath) {
        $Version = (Get-Content $versionPath -Raw).Trim()
    } else {
        $Version = "0.0.0"
    }
}

$sourceDir = Resolve-Path "$PSScriptRoot\..\.."
$wxs = Resolve-Path "$PSScriptRoot\Product.wxs"

if (-not (Test-Path "$sourceDir\autostep.exe")) {
    Write-Error "Build autostep.exe (GOOS=windows) at $sourceDir\autostep.exe before running this script."
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$msi = Join-Path $OutDir "autostep-$Version.msi"

& wix.exe build $wxs `
    -o "$msi" `
    -d "SourceDir=$sourceDir" `
    -d "Version=$Version" `
    -arch x64

Write-Host "Built MSI: $msi"

