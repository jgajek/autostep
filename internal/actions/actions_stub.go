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

func RegistrySave(path string, hiveFile string) error {
	return ErrUnsupported
}

func RegistryRestore(path string, hiveFile string) error {
	return ErrUnsupported
}

func RegistryLoad(path string, hiveFile string) error {
	return ErrUnsupported
}

func RegistryUnload(path string) error {
	return ErrUnsupported
}

func RegistryAppend(path string, suffix string) error {
	return ErrUnsupported
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

func ServiceStart(name string) error {
	return ErrUnsupported
}

func ServiceStop(name string) error {
	return ErrUnsupported
}

func ServiceRunning(name string) (bool, error) {
	return false, ErrUnsupported
}

func DriverLoad(name string, path string) error {
	return ErrUnsupported
}

func DriverUnload(name string) error {
	return ErrUnsupported
}

func DriverLoaded(name string) (bool, error) {
	return false, ErrUnsupported
}
