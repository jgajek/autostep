//go:build windows

package eventlog

import (
	"golang.org/x/sys/windows/svc/eventlog"
)

const source = "Autostep"

func writer() (*eventlog.Log, error) {
	return eventlog.Open(source)
}

// Info writes an informational event.
func Info(eventID uint32, message string) error {
	el, err := writer()
	if err != nil {
		return err
	}
	defer el.Close()
	return el.Info(eventID, message)
}

// Error writes an error event.
func Error(eventID uint32, message string) error {
	el, err := writer()
	if err != nil {
		return err
	}
	defer el.Close()
	return el.Error(eventID, message)
}
