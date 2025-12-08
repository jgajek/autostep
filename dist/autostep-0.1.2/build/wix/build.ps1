param(
    [string]$Version,
    [string]$OutDir = "$(Split-Path $PSScriptRoot)\dist"
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command candle.exe -ErrorAction SilentlyContinue)) {
    Write-Error "candle.exe not found. Install WiX Toolset 3.14+ and ensure it's on PATH."
}
if (-not (Get-Command light.exe -ErrorAction SilentlyContinue)) {
    Write-Error "light.exe not found. Install WiX Toolset 3.14+ and ensure it's on PATH."
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

$obj = Join-Path $OutDir "Product.wixobj"
$msi = Join-Path $OutDir "autostep-$Version.msi"

& candle.exe "-dSourceDir=$sourceDir" "-dVersion=$Version" "-out" $obj $wxs
& light.exe -ext WixUtilExtension -cultures:en-us "-out" $msi $obj

Write-Host "Built MSI: $msi"

