package logging

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// Setup returns a logger that writes to stdout and a rotating log file path (no rotation implemented yet).
func Setup(logsDir string) (*log.Logger, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(logsDir, "autostep.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	mw := io.MultiWriter(os.Stdout, f)
	logger := log.New(mw, "", log.LstdFlags|log.Lmicroseconds)
	return logger, nil
}
