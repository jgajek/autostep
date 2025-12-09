package actions

import "errors"

// ErrUnsupported indicates an action is not supported on the current platform.
var ErrUnsupported = errors.New("action not supported on this platform")

// ErrRebooting indicates a reboot was requested; caller should stop processing.
var ErrRebooting = errors.New("reboot requested")

// Registry primitives.
func RegistrySet(path string, valueType string, value any) error { return ErrUnsupported }
func RegistryDeleteValue(path string) error                      { return ErrUnsupported }
func RegistryGetString(path string) (string, error)              { return "", ErrUnsupported }
func RegistrySave(path string, hiveFile string) error            { return ErrUnsupported }
func RegistryRestore(path string, hiveFile string) error         { return ErrUnsupported }
func RegistryLoad(path string, hiveFile string) error            { return ErrUnsupported }
func RegistryUnload(path string) error                           { return ErrUnsupported }
func RegistryAppend(path string, suffix string) error            { return ErrUnsupported }
func ServiceStart(name string) error                             { return ErrUnsupported }
func ServiceStop(name string) error                              { return ErrUnsupported }
func ServiceRunning(name string) (bool, error)                   { return false, ErrUnsupported }
func DriverLoad(name string, path string) error                  { return ErrUnsupported }
func DriverUnload(name string) error                             { return ErrUnsupported }
func DriverLoaded(name string) (bool, error)                     { return false, ErrUnsupported }
