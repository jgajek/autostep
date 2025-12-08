package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WorkflowRef describes a workflow entry in manifest.json.
type WorkflowRef struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Version   string   `json:"version,omitempty"`
	Artifacts []string `json:"artifacts,omitempty"`
}

// Manifest maps workflow names to definitions/artifacts.
type Manifest struct {
	Workflows []WorkflowRef `json:"workflows"`
}

// Load reads a manifest from disk.
func Load(path string) (*Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(content, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return &m, nil
}

// Find locates a workflow by name.
func (m *Manifest) Find(name string) (*WorkflowRef, bool) {
	for _, wf := range m.Workflows {
		if wf.Name == name {
			// Resolve paths to a canonical form (e.g., normalize separators).
			wf.Path = filepath.Clean(wf.Path)
			return &wf, true
		}
	}
	return nil, false
}
