package runner

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	case "copy":
		return r.handleCopy(step)
	case "registry_set":
		return r.handleRegistrySet(step)
	case "registry_delete":
		return r.handleRegistryDelete(step)
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

func (r *Runner) handleCopy(step workflow.Step) error {
	if step.From == "" || step.To == "" {
		return errors.New("copy action requires from and to")
	}
	src := step.From
	if strings.HasPrefix(src, "cache://") {
		src = filepath.Join(r.paths.ArtifactsDir, strings.TrimPrefix(src, "cache://"))
	}
	if err := os.MkdirAll(filepath.Dir(step.To), 0o755); err != nil {
		return fmt.Errorf("make dest dir: %w", err)
	}
	if err := copyFile(src, step.To); err != nil {
		return err
	}
	if step.VerifySHA256 != "" {
		hash, err := fileSHA256(step.To)
		if err != nil {
			return fmt.Errorf("hash dest: %w", err)
		}
		if !strings.EqualFold(hash, step.VerifySHA256) {
			return fmt.Errorf("checksum mismatch: expected %s got %s", step.VerifySHA256, hash)
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
			if _, err := os.Stat(assertion.Path); err != nil {
				return fmt.Errorf("verify file_exists failed: %w", err)
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
