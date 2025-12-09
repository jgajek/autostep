# Workflows: Authoring and Reference

This document explains how Autostep workflows are structured, how they run, and provides an action-by-action reference with examples.

## How workflows are discovered and run
- Workflows live in `C:\ProgramData\Autostep\workflows\` and are listed in `C:\ProgramData\Autostep\manifest.json`.
- The manifest maps a workflow name (e.g., `safemode_copy`) to a file path (YAML/JSON) relative to the Autostep root.
- Invoke by name: `autostep run <workflow_name>`. The CLI reads `manifest.json`, loads the workflow, and executes it.
- State is kept in `C:\ProgramData\Autostep\state.json`: run id, current step index, pending reboot flags, and per-step results.
- The Windows service resumes runs after reboots. If a step requested reboot, the service auto-starts, clears the pending flag, and continues at the next step.
- Artifacts referenced with `cache://...` are resolved under `C:\ProgramData\Autostep\artifacts\`.

## Workflow file template (YAML)
```yaml
version: 1
name: my_workflow
steps:
  - id: copy-driver
    action: file_copy
    src_path: cache://driver_v1.zip
    dst_path: C:\Drivers\driver_v1.zip
    verify_sha256: "<optional sha256>"

  - id: set-reg
    action: registry_set
    path: HKLM\SOFTWARE\Contoso\Driver\Enabled
    type: dword
    value: 1

  - id: reboot-to-apply
    action: reboot
    resume_delay_seconds: 15

  - id: verify-install
    action: verify
    assertions:
      - kind: file_exists
        path: C:\Drivers\driver.sys
      - kind: registry_equals
        path: HKLM\SOFTWARE\Contoso\Driver\Enabled
        expected: 1
```

## Field reference (common)
- `id` (string, required): Unique per step.
- `action` (string, required): One of the actions listed below.
- `expected` (bool/string/number, optional, default `true`): For check-type actions (e.g., `file_exists`, `service_running`, `driver_loaded`, assertions). `expected: false` inverts the check.
- `notes` (string, optional): Free-form description.

## Action reference

### File actions
- `file_copy`: Copy a file (supports `cache://` source).
  - `src_path` (required)
  - `dst_path` (required)
  - `verify_sha256` (optional)
- `file_rename`: Rename within the same directory.
  - `src_path` (required)
  - `new_name` (required)
- `file_delete`: Delete files matching regex.
  - `path_regex` (required) — regex tested against absolute paths; search root inferred from non-regex prefix; directories are skipped.
- `file_exists`: Check presence/absence.
  - `path_regex` (required)
  - `expected` (optional, default `true`) — `false` asserts absence.

### Registry actions
- `registry_set`: Write a value.
  - `path` (required) — e.g., `HKLM\SOFTWARE\Foo\Bar`
  - `type` (required) — `string|sz|dword`
  - `value` (required)
- `registry_delete`: Delete a value.
  - `path` (required)
- `registry_save`: Save a key to hive file.
  - `path` (required)
  - `hive_file` (required)
- `registry_restore`: Restore a key from hive file.
  - `path` (required)
  - `hive_file` (required)
- `registry_load`: Load a hive into a key (e.g., under HKLM/HKU).
  - `path` (required)
  - `hive_file` (required)
- `registry_unload`: Unload a hive.
  - `path` (required)
- `registry_append`: Append string data to an existing string value.
  - `path` (required)
  - `value` (required) — suffix to append (any type is stringified)
- `registry_equals`: Check string value equals expected.
  - `path` (required)
  - `expected` (required) — compared as string; set to `false` only when paired with `expected` semantics in verify assertions (see below).

### Service actions
- `service_start`
  - `service` (required)
- `service_stop`
  - `service` (required)
- `service_running`: Check running/not running.
  - `service` (required)
  - `expected` (optional, default `true`) — `false` asserts stopped.

### Driver (kernel) actions
- `driver_load`
  - `driver_name` (required)
  - `driver_path` (required) — path to `.sys` (installed as a kernel driver service if missing)
- `driver_unload`
  - `driver_name` (required)
- `driver_loaded`: Check loaded/not loaded.
  - `driver_name` (required)
  - `expected` (optional, default `true`) — `false` asserts not loaded.

### Execution and timing
- `run`: Execute a process.
  - `command` (required)
  - `args` (optional array)
  - `env` (optional array of `{key,value}`)
  - `working_dir` (optional)
- `sleep`: Pause execution.
  - `sleep_seconds` (required, >= 0)

### Boot control
- `reboot`: Request reboot and mark resume point.
  - `safe_mode` (optional bool) — indicates next boot should be Safe Mode (workflow is responsible for ensuring Safe Mode entry)
  - `resume_delay_seconds` (optional int) — delay before resume
- `safeboot`: Toggle BCD safeboot flag.
  - `safe_boot_mode` (required) — `minimal|network|off`

### Verify (assertions block)
- `verify` executes an array of assertions; all must pass.
- Assertions:
  - `file_exists`
    - `path` (required)
    - `expected` (optional, default `true`; `false` asserts absence)
  - `registry_equals`
    - `path` (required)
    - `expected` (required) — compared as string

## Notes on `expected`
- Applies to check-style actions: `file_exists`, `service_running`, `driver_loaded`, and verify assertions (e.g., `file_exists` in `assertions`).
- Defaults to `true` when omitted.
- Accepts bools, boolean-like strings (`"true"`, `"false"`, `"1"`, `"0"`, `"yes"`, `"no"`, `"on"`, `"off"`), or numbers (0/1).

## Safe Mode and reboot behavior
- Before executing a step, Autostep marks it `pending` in `state.json`.
- For `reboot`, it records the next step index and desired boot mode, flushes to disk, and returns `ErrRebooting`; the CLI exits 0. The service resumes after reboot.
- Workflows are responsible for entering/exiting Safe Mode (`safeboot` and subsequent reboot steps) and ensuring the Autostep service can start in that mode.

## Manifest example
```json
{
  "version": 1,
  "workflows": {
    "sample_copy": {
      "path": "workflows/sample_copy.yaml"
    },
    "safemode_copy": {
      "path": "workflows/safemode_copy.yaml"
    }
  }
}
```

## Sample: service/driver checks
```yaml
steps:
  - id: start-svc
    action: service_start
    service: W32Time

  - id: svc-running
    action: service_running
    service: W32Time

  - id: drv-check
    action: driver_loaded
    driver_name: MyDriver
    expected: false  # assert not loaded
```

