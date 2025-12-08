//go:build windows

package actions

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
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
	val, _, err := key.GetStringValue(name)
	return val, err
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
