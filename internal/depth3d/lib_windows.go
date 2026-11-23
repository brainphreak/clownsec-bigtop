//go:build windows

package depth3d

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

// DirectML.dll sits next to onnxruntime.dll: loading with the library's own folder searched first
// picks that one, not an older copy in System32.
const loadWithAlteredSearchPath = 0x00000008

func openLibrary(path string) (uintptr, error) {
	p, err := syscall.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return 0, err
	}
	h, err := loadLibraryEx(p, loadWithAlteredSearchPath)
	return uintptr(h), err
}

var procLoadLibraryExW = syscall.NewLazyDLL("kernel32.dll").NewProc("LoadLibraryExW")

func loadLibraryEx(name *uint16, flags uintptr) (syscall.Handle, error) {
	r, _, e := procLoadLibraryExW.Call(uintptr(unsafe.Pointer(name)), 0, flags)
	if r == 0 {
		return 0, e
	}
	return syscall.Handle(r), nil
}

func symbol(lib uintptr, name string) (uintptr, error) {
	return syscall.GetProcAddress(syscall.Handle(lib), name)
}
