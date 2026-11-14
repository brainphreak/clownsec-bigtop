//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// setupOutput: the Windows build is a tray app with no console, so what other systems print goes
// to bigtop.log in the data folder. -console asks for a console window instead (and a Big Top
// started from a command prompt keeps using that one).
func setupOutput(dataDir string, wantConsole bool) {
	if hasConsole() {
		return
	}
	if wantConsole {
		kernel32.NewProc("AllocConsole").Call()
		if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			os.Stdout, os.Stderr = out, out
			log.SetOutput(out)
			if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
				os.Stdin = in
			}
			return
		}
	}
	p := filepath.Join(dataDir, "bigtop.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 2<<20 { // keep it small: start over past 2 MB
		os.Rename(p, p+".old")
	}
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		os.Stdout, os.Stderr = f, f
		log.SetOutput(f)
	}
}

func hasConsole() bool {
	var mode uint32
	r, _, _ := kernel32.NewProc("GetConsoleMode").Call(os.Stdout.Fd(), uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

// setTitle names the console window, when there is one.
func setTitle(t string) {
	if !hasConsole() {
		return
	}
	p, err := syscall.UTF16PtrFromString(t)
	if err != nil {
		return
	}
	kernel32.NewProc("SetConsoleTitleW").Call(uintptr(unsafe.Pointer(p)))
}

func hasConsoleNow() bool { return hasConsole() }
