package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Workflow describes a declarative set of steps.
type Workflow struct {
	Version int    `json:"version" yaml:"version"`
	Name    string `json:"name" yaml:"name"`
	Steps   []Step `json:"steps" yaml:"steps"`
}

// Step represents a single action in the workflow DSL.
type Step struct {
	ID                 string      `json:"id" yaml:"id"`
	Action             string      `json:"action" yaml:"action"`
	From               string      `json:"from,omitempty" yaml:"from,omitempty"`
	To                 string      `json:"to,omitempty" yaml:"to,omitempty"`
	VerifySHA256       string      `json:"verify_sha256,omitempty" yaml:"verify_sha256,omitempty"`
	Path               string      `json:"path,omitempty" yaml:"path,omitempty"` // e.g., registry path or file path
	Type               string      `json:"type,omitempty" yaml:"type,omitempty"` // e.g., registry type
	Value              any         `json:"value,omitempty" yaml:"value,omitempty"`
	Command            string      `json:"command,omitempty" yaml:"command,omitempty"`
	Args               []string    `json:"args,omitempty" yaml:"args,omitempty"`
	Assertions         []Assertion `json:"assertions,omitempty" yaml:"assertions,omitempty"`
	SleepSeconds       int         `json:"sleep_seconds,omitempty" yaml:"sleep_seconds,omitempty"`
	ResumeDelaySeconds int         `json:"resume_delay_seconds,omitempty" yaml:"resume_delay_seconds,omitempty"`
	SafeMode           bool        `json:"safe_mode,omitempty" yaml:"safe_mode,omitempty"`
	SafeBootMode       string      `json:"safe_boot_mode,omitempty" yaml:"safe_boot_mode,omitempty"` // minimal|network|off (for safeboot action)
	Env                []EnvVar    `json:"env,omitempty" yaml:"env,omitempty"`
	WorkingDir         string      `json:"working_dir,omitempty" yaml:"working_dir,omitempty"`
	Notes              string      `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// Assertion is used for verify steps.
type Assertion struct {
	Kind     string `json:"kind" yaml:"kind"` // e.g., file_exists, registry_equals
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
	Expected any    `json:"expected,omitempty" yaml:"expected,omitempty"`
}

// EnvVar represents an environment variable override.
type EnvVar struct {
	Key   string `json:"key" yaml:"key"`
	Value string `json:"value" yaml:"value"`
}

// Load reads a workflow from YAML or JSON based on file extension.
func Load(path string) (*Workflow, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow %s: %w", path, err)
	}
	var wf Workflow
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(content, &wf); err != nil {
			return nil, fmt.Errorf("parse yaml %s: %w", path, err)
		}
	default:
		if err := json.Unmarshal(content, &wf); err != nil {
			return nil, fmt.Errorf("parse json %s: %w", path, err)
		}
	}
	return &wf, nil
}
