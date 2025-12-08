# Autostep (Windows Workflow Agent)

Goal: deliver a single compiled Windows binary (shipped via MSI) that runs automated multi‑step workflows, preserves state across reboots, caches workflow definitions and artifacts locally, and writes important events to the Windows Event Log.

## Requirements (from kickoff + updates)
- Works on recent Windows (10/11, Server 2019+).
- Easy install: MSI that installs a service and bundles defaults.
- Local cache of workflows, binary artifacts, and success criteria to allow offline/low-connectivity runs.
- Workflow steps include copying artifacts, editing registry keys, rebooting mid-run, and verifying state.
- Resumes after reboot at the next step.
- Logs key events to the Windows Event Log.
- Reboot steps must support Safe Mode hops: allow a sequence “boot to Safe Mode → perform next step → boot to normal mode”.
- Core steps are extensible; new actions (with parameters) will be added over time without breaking existing workflows.
- Cache location contains per-workflow subfolders for definitions and artifacts; workflows are invocable by name using a manifest/naming convention.

## Proposed stack
- Language: Go 1.22+, targeting `windows/amd64`; statically linked where possible.
- Service framework: `github.com/kardianos/service` (simplifies install/start/stop and works with a single binary).
- Logging: `golang.org/x/sys/windows/svc/eventlog` for Event Log + rotating structured file logs (JSON) via `zap` or `zerolog`.
- Persistence: SQLite (via `modernc.org/sqlite`) or Badger/bolt-style embedded DB for state and history; no external deps.
- Packaging: WiX Toolset 3.14+ to produce MSI (installs service, creates Event Log source, lays down ProgramData dirs).
- Hashing: SHA-256 for artifact caching and verification.

## Runtime layout (defaults)
- Binary: `C:\Program Files\Autostep\autostep.exe`.
- Data root: `C:\ProgramData\Autostep\`
  - `workflows/` – cached workflow definitions (YAML/JSON).
  - `artifacts/` – cached binaries/assets, keyed by hash/version.
  - `manifest.json` – maps workflow name → definition path, version, artifact set.
  - `state.db` – run history, step pointer, success criteria status.
  - `logs/` – structured logs; Event Log mirrors important events.

## Components
- **Agent service**: Windows service set to Automatic start. On boot, checks `state.db` for in-flight runs and resumes at the pending step. Provides an internal scheduler to pick up queued workflows.
- **CLI (same binary)**: `autostep.exe run <workflow>`, `list`, `status`, `cache refresh`. Talks to the service over a local named pipe/HTTP over named pipe.
- **Workflow engine**: Executes a declarative workflow DSL. Steps are designed to be idempotent and record a durable checkpoint before and after execution.
- **State store**: Persists runs, step pointer, per-step digests, verification results, and timestamps. A crash-safe “pend → apply → commit” pattern prevents double-application.
- **Artifact manager**: Downloads or ingests artifacts, verifies checksum/signature, caches under hash, and exposes them to steps.
- **Eventing**: On install, registers an Event Log source `Autostep`. Emits start/finish/failure/reboot events with correlation IDs.

## Workflow DSL (early sketch)
Stored as YAML/JSON; versioned per workflow.
```yaml
version: 1
name: install_driver
steps:
  - id: copy-driver
    action: copy
    from: cache://driver_v1.zip
    to: C:\Drivers\
    verify_sha256: "<hash>"
  - id: set-reg
    action: registry_set
    path: HKLM\SOFTWARE\Contoso\Driver\Enabled
    type: dword
    value: 1
  - id: reboot
    action: reboot
    reason: "Driver activation"
    resume_delay_seconds: 15
  - id: verify
    action: verify
    assertions:
      - kind: file_exists
        path: C:\Drivers\driver.sys
      - kind: registry_equals
        path: HKLM\SOFTWARE\Contoso\Driver\Enabled
        expected: 1
```
Initial built-in actions: `copy`, `registry_set`, `registry_delete`, `reboot`, `verify` (file/registry/service/process checks), `run` (invoke process), `sleep`.
Added: `safeboot` (mode: minimal|network|off) to toggle BCD Safe Mode. Workflows remain responsible for sequencing reboots and ensuring the service is available.
Extensibility: versioned action schema; new actions added with feature flags/defaults to stay backward compatible.

## Reboot + resume strategy (including Safe Mode)
- Before executing a step, record `pending` with step ID in `state.db`.
- For `reboot`, write a `pending_reboot` marker containing the next step ID and desired boot mode (normal/safe); flush to disk; request reboot; service restarts automatically and resumes at the recorded next step after optional delay.
- Safe Mode hop: the workflow is responsible for how the machine enters/exits Safe Mode (including any unconventional methods). Autostep only records intent and resumes once the service is available again. The workflow must ensure the service will start in Safe Mode (e.g., via a prior step that adjusts service start settings) before switching modes.
- All steps should be idempotent; repeated runs consult per-step digests to avoid reapplying already-committed changes when safe.

## Logging & observability
- Event Log channel `Application` under source `Autostep`; event IDs mapped per action (start, success, failure, resume, reboot-requested, verification-failed).
- File logs: JSON, rotated daily and by size in `ProgramData\Autostep\logs`.
- Structured metadata: workflow name, run ID, step ID, duration, exit codes, hashes.

## Packaging (MSI)
- WiX installer:
  - Installs `autostep.exe` to `Program Files\Autostep`.
  - Creates `ProgramData\Autostep` subdirectories with ACLs.
  - Registers and starts the Windows service (Automatic).
  - Registers Event Log source.
  - Seeds `workflows/` + `artifacts/` + `manifest.json` with a default workflow set (including `safemode_copy`).
  - Configures service to start in Safe Mode and Safe Mode with Networking via SafeBoot registry entries.
  - Optional: installs a Start Menu shortcut for the CLI help.
- Recommend code signing the MSI and binary.

## CLI (current)
- `autostep run <name>` — run workflow once (uses `manifest.json`)
- `autostep resume-pending` — resume runs that were waiting on reboot
- `autostep status` — dump state.json
- `autostep list` — list workflows in manifest
- `autostep configure-safeboot-service` — register the Autostep service to run in Safe Mode and Safe Mode with Networking (Windows)
- `autostep version` — show version, commit, build date

## Hands-free MVP flow (safemode_copy)
1) Install `autostep-<version>.msi` (per-machine; installs service, seeds ProgramData, SafeBoot registry, Event Log source).  
2) Open an elevated CMD/PowerShell and run: `autostep run safemode_copy`.  
3) The workflow will: enable safeboot (minimal) → reboot → service auto-starts in Safe Mode and resumes → copy `hello.txt` to `%WINDIR%\Temp` → disable safeboot → reboot → service auto-starts in normal mode and resumes → verify file exists. No manual `resume-pending` is required; the service resumes automatically on each boot.  
4) Check status if desired: `autostep status`.

## Build MSI (WiX 4+/6, Windows)
- Prereqs: Go 1.22+, WiX Toolset 4 or later (v4–v6) with `wix.exe` on PATH. Authoring uses the v4 schema and builds successfully with WiX 6 `wix.exe`.
- Build the binary with version info:  
  `set GOOS=windows`  
  `set GOARCH=amd64`  
  `go build -ldflags "-X main.version=0.1.0 -X main.commit=REPLACE_WITH_GIT_SHA -X main.buildDate=%DATE%" -o autostep.exe ./cmd/autostep`
- Build MSI: from the package root, run `pwsh -File .\build\wix\build.ps1 -Version 0.1.0` (or inside pwsh: `.\build\wix\build.ps1`). If ExecutionPolicy blocks it, add `-ExecutionPolicy Bypass`. Paths with spaces are supported by the script.
- Output: `build/wix/dist/autostep-0.1.0.msi`. Installs service + seeds ProgramData cache + SafeBoot registry entries + Event Log source.
- For WiX install steps and MSI installation notes, see `build/README-msi-src.txt` (included in the MSI source bundle).

## Security notes
- Service runs as LocalSystem by default (required for registry/system changes); consider configurable service account if reduced privileges are acceptable.
- Validate all downloaded workflow definitions and artifacts with signature/hash.
- Protect `ProgramData\Autostep` with restrictive ACLs.

## Compatibility & build
- Target: Windows 10/11, Server 2019+ (amd64).
- Build (Windows host preferred for MSI): `go build -ldflags "-s -w" -o autostep.exe ./cmd/autostep`.
- Cross-compile from Linux for dev: `GOOS=windows GOARCH=amd64 go build ...` (no CGO).
- MSI build: WiX `candle`/`light` scripts under `build/wix/` (to be added).

## Next steps
1) Finalize DSL schema (step set and assertion types).  
2) Define workflow discovery manifest/naming convention (`manifest.json`) and cache layout invariants.  
3) Scaffold Go module (`go mod init github.com/<org>/autostep`) with `cmd/autostep/main.go`.  
4) Implement service harness (kardianos/service) + Event Log writer + file logger.  
5) Add state store and checkpoint/resume logic (including Safe Mode flags).  
6) Implement core steps: copy (with checksum), registry_set/delete, reboot (normal/safe), verify.  
7) Add artifact cache and fetch pipeline.  
8) Author WiX installer scripts and service configuration (with optional Safe Mode service start prep).  
9) Smoke tests on Windows 10/11 + Server 2019/2022 VMs (reboot/resume, Event Log entries, Safe Mode hop initiated by workflow steps).

