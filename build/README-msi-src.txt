Autostep MSI build instructions (Windows)

Prereqs:
- Windows with WiX Toolset 3.14+ installed (candle.exe, light.exe on PATH)
- PowerShell (pwsh recommended)

Steps:
1) Open PowerShell in the unpacked package root (contains autostep.exe, manifest.json, workflows/, artifacts/, build/).
2) Run: pwsh build/wix/build.ps1
   - If you want to override the version: pwsh build/wix/build.ps1 -Version X.Y.Z
3) Output MSI: build/wix/dist/autostep-<version>.msi

Versioning:
- build/version.txt holds the current version stamped into the MSI and binary.
- The packager script (build/package.sh, run on Linux) auto-bumps the patch version and rebuilds autostep.exe with ldflags:
    -X main.version=<version>
    -X main.commit=<git short sha>
    -X main.buildDate=<iso8601>
- You normally do NOT need to pass -Version when building on Windows; build.ps1 reads build/version.txt by default.

