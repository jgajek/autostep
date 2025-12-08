Autostep MSI build instructions (Windows)

Prereqs:
- Windows with WiX Toolset 4 or later (v4–v6) installed (`wix.exe` on PATH). WiX 3.x (candle/light) is deprecated. Authoring uses the v4 schema and builds with `wix.exe` from WiX 6.
- PowerShell (pwsh recommended)

Install WiX Toolset:
- Download WiX Toolset 4.x or 6.x from the official GitHub releases: https://github.com/wixtoolset/wix/releases and install.
- Ensure `wix.exe` is on PATH (e.g., `C:\Program Files\WiX Toolset v6\bin` or `C:\Program Files (x86)\WiX Toolset v4\bin`).
- Verify: run `wix -?` in a new terminal.

Steps:
1) Open PowerShell in the unpacked package root (contains autostep.exe, manifest.json, workflows/, artifacts/, build/).
2) Run: `pwsh -File .\build\wix\build.ps1`
   - To override the version: `pwsh -File .\build\wix\build.ps1 -Version X.Y.Z`
   - If ExecutionPolicy blocks it: add `-ExecutionPolicy Bypass`.
3) Output MSI: `build/wix/dist/autostep-<version>.msi`

Install the Autostep MSI:
- From an elevated prompt: `msiexec /i build\wix\dist\autostep-<version>.msi /qn` for silent install, or double-click for GUI.
- Service `Autostep` installs (Automatic start) and SafeBoot registry keys allow it to start in Safe Mode/Networking.
- ProgramData cache is seeded under `C:\ProgramData\Autostep\` with workflows/artifacts/manifest.

Versioning:
- build/version.txt holds the current version stamped into the MSI and binary.
- The packager script (build/package.sh, run on Linux) auto-bumps the patch version and rebuilds autostep.exe with ldflags:
    -X main.version=<version>
    -X main.commit=<git short sha>
    -X main.buildDate=<iso8601>
- You normally do NOT need to pass -Version when building on Windows; build.ps1 reads build/version.txt by default.

