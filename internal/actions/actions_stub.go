//go:build !windows

package actions

// Stub implementations for non-Windows platforms to allow development on other OSes.

func RegistrySet(path string, valueType string, value any) error {
	return ErrUnsupported
}

func RegistryDeleteValue(path string) error {
	return ErrUnsupported
}

func RegistryGetString(path string) (string, error) {
	return "", ErrUnsupported
}

func RequestReboot(safeMode bool) error {
	return ErrUnsupported
}

func BcdeditSafeBoot(mode string) error {
	return ErrUnsupported
}

func EnsureServiceSafeBoot(serviceName string) error {
	return ErrUnsupported
}
