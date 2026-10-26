//go:build windows

package userenv

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x0001
	keySetValue     = 0x0002
	regSz           = 1
	regExpandSz     = 2
	errorSuccess    = 0
	errorNotFound   = 2
)

var (
	advapi32             = syscall.NewLazyDLL("advapi32.dll")
	procRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
	procRegDeleteKeyW    = advapi32.NewProc("RegDeleteKeyW")
)

// broadcastEnvChange tells the shell that HKCU\Environment changed.
// Tests replace it.
var broadcastEnvChange = sendBroadcast

func platformDefault() Store { return registryStore{path: `Environment`} }

type registryStore struct {
	path string
}

func (s registryStore) Notify() { broadcastEnvChange() }

func (s registryStore) Get(key string) (string, bool, error) {
	var value string
	var ok bool
	err := s.with(func(h syscall.Handle) error {
		v, found, err := queryString(h, key)
		if err != nil {
			return err
		}
		value, ok = v, found
		return nil
	})
	return value, ok, err
}

func (s registryStore) Set(key, value string) error {
	return s.with(func(h syscall.Handle) error {
		return setString(h, key, value)
	})
}

func (s registryStore) Delete(key string) error {
	return s.with(func(h syscall.Handle) error {
		name, err := syscall.UTF16PtrFromString(key)
		if err != nil {
			return err
		}
		r, _, _ := procRegDeleteValueW.Call(uintptr(h), uintptr(unsafe.Pointer(name)))
		if r == errorNotFound || r == errorSuccess {
			return nil
		}
		return fmt.Errorf("delete user environment variable %s: %w", key, syscall.Errno(r))
	})
}

func (s registryStore) with(fn func(syscall.Handle) error) error {
	path, err := syscall.UTF16PtrFromString(s.path)
	if err != nil {
		return err
	}
	var h syscall.Handle
	r, _, _ := procRegCreateKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(path)),
		0,
		0,
		0,
		keyQueryValue|keySetValue,
		0,
		uintptr(unsafe.Pointer(&h)),
		0,
	)
	if r != errorSuccess {
		return fmt.Errorf("open HKCU\\%s: %w", s.path, syscall.Errno(r))
	}
	defer procRegCloseKey.Call(uintptr(h))
	return fn(h)
}

func queryString(h syscall.Handle, key string) (string, bool, error) {
	name, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return "", false, err
	}
	var typ uint32
	var n uint32
	r, _, _ := procRegQueryValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		0,
		uintptr(unsafe.Pointer(&n)),
	)
	if r == errorNotFound {
		return "", false, nil
	}
	if r != errorSuccess {
		return "", false, fmt.Errorf("read user environment variable %s: %w", key, syscall.Errno(r))
	}
	if typ != regSz && typ != regExpandSz {
		return "", false, fmt.Errorf("user environment variable %s has unsupported registry type %d", key, typ)
	}
	if n == 0 {
		return "", true, nil
	}
	buf := make([]uint16, n/2+1)
	r, _, _ = procRegQueryValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)),
	)
	if r != errorSuccess {
		return "", false, fmt.Errorf("read user environment variable %s: %w", key, syscall.Errno(r))
	}
	return syscall.UTF16ToString(buf), true, nil
}

func setString(h syscall.Handle, key, value string) error {
	name, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return err
	}
	data, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	r, _, _ := procRegSetValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(name)),
		0,
		regSz,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)*2),
	)
	if r != errorSuccess {
		return fmt.Errorf("write user environment variable %s: %w", key, syscall.Errno(r))
	}
	return nil
}

// DeleteKey removes a registry key under HKCU. Tests use it for a private key.
func DeleteKey(path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, _ := procRegDeleteKeyW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(p)))
	if r == errorNotFound || r == errorSuccess {
		return nil
	}
	return fmt.Errorf("delete HKCU\\%s: %w", path, syscall.Errno(r))
}

func sendBroadcast() {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	env, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	proc.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 5000, 0)
}
