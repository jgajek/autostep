package actions

import "errors"

// ErrUnsupported indicates an action is not supported on the current platform.
var ErrUnsupported = errors.New("action not supported on this platform")

// ErrRebooting indicates a reboot was requested; caller should stop processing.
var ErrRebooting = errors.New("reboot requested")
