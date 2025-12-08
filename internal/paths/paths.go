package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// Paths holds resolved locations for runtime artifacts.
type Paths struct {
	Root         string
	Manifest     string
	WorkflowsDir string
	ArtifactsDir string
	StatePath    string
	LogsDir      string
}

// DefaultRoot returns the base data directory. AUTOSTEP_ROOT overrides the default.
func DefaultRoot() string {
	if env := os.Getenv("AUTOSTEP_ROOT"); env != "" {
		return env
	}
	if runtime.GOOS == "windows" {
		return `C:\ProgramData\Autostep`
	}
	return filepath.Join(".", "var", "autostep")
}

// FromRoot constructs the standard layout under a given root.
func FromRoot(root string) Paths {
	return Paths{
		Root:         root,
		Manifest:     filepath.Join(root, "manifest.json"),
		WorkflowsDir: filepath.Join(root, "workflows"),
		ArtifactsDir: filepath.Join(root, "artifacts"),
		StatePath:    filepath.Join(root, "state.json"),
		LogsDir:      filepath.Join(root, "logs"),
	}
}

// Ensure creates required directories if missing.
func Ensure(p Paths) error {
	dirs := []string{p.Root, p.WorkflowsDir, p.ArtifactsDir, p.LogsDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
