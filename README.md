# Autostep (Windows Workflow Agent)

Autostep installs a single Windows service + CLI that runs declarative workflows, keeps state across reboots (including Safe Mode hops), and logs to the Windows Event Log. Workflows and artifacts are cached locally so runs can proceed without network access.

## What you get
- Per-machine install via MSI (service + CLI on PATH for all users).
- Durable runs: checkpoints before/after each step, automatic resume after reboot.
- Safe Mode support: workflows can request Safe Mode hops; the service is registered to start there.
- Local cache: workflows, artifacts, manifest, state, and JSON logs under ProgramData.
- Event Log source `Autostep` for key events.

## Install (Windows)
1) Download `autostep-<version>.msi`.
2) Run as Administrator. The MSI:
   - Installs `autostep.exe` to `C:\Program Files\Autostep\` and adds it to system PATH.
   - Seeds `C:\ProgramData\Autostep\` (workflows, artifacts, manifest, logs, state).
   - Registers/starts the `Autostep` Windows service (Automatic), including SafeBoot registry entries.
   - Registers Event Log source `Autostep`.

## Usage (CLI)
- `autostep list` — list workflows from `manifest.json`
- `autostep run <name>` — run a workflow by name (uses manifest)
- `autostep status` — show current state (runs, pending reboot)
- `autostep resume-pending` — manual resume if needed
- `autostep configure-safeboot-service` — ensure the service starts in Safe Mode (+ networking)
- `autostep version` — show version/commit/build date

Typical flow (bundled example):
1) Install the MSI.
2) Run `autostep run safemode_copy` from an elevated shell. The service will resume automatically after each reboot.
3) Inspect state/logs if desired: `autostep status`.

## Uninstall / Update
- Uninstall via Apps & Features / `msiexec /x autostep-<version>.msi`.
- Update by installing a newer MSI; the service and binary are replaced. ProgramData defaults may be refreshed—back up custom workflows/artifacts under `C:\ProgramData\Autostep\` if you’ve modified them.

## Where things live
- Binary: `C:\Program Files\Autostep\autostep.exe` (on PATH).
- Data root: `C:\ProgramData\Autostep\`
  - `workflows/`, `artifacts/`, `manifest.json`
  - `state.json` (durable run state)
  - `logs/` (JSON file logs) + Windows Event Log source `Autostep`

## How it resumes after reboot
- Steps are marked pending/complete in `state.json`.
- A reboot step records the next step and desired boot mode, then requests reboot. After boot, the service auto-starts (including in Safe Mode) and continues at the next step after any configured delay.

## Workflow authoring
- Workflows are YAML/JSON and listed in `manifest.json` under `C:\ProgramData\Autostep\`.
- For the DSL, actions, parameters, and examples, see `docs/workflows.md`.

## Build from source (summary)
- Cross-compile or build on Windows: `GOOS=windows GOARCH=amd64 go build -o autostep.exe ./cmd/autostep`.
- To produce MSI from WSL/Windows, use `build/package.sh` (WSL) or `build/wix/build.ps1` (Windows). See `build/README-msi-src.txt` for WiX/MSI build details.

