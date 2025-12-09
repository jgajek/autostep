//go:build windows

package actions

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// RegistrySet writes a registry value at the given path.
// Path format: HKLM\SOFTWARE\Vendor\Product\ValueName
// valueType: string | dword
func RegistrySet(path string, valueType string, value any) error {
	root, subkey, name, err := splitRegistryPath(path)
	if err != nil {
		return err
	}
	key, _, err := registry.CreateKey(root, subkey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	switch strings.ToLower(valueType) {
	case "string", "sz":
		return key.SetStringValue(name, fmt.Sprint(value))
	case "dword":
		v, err := toUint32(value)
		if err != nil {
			return err
		}
		return key.SetDWordValue(name, v)
	default:
		return fmt.Errorf("unsupported registry value type %q", valueType)
	}
}

// RegistryDeleteValue deletes a value from the registry.
func RegistryDeleteValue(path string) error {
	root, subkey, name, err := splitRegistryPath(path)
	if err != nil {
		return err
	}
	key, err := registry.OpenKey(root, subkey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.DeleteValue(name)
}

// RegistryGetString reads a string value.
func RegistryGetString(path string) (string, error) {
	root, subkey, name, err := splitRegistryPath(path)
	if err != nil {
		return "", err
	}
	key, err := registry.OpenKey(root, subkey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	if val, _, err := key.GetStringValue(name); err == nil {
		return val, nil
	}
	if dword, _, err := key.GetIntegerValue(name); err == nil {
		return fmt.Sprint(dword), nil
	}
	return "", fmt.Errorf("registry value %s not found or unsupported type", path)
}

// RegistrySave saves a registry key to a hive file.
func RegistrySave(path string, hiveFile string) error {
	if path == "" || hiveFile == "" {
		return fmt.Errorf("registry_save requires path and hive_file")
	}
	cmd := exec.Command("reg.exe", "save", path, hiveFile, "/y")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg save failed: %v output: %s", err, string(out))
	}
	return nil
}

// RegistryRestore restores a registry key from a hive file.
func RegistryRestore(path string, hiveFile string) error {
	if path == "" || hiveFile == "" {
		return fmt.Errorf("registry_restore requires path and hive_file")
	}
	// reg restore does not support /y; restore overwrites the key specified.
	cmd := exec.Command("reg.exe", "restore", path, hiveFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg restore failed: %v output: %s", err, string(out))
	}
	return nil
}

// RegistryLoad loads a hive into a key (usually under HKLM or HKU).
func RegistryLoad(path string, hiveFile string) error {
	if path == "" || hiveFile == "" {
		return fmt.Errorf("registry_load requires path and hive_file")
	}
	cmd := exec.Command("reg.exe", "load", path, hiveFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg load failed: %v output: %s", err, string(out))
	}
	return nil
}

// RegistryUnload unloads a hive from a key.
func RegistryUnload(path string) error {
	if path == "" {
		return fmt.Errorf("registry_unload requires path")
	}
	cmd := exec.Command("reg.exe", "unload", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg unload failed: %v output: %s", err, string(out))
	}
	return nil
}

// RegistryAppend appends a suffix to an existing string value.
func RegistryAppend(path string, suffix string) error {
	if path == "" {
		return fmt.Errorf("registry_append requires path")
	}
	root, subkey, name, err := splitRegistryPath(path)
	if err != nil {
		return err
	}
	key, err := registry.OpenKey(root, subkey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	current, _, err := key.GetStringValue(name)
	if err != nil {
		return fmt.Errorf("read existing value: %w", err)
	}
	return key.SetStringValue(name, current+suffix)
}

// ServiceStart starts a Windows service.
func ServiceStart(name string) error {
	if name == "" {
		return fmt.Errorf("service_start requires service")
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("open service %s: %w", name, err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service %s: %w", name, err)
	}
	return nil
}

// ServiceStop stops a Windows service.
func ServiceStop(name string) error {
	if name == "" {
		return fmt.Errorf("service_stop requires service")
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("open service %s: %w", name, err)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stop service %s: %w", name, err)
	}
	if status.State != svc.Stopped && status.State != svc.StopPending {
		return fmt.Errorf("service %s unexpected state after stop: %v", name, status.State)
	}
	return nil
}

// ServiceRunning reports whether a Windows service is running.
func ServiceRunning(name string) (bool, error) {
	if name == "" {
		return false, fmt.Errorf("service_running requires service")
	}
	m, err := mgr.Connect()
	if err != nil {
		return false, fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return false, fmt.Errorf("open service %s: %w", name, err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return false, fmt.Errorf("query service %s: %w", name, err)
	}
	return status.State == svc.Running, nil
}

// DriverLoad installs (if needed) and starts a kernel driver.
func DriverLoad(name string, path string) error {
	if name == "" || path == "" {
		return fmt.Errorf("driver_load requires driver_name and driver_path")
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		// Try to create if not present.
		cfg := mgr.Config{
			DisplayName:    name,
			ServiceType:    windows.SERVICE_KERNEL_DRIVER,
			StartType:      mgr.StartManual,
			BinaryPathName: path,
		}
		s, err = m.CreateService(name, path, cfg)
		if err != nil {
			return fmt.Errorf("create driver service %s: %w", name, err)
		}
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		// If already running, consider it success.
		if !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return fmt.Errorf("start driver %s: %w", name, err)
		}
	}
	return nil
}

// DriverUnload stops and deletes the kernel driver service.
func DriverUnload(name string) error {
	if name == "" {
		return fmt.Errorf("driver_unload requires driver_name")
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("open driver service %s: %w", name, err)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err == nil {
		for i := 0; i < 10 && status.State == svc.StopPending; i++ {
			time.Sleep(200 * time.Millisecond)
			status, err = s.Query()
			if err != nil {
				break
			}
		}
	}
	// Ignore stop errors; attempt delete regardless.
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete driver service %s: %w", name, err)
	}
	return nil
}

// DriverLoaded reports whether the kernel driver service is running.
func DriverLoaded(name string) (bool, error) {
	if name == "" {
		return false, fmt.Errorf("driver_loaded requires driver_name")
	}
	m, err := mgr.Connect()
	if err != nil {
		return false, fmt.Errorf("connect service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(name)
	if err != nil {
		return false, fmt.Errorf("open driver service %s: %w", name, err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return false, fmt.Errorf("query driver service %s: %w", name, err)
	}
	return status.State == svc.Running, nil
}

// RequestReboot asks Windows to reboot. safeMode flag is recorded by caller; entry into
// Safe Mode is handled by workflow steps (e.g., BCD edits) before this call.
func RequestReboot(safeMode bool) error {
	flags := uint32(windows.EWX_REBOOT | windows.EWX_FORCEIFHUNG)
	// The caller is responsible for preparing the boot mode; we only trigger reboot.
	return windows.ExitWindowsEx(flags, windows.SHTDN_REASON_MAJOR_OTHER)
}

// BcdeditSafeBoot toggles safeboot mode: mode can be "minimal", "network", or "off".
func BcdeditSafeBoot(mode string) error {
	mode = strings.ToLower(mode)
	var cmd *exec.Cmd
	switch mode {
	case "minimal":
		cmd = exec.Command("bcdedit", "/set", "{current}", "safeboot", "minimal")
	case "network":
		cmd = exec.Command("bcdedit", "/set", "{current}", "safeboot", "network")
	case "off", "none", "":
		cmd = exec.Command("bcdedit", "/deletevalue", "{current}", "safeboot")
	default:
		return fmt.Errorf("unknown safeboot mode %q", mode)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bcdedit safeboot (%s) failed: %v output: %s", mode, err, string(out))
	}
	return nil
}

// EnsureServiceSafeBoot registers the service for Safe Mode and Safe Mode with Networking.
func EnsureServiceSafeBoot(serviceName string) error {
	paths := []string{
		`SYSTEM\CurrentControlSet\Control\SafeBoot\Minimal\` + serviceName,
		`SYSTEM\CurrentControlSet\Control\SafeBoot\Network\` + serviceName,
	}
	for _, p := range paths {
		k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, p, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("create safeboot key %s: %w", p, err)
		}
		// Value must be "Service" to mark it as a service allowed in Safe Mode.
		if err := k.SetStringValue("", "Service"); err != nil {
			k.Close()
			return fmt.Errorf("set safeboot value %s: %w", p, err)
		}
		k.Close()
	}
	return nil
}

func splitRegistryPath(full string) (registry.Key, string, string, error) {
	parts := strings.Split(full, `\`)
	if len(parts) < 2 {
		return 0, "", "", fmt.Errorf("invalid registry path: %s", full)
	}
	rootStr := strings.ToUpper(parts[0])
	var root registry.Key
	switch rootStr {
	case "HKLM", "HKEY_LOCAL_MACHINE":
		root = registry.LOCAL_MACHINE
	case "HKCU", "HKEY_CURRENT_USER":
		root = registry.CURRENT_USER
	case "HKCR", "HKEY_CLASSES_ROOT":
		root = registry.CLASSES_ROOT
	case "HKU", "HKEY_USERS":
		root = registry.USERS
	default:
		return 0, "", "", fmt.Errorf("unsupported root: %s", rootStr)
	}
	if len(parts) < 3 {
		return 0, "", "", fmt.Errorf("registry path must include value name: %s", full)
	}
	subkey := strings.Join(parts[1:len(parts)-1], `\`)
	valueName := parts[len(parts)-1]
	return root, subkey, valueName, nil
}

func toUint32(v any) (uint32, error) {
	switch t := v.(type) {
	case uint32:
		return t, nil
	case int:
		if t < 0 {
			return 0, fmt.Errorf("negative dword not allowed")
		}
		return uint32(t), nil
	case int64:
		if t < 0 {
			return 0, fmt.Errorf("negative dword not allowed")
		}
		return uint32(t), nil
	case float64:
		if t < 0 {
			return 0, fmt.Errorf("negative dword not allowed")
		}
		return uint32(t), nil
	case string:
		var parsed uint32
		_, err := fmt.Sscanf(t, "%d", &parsed)
		return parsed, err
	default:
		return 0, fmt.Errorf("cannot convert %T to dword", v)
	}
}
