package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RunStatus values.
const (
	StatusPending       = "pending"
	StatusRunning       = "running"
	StatusCompleted     = "completed"
	StatusFailed        = "failed"
	StatusPendingReboot = "pending_reboot"
)

// Store keeps durable run state on disk.
type Store struct {
	path string

	mu   sync.Mutex
	runs map[string]*RunRecord
}

// RunRecord tracks a single workflow run.
type RunRecord struct {
	RunID               string       `json:"run_id"`
	WorkflowName        string       `json:"workflow_name"`
	Status              string       `json:"status"`
	StartedAt           time.Time    `json:"started_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
	CurrentStepIndex    int          `json:"current_step_index"`
	PendingRebootNext   *int         `json:"pending_reboot_next,omitempty"`
	PendingBootMode     string       `json:"pending_boot_mode,omitempty"` // normal|safe
	Steps               []StepRecord `json:"steps"`
	LastError           string       `json:"last_error,omitempty"`
	ResumeDelaySeconds  int          `json:"resume_delay_seconds,omitempty"`
	TotalSteps          int          `json:"total_steps"`
	WorkflowDisplayName string       `json:"workflow_display_name,omitempty"`
}

// StepRecord stores per-step status.
type StepRecord struct {
	StepID string `json:"step_id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Open loads an existing store or creates a new one.
func Open(path string) (*Store, error) {
	s := &Store{
		path: path,
		runs: map[string]*RunRecord{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	content, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	if len(content) == 0 {
		return nil
	}
	var runs map[string]*RunRecord
	if err := json.Unmarshal(content, &runs); err != nil {
		return fmt.Errorf("parse state: %w", err)
	}
	s.runs = runs
	return nil
}

// Export returns a copy suitable for printing/status.
func (s *Store) Export() map[string]*RunRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string]*RunRecord, len(s.runs))
	for k, v := range s.runs {
		clone := *v
		clone.Steps = append([]StepRecord(nil), v.Steps...)
		out[k] = &clone
	}
	return out
}

// StartRun initializes a run record.
func (s *Store) StartRun(runID, workflowName string, totalSteps int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.runs[runID]; exists {
		return fmt.Errorf("run %s already exists", runID)
	}

	s.runs[runID] = &RunRecord{
		RunID:            runID,
		WorkflowName:     workflowName,
		Status:           StatusRunning,
		StartedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		CurrentStepIndex: 0,
		Steps:            make([]StepRecord, totalSteps),
		TotalSteps:       totalSteps,
	}
	return s.persistLocked()
}

// MarkStepPending records a step as pending.
func (s *Store) MarkStepPending(runID string, stepIndex int, stepID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	rec.Status = StatusRunning
	rec.CurrentStepIndex = stepIndex
	rec.UpdatedAt = time.Now().UTC()
	rec.Steps[stepIndex] = StepRecord{StepID: stepID, Status: StatusPending}
	return s.persistLocked()
}

// MarkStepComplete records completion for a step.
func (s *Store) MarkStepComplete(runID string, stepIndex int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	rec.Steps[stepIndex] = StepRecord{StepID: rec.Steps[stepIndex].StepID, Status: StatusCompleted}
	rec.CurrentStepIndex = stepIndex
	rec.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

// MarkStepFailed stores failure state for a step and marks run failed.
func (s *Store) MarkStepFailed(runID string, stepIndex int, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	rec.Steps[stepIndex] = StepRecord{StepID: rec.Steps[stepIndex].StepID, Status: StatusFailed, Error: errMsg}
	rec.Status = StatusFailed
	rec.LastError = errMsg
	rec.UpdatedAt = time.Now().UTC()
	s.pruneHistoryLocked()
	return s.persistLocked()
}

// MarkPendingReboot records a reboot request and the next step.
func (s *Store) MarkPendingReboot(runID string, nextStep int, bootMode string, delaySeconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	rec.Status = StatusPendingReboot
	rec.PendingRebootNext = &nextStep
	rec.PendingBootMode = bootMode
	rec.ResumeDelaySeconds = delaySeconds
	rec.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

// ClearPendingReboot transitions a run from pending_reboot back to running.
func (s *Store) ClearPendingReboot(runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	rec.Status = StatusRunning
	rec.PendingRebootNext = nil
	rec.PendingBootMode = ""
	rec.ResumeDelaySeconds = 0
	rec.UpdatedAt = time.Now().UTC()
	return s.persistLocked()
}

// MarkRunCompleted marks a run as completed.
func (s *Store) MarkRunCompleted(runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("run %s not found", runID)
	}
	rec.Status = StatusCompleted
	rec.UpdatedAt = time.Now().UTC()
	s.pruneHistoryLocked()
	return s.persistLocked()
}

// pruneHistoryLocked keeps pending/incomplete runs and retains only the most recent completed/failed run.
func (s *Store) pruneHistoryLocked() {
	var latestKey string
	var latestTime time.Time
	for k, v := range s.runs {
		if v.Status == StatusCompleted || v.Status == StatusFailed {
			if v.UpdatedAt.After(latestTime) || latestKey == "" {
				latestKey = k
				latestTime = v.UpdatedAt
			}
		}
	}
	for k, v := range s.runs {
		if v.Status == StatusCompleted || v.Status == StatusFailed {
			if k != latestKey {
				delete(s.runs, k)
			}
		}
	}
}

func (s *Store) persistLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.runs, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
