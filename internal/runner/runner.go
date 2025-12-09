package runner

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/autostep/autostep/internal/actions"
	"github.com/autostep/autostep/internal/paths"
	"github.com/autostep/autostep/internal/state"
	"github.com/autostep/autostep/internal/workflow"
)

// Runner executes workflows step-by-step with durable checkpoints.
type Runner struct {
	paths  paths.Paths
	store  *state.Store
	logger Logger
}

// Logger is a minimal logging interface.
type Logger interface {
	Printf(format string, v ...any)
}

// New constructs a Runner.
func New(p paths.Paths, store *state.Store, logger Logger) *Runner {
	return &Runner{paths: p, store: store, logger: logger}
}

// RunWorkflow executes the workflow sequentially.
func (r *Runner) RunWorkflow(ctx context.Context, runID string, wf *workflow.Workflow) error {
	if err := r.store.StartRun(runID, wf.Name, len(wf.Steps)); err != nil {
		return fmt.Errorf("start run: %w", err)
	}

	return r.runFromIndex(ctx, runID, wf, 0)
}

// ContinueWorkflow resumes an existing run from the given step index.
func (r *Runner) ContinueWorkflow(ctx context.Context, runID string, wf *workflow.Workflow, startIndex int) error {
	if startIndex < 0 || startIndex >= len(wf.Steps) {
		return fmt.Errorf("start index out of range")
	}
	if err := r.store.ClearPendingReboot(runID); err != nil {
		return fmt.Errorf("clear pending reboot: %w", err)
	}
	return r.runFromIndex(ctx, runID, wf, startIndex)
}

func (r *Runner) runFromIndex(ctx context.Context, runID string, wf *workflow.Workflow, start int) error {
	for idx := start; idx < len(wf.Steps); idx++ {
		step := wf.Steps[idx]
		if err := r.store.MarkStepPending(runID, idx, step.ID); err != nil {
			return fmt.Errorf("mark step pending: %w", err)
		}
		if err := r.execStep(ctx, runID, idx, step); err != nil {
			if errors.Is(err, actions.ErrRebooting) {
				// Consider the step committed and stop further processing; run remains pending_reboot.
				if err2 := r.store.MarkStepComplete(runID, idx); err2 != nil {
					return fmt.Errorf("mark step complete after reboot: %w", err2)
				}
				return nil
			}
			_ = r.store.MarkStepFailed(runID, idx, err.Error())
			return err
		}
		if err := r.store.MarkStepComplete(runID, idx); err != nil {
			return fmt.Errorf("mark step complete: %w", err)
		}
	}
	return r.store.MarkRunCompleted(runID)
}

func (r *Runner) execStep(ctx context.Context, runID string, idx int, step workflow.Step) error {
	r.logger.Printf("run %s step %s action=%s", runID, step.ID, step.Action)
	switch strings.ToLower(step.Action) {
	case "file_copy":
		return r.handleFileCopy(step)
	case "file_rename":
		return r.handleFileRename(step)
	case "file_delete":
		return r.handleFileDelete(step)
	case "file_exists":
		return r.handleFileExists(step)
	case "registry_set":
		return r.handleRegistrySet(step)
	case "registry_delete":
		return r.handleRegistryDelete(step)
	case "registry_save":
		return r.handleRegistrySave(step)
	case "registry_restore":
		return r.handleRegistryRestore(step)
	case "registry_load":
		return r.handleRegistryLoad(step)
	case "registry_unload":
		return r.handleRegistryUnload(step)
	case "registry_append":
		return r.handleRegistryAppend(step)
	case "registry_equals":
		return r.handleRegistryEquals(step)
	case "service_start":
		return r.handleServiceStart(step)
	case "service_stop":
		return r.handleServiceStop(step)
	case "service_running":
		return r.handleServiceRunning(step)
	case "driver_load":
		return r.handleDriverLoad(step)
	case "driver_unload":
		return r.handleDriverUnload(step)
	case "driver_loaded":
		return r.handleDriverLoaded(step)
	case "reboot":
		return r.handleReboot(runID, idx, step)
	case "verify":
		return r.handleVerify(step)
	case "run":
		return r.handleRun(ctx, step)
	case "sleep":
		return r.handleSleep(step)
	case "safeboot":
		return r.handleSafeBoot(step)
	default:
		return fmt.Errorf("unknown action %q", step.Action)
	}
}

func (r *Runner) handleFileCopy(step workflow.Step) error {
	if step.SrcPath == "" || step.DstPath == "" {
		return errors.New("file_copy requires src_path and dst_path")
	}
	src := step.SrcPath
	if strings.HasPrefix(src, "cache://") {
		src = filepath.Join(r.paths.ArtifactsDir, strings.TrimPrefix(src, "cache://"))
	}
	if err := os.MkdirAll(filepath.Dir(step.DstPath), 0o755); err != nil {
		return fmt.Errorf("make dest dir: %w", err)
	}
	if err := copyFile(src, step.DstPath); err != nil {
		return err
	}
	if step.VerifySHA256 != "" {
		hash, err := fileSHA256(step.DstPath)
		if err != nil {
			return fmt.Errorf("hash dest: %w", err)
		}
		if !strings.EqualFold(hash, step.VerifySHA256) {
			return fmt.Errorf("checksum mismatch: expected %s got %s", step.VerifySHA256, hash)
		}
	}
	return nil
}

func (r *Runner) handleFileRename(step workflow.Step) error {
	if step.SrcPath == "" || step.NewName == "" {
		return errors.New("file_rename requires src_path and new_name")
	}
	dir := filepath.Dir(step.SrcPath)
	dest := filepath.Join(dir, step.NewName)
	if err := os.Rename(step.SrcPath, dest); err != nil {
		return fmt.Errorf("rename %s to %s: %w", step.SrcPath, dest, err)
	}
	return nil
}

func (r *Runner) handleFileDelete(step workflow.Step) error {
	matches, err := r.matchPaths(step.PathRegex)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return nil
	}
	for _, p := range matches {
		info, statErr := os.Stat(p)
		if statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("stat %s: %w", p, statErr)
		}
		if info.IsDir() {
			// Skip directories; file_delete only targets files.
			continue
		}
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("delete %s: %w", p, err)
		}
	}
	return nil
}

func (r *Runner) handleFileExists(step workflow.Step) error {
	matches, err := r.matchPaths(step.PathRegex)
	if err != nil {
		return err
	}
	expect, err := expectedBool(step.Expected)
	if err != nil {
		return err
	}
	if expect {
		if len(matches) == 0 {
			return fmt.Errorf("file_exists: no matches for %s", step.PathRegex)
		}
	} else {
		if len(matches) > 0 {
			return fmt.Errorf("file_exists: unexpected match(es) for %s", step.PathRegex)
		}
	}
	return nil
}

func (r *Runner) handleRegistrySet(step workflow.Step) error {
	if step.Path == "" || step.Type == "" {
		return errors.New("registry_set requires path and type")
	}
	return actions.RegistrySet(step.Path, step.Type, step.Value)
}

func (r *Runner) handleRegistryDelete(step workflow.Step) error {
	if step.Path == "" {
		return errors.New("registry_delete requires path")
	}
	return actions.RegistryDeleteValue(step.Path)
}

func (r *Runner) handleRegistrySave(step workflow.Step) error {
	if step.Path == "" || step.HiveFile == "" {
		return errors.New("registry_save requires path and hive_file")
	}
	return actions.RegistrySave(step.Path, step.HiveFile)
}

func (r *Runner) handleRegistryRestore(step workflow.Step) error {
	if step.Path == "" || step.HiveFile == "" {
		return errors.New("registry_restore requires path and hive_file")
	}
	return actions.RegistryRestore(step.Path, step.HiveFile)
}

func (r *Runner) handleRegistryLoad(step workflow.Step) error {
	if step.Path == "" || step.HiveFile == "" {
		return errors.New("registry_load requires path and hive_file")
	}
	return actions.RegistryLoad(step.Path, step.HiveFile)
}

func (r *Runner) handleRegistryUnload(step workflow.Step) error {
	if step.Path == "" {
		return errors.New("registry_unload requires path")
	}
	return actions.RegistryUnload(step.Path)
}

func (r *Runner) handleRegistryAppend(step workflow.Step) error {
	if step.Path == "" {
		return errors.New("registry_append requires path")
	}
	suffix := fmt.Sprint(step.Value)
	return actions.RegistryAppend(step.Path, suffix)
}

func (r *Runner) handleRegistryEquals(step workflow.Step) error {
	if step.Path == "" {
		return errors.New("registry_equals requires path")
	}
	expected := fmt.Sprint(step.Expected)
	got, err := actions.RegistryGetString(step.Path)
	if err != nil {
		return fmt.Errorf("registry read: %w", err)
	}
	if expected != got {
		return fmt.Errorf("registry_equals mismatch: expected %s got %s", expected, got)
	}
	return nil
}

func (r *Runner) handleServiceStart(step workflow.Step) error {
	if step.Service == "" {
		return errors.New("service_start requires service")
	}
	return actions.ServiceStart(step.Service)
}

func (r *Runner) handleServiceStop(step workflow.Step) error {
	if step.Service == "" {
		return errors.New("service_stop requires service")
	}
	return actions.ServiceStop(step.Service)
}

func (r *Runner) handleServiceRunning(step workflow.Step) error {
	if step.Service == "" {
		return errors.New("service_running requires service")
	}
	expect, err := expectedBool(step.Expected)
	if err != nil {
		return err
	}
	running, err := actions.ServiceRunning(step.Service)
	if err != nil {
		return err
	}
	if expect && !running {
		return fmt.Errorf("service %s is not running", step.Service)
	}
	if !expect && running {
		return fmt.Errorf("service %s is running but expected false", step.Service)
	}
	return nil
}

func (r *Runner) handleDriverLoad(step workflow.Step) error {
	if step.DriverName == "" || step.DriverPath == "" {
		return errors.New("driver_load requires driver_name and driver_path")
	}
	return actions.DriverLoad(step.DriverName, step.DriverPath)
}

func (r *Runner) handleDriverUnload(step workflow.Step) error {
	if step.DriverName == "" {
		return errors.New("driver_unload requires driver_name")
	}
	return actions.DriverUnload(step.DriverName)
}

func (r *Runner) handleDriverLoaded(step workflow.Step) error {
	if step.DriverName == "" {
		return errors.New("driver_loaded requires driver_name")
	}
	expect, err := expectedBool(step.Expected)
	if err != nil {
		return err
	}
	running, err := actions.DriverLoaded(step.DriverName)
	if err != nil {
		return err
	}
	if expect && !running {
		return fmt.Errorf("driver %s is not loaded", step.DriverName)
	}
	if !expect && running {
		return fmt.Errorf("driver %s is loaded but expected false", step.DriverName)
	}
	return nil
}

func expectedBool(v any) (bool, error) {
	if v == nil {
		return true, nil
	}
	switch t := v.(type) {
	case bool:
		return t, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		default:
			return false, fmt.Errorf("expected must be boolean-like, got %q", t)
		}
	case int:
		return t != 0, nil
	case int64:
		return t != 0, nil
	case float64:
		return t != 0, nil
	default:
		return false, fmt.Errorf("expected must be bool/number/string, got %T", v)
	}
}

func (r *Runner) handleReboot(runID string, idx int, step workflow.Step) error {
	next := idx + 1
	bootMode := "normal"
	if step.SafeMode {
		bootMode = "safe"
	}
	if err := r.store.MarkPendingReboot(runID, next, bootMode, step.ResumeDelaySeconds); err != nil {
		return err
	}
	if err := actions.RequestReboot(step.SafeMode); err != nil {
		return err
	}
	return actions.ErrRebooting
}

func (r *Runner) handleVerify(step workflow.Step) error {
	for _, assertion := range step.Assertions {
		switch strings.ToLower(assertion.Kind) {
		case "file_exists":
			if assertion.Path == "" {
				return errors.New("file_exists requires path")
			}
			expect, err := expectedBool(assertion.Expected)
			if err != nil {
				return err
			}
			_, statErr := os.Stat(assertion.Path)
			if expect {
				if statErr != nil {
					return fmt.Errorf("verify file_exists failed: %w", statErr)
				}
			} else {
				if statErr == nil {
					return fmt.Errorf("verify file_exists expected absence but found: %s", assertion.Path)
				}
				if !errors.Is(statErr, os.ErrNotExist) {
					return fmt.Errorf("verify file_exists unexpected error: %w", statErr)
				}
			}
		case "registry_equals":
			if assertion.Path == "" {
				return errors.New("registry_equals requires path")
			}
			got, err := actions.RegistryGetString(assertion.Path)
			if err != nil {
				return fmt.Errorf("registry read: %w", err)
			}
			if fmt.Sprint(assertion.Expected) != got {
				return fmt.Errorf("registry_equals mismatch: expected %v got %s", assertion.Expected, got)
			}
		default:
			return fmt.Errorf("unknown assertion kind %q", assertion.Kind)
		}
	}
	return nil
}

func (r *Runner) handleRun(ctx context.Context, step workflow.Step) error {
	if step.Command == "" {
		return errors.New("run requires command")
	}
	cmd := exec.CommandContext(ctx, step.Command, step.Args...)
	if step.WorkingDir != "" {
		cmd.Dir = step.WorkingDir
	}
	if len(step.Env) > 0 {
		cmd.Env = os.Environ()
		for _, kv := range step.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", kv.Key, kv.Value))
		}
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *Runner) handleSleep(step workflow.Step) error {
	if step.SleepSeconds < 0 {
		return errors.New("sleep_seconds must be >= 0")
	}
	time.Sleep(time.Duration(step.SleepSeconds) * time.Second)
	return nil
}

func (r *Runner) handleSafeBoot(step workflow.Step) error {
	mode := strings.ToLower(step.SafeBootMode)
	if mode == "" {
		return errors.New("safeboot requires safe_boot_mode: minimal|network|off")
	}
	return actions.BcdeditSafeBoot(mode)
}

func (r *Runner) matchPaths(pattern string) ([]string, error) {
	if pattern == "" {
		return nil, errors.New("path_regex is required")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile regex: %w", err)
	}
	root := searchRoot(pattern)
	if root == "" {
		root = "."
	}
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("stat root %s: %w", root, err)
	}

	var matches []string
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if re.MatchString(path) {
			matches = append(matches, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return matches, nil
}

func searchRoot(pattern string) string {
	cleaned := filepath.Clean(pattern)
	meta := "*+?[](){}|^$"
	firstMeta := -1
	for i, r := range cleaned {
		if r == filepath.Separator {
			continue
		}
		if strings.ContainsRune(meta, r) {
			firstMeta = i
			break
		}
	}
	if firstMeta == -1 {
		return filepath.Dir(cleaned)
	}
	prefix := cleaned[:firstMeta]
	if prefix == "" {
		return string(filepath.Separator)
	}
	return filepath.Dir(prefix)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return out.Close()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
