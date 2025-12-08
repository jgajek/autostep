package eventlog

// Stub for non-Windows builds. In the future, implement structured Event Log writes.

func Info(eventID uint32, message string) error  { return nil }
func Error(eventID uint32, message string) error { return nil }
