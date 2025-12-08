package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/autostep/autostep/internal/actions"
	"github.com/autostep/autostep/internal/logging"
	"github.com/autostep/autostep/internal/manifest"
	"github.com/autostep/autostep/internal/paths"
	"github.com/autostep/autostep/internal/runner"
	"github.com/autostep/autostep/internal/state"
	"github.com/autostep/autostep/internal/workflow"
	service "github.com/kardianos/service"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func usage() {
	fmt.Println("autostep usage:")
	fmt.Println("  autostep run <workflow-name>        # run a workflow once")
	fmt.Println("  autostep list                       # list available workflows from manifest")
	fmt.Println("  autostep status                     # show stored run state")
	fmt.Println("  autostep resume-pending             # resume pending runs (after reboot)")
	fmt.Println("  autostep serve                      # run as a service/daemon (skeleton)")
	fmt.Println("  autostep configure-safeboot-service # allow service to start in Safe Mode/Network (Windows)")
	fmt.Println("  autostep version                    # show version/build info")
	fmt.Println()
	fmt.Println("Environment:")
	fmt.Println("  AUTOSTEP_ROOT   override data root (default: ProgramData/Autostep on Windows)")
}

func main() {
	root := paths.DefaultRoot()
	p := paths.FromRoot(root)
	if err := paths.Ensure(p); err != nil {
		log.Fatalf("failed to ensure paths: %v", err)
	}

	logger, err := logging.Setup(p.LogsDir)
	if err != nil {
		log.Fatalf("failed to setup logging: %v", err)
	}

	// If running as a Windows service (non-interactive) and no args were supplied,
	// automatically start service mode so the SCM can launch us without arguments.
	if len(os.Args) < 2 {
		if !service.Interactive() {
			runService(logger, p)
			return
		}
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "run":
		if len(os.Args) < 3 {
			fmt.Println("missing workflow name")
			usage()
			os.Exit(1)
		}
		workflowName := os.Args[2]
		if err := runWorkflowOnce(logger, p, workflowName); err != nil {
			logger.Fatalf("run failed: %v", err)
		}
	case "list":
		if err := listWorkflows(p); err != nil {
			logger.Fatalf("list failed: %v", err)
		}
	case "status":
		if err := showStatus(p); err != nil {
			logger.Fatalf("status failed: %v", err)
		}
	case "resume-pending":
		if err := resumePending(logger, p); err != nil {
			logger.Fatalf("resume failed: %v", err)
		}
	case "serve":
		runService(logger, p)
	case "configure-safeboot-service":
		if err := configureSafeBootService(logger); err != nil {
			logger.Fatalf("configure safe boot: %v", err)
		}
	case "version":
		printVersion()
	default:
		fmt.Printf("unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}

func listWorkflows(p paths.Paths) error {
	m, err := manifest.Load(p.Manifest)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	fmt.Println("Workflows:")
	for _, wf := range m.Workflows {
		fmt.Printf("- %s (path: %s", wf.Name, wf.Path)
		if wf.Version != "" {
			fmt.Printf(", version: %s", wf.Version)
		}
		fmt.Println(")")
	}
	return nil
}

func showStatus(p paths.Paths) error {
	store, err := state.Open(p.StatePath)
	if err != nil {
		return fmt.Errorf("open state: %w", err)
	}

	data := store.Export()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func runWorkflowOnce(logger *log.Logger, p paths.Paths, workflowName string) error {
	wf, err := loadWorkflowByName(p, workflowName)
	if err != nil {
		return err
	}
	store, err := state.Open(p.StatePath)
	if err != nil {
		return fmt.Errorf("open state: %w", err)
	}

	r := runner.New(p, store, logger)
	runID := fmt.Sprintf("%s-%d", wf.Name, time.Now().UnixNano())

	ctx := context.Background()
	if err := r.RunWorkflow(ctx, runID, wf); err != nil {
		if errors.Is(err, actions.ErrRebooting) {
			logger.Printf("workflow %s requested reboot (run %s); will resume automatically on next boot", wf.Name, runID)
			return nil
		}
		return err
	}

	logger.Printf("workflow %s completed (run %s)", wf.Name, runID)
	return nil
}

func loadWorkflowByName(p paths.Paths, name string) (*workflow.Workflow, error) {
	m, err := manifest.Load(p.Manifest)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	ref, ok := m.Find(name)
	if !ok {
		return nil, fmt.Errorf("workflow %q not found in manifest", name)
	}

	wfPath := ref.Path
	if !filepath.IsAbs(wfPath) {
		wfPath = filepath.Join(p.WorkflowsDir, wfPath)
	}

	return workflow.Load(wfPath)
}

func resumePending(logger *log.Logger, p paths.Paths) error {
	store, err := state.Open(p.StatePath)
	if err != nil {
		return fmt.Errorf("open state: %w", err)
	}
	m, err := manifest.Load(p.Manifest)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	exports := store.Export()
	if len(exports) == 0 {
		logger.Println("no runs in state")
		return nil
	}

	r := runner.New(p, store, logger)
	ctx := context.Background()
	for runID, rec := range exports {
		if rec.Status != state.StatusPendingReboot || rec.PendingRebootNext == nil {
			continue
		}
		ref, ok := m.Find(rec.WorkflowName)
		if !ok {
			logger.Printf("manifest entry for workflow %s not found; skipping run %s", rec.WorkflowName, runID)
			continue
		}
		wfPath := ref.Path
		if !filepath.IsAbs(wfPath) {
			wfPath = filepath.Join(p.WorkflowsDir, wfPath)
		}
		wf, err := workflow.Load(wfPath)
		if err != nil {
			logger.Printf("failed to load workflow %s for run %s: %v", wfPath, runID, err)
			continue
		}
		start := *rec.PendingRebootNext
		logger.Printf("resuming run %s workflow %s at step %d", runID, rec.WorkflowName, start)
		if err := r.ContinueWorkflow(ctx, runID, wf, start); err != nil {
			logger.Printf("resume run %s failed: %v", runID, err)
		}
	}
	return nil
}

// runService installs and runs the service/daemon skeleton.
func runService(baseLogger *log.Logger, p paths.Paths) {
	svcConfig := &service.Config{
		Name:        "Autostep",
		DisplayName: "Autostep Workflow Agent",
		Description: "Runs declarative workflows with reboot/resume support.",
	}
	app := &svcApp{paths: p, baseLogger: baseLogger}
	s, err := service.New(app, svcConfig)
	if err != nil {
		baseLogger.Fatalf("service create: %v", err)
	}
	if err := s.Run(); err != nil {
		baseLogger.Fatalf("service run: %v", err)
	}
}

type svcApp struct {
	paths      paths.Paths
	baseLogger *log.Logger
	logger     *log.Logger
}

func (a *svcApp) Start(_ service.Service) error {
	logger, err := logging.Setup(a.paths.LogsDir)
	if err != nil {
		return err
	}
	a.logger = logger
	go func() {
		if err := resumePending(a.logger, a.paths); err != nil {
			a.logger.Printf("resume pending error: %v", err)
		}
	}()
	return nil
}

func (a *svcApp) Stop(_ service.Service) error {
	if a.logger != nil {
		a.logger.Println("service stopping")
	}
	return nil
}

func configureSafeBootService(logger *log.Logger) error {
	if err := actions.EnsureServiceSafeBoot("Autostep"); err != nil {
		return err
	}
	logger.Println("configured Autostep service for Safe Mode and Safe Mode with Networking")
	return nil
}

func printVersion() {
	fmt.Printf("autostep %s (commit %s, build %s)\n", version, commit, buildDate)
}
