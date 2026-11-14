//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// openBrowser asks Windows to open the page the normal way (ShellExecute), rather than through
// rundll32 -- a trick antivirus software rightly distrusts.
func openBrowser(u string) {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	exec := shell32.NewProc("ShellExecuteW")
	verb, _ := syscall.UTF16PtrFromString("open")
	url, _ := syscall.UTF16PtrFromString(u)
	exec.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(url)), 0, 0, 1) // SW_SHOWNORMAL
}
