//go:build windows

package autostart

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	modAdvapi32     = syscall.NewLazyDLL("advapi32.dll")
	procRegSetValue = modAdvapi32.NewProc("RegSetValueExW")
	procRegDelValue = modAdvapi32.NewProc("RegDeleteValueW")
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "AttackSharkX11Battery"
)

// IsEnabled checks whether the app is registered in HKCU\...\Run.
func IsEnabled() bool {
	keyPath, err := syscall.UTF16PtrFromString(runKeyPath)
	if err != nil {
		return false
	}
	var hKey syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, keyPath, 0, syscall.KEY_READ, &hKey); err != nil {
		return false
	}
	defer syscall.RegCloseKey(hKey)

	valName, _ := syscall.UTF16PtrFromString(valueName)
	var typ uint32
	var size uint32
	err = syscall.RegQueryValueEx(hKey, valName, nil, &typ, nil, &size)
	return err == nil && size > 0
}

// SetEnabled enables or disables automatic startup with Windows for the current user.
func SetEnabled(enable bool) error {
	keyPath, err := syscall.UTF16PtrFromString(runKeyPath)
	if err != nil {
		return err
	}
	var hKey syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, keyPath, 0, syscall.KEY_SET_VALUE, &hKey); err != nil {
		return fmt.Errorf("ouverture registre Run: %w", err)
	}
	defer syscall.RegCloseKey(hKey)

	valName, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return err
	}

	if !enable {
		r, _, _ := procRegDelValue.Call(uintptr(hKey), uintptr(unsafe.Pointer(valName)))
		if r != 0 && syscall.Errno(r) != syscall.ERROR_FILE_NOT_FOUND {
			return syscall.Errno(r)
		}
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf(`"%s"`, exePath)
	data, err := syscall.UTF16FromString(cmd)
	if err != nil {
		return err
	}

	r, _, _ := procRegSetValue.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(valName)),
		0,
		syscall.REG_SZ,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)*2),
	)
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}
