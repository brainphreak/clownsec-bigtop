//go:build windows

package main

import (
	"log"
	"os"
	"syscall"
	"unsafe"
)

// fixConsole: a Big Top started by the updater gets a console window of its own, but a started
// program's output goes to NUL unless told otherwise -- the window stayed empty and looked hung.
// When our output isn't a console, write to the window's console instead.
func fixConsole() {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	getMode := k32.NewProc("GetConsoleMode")
	var mode uint32
	if r, _, _ := getMode.Call(os.Stdout.Fd(), uintptr(unsafe.Pointer(&mode))); r != 0 {
		return // already a console
	}
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return // no console at all (a service): stay quiet
	}
	os.Stdout, os.Stderr = out, out
	log.SetOutput(out)
	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = in // fatal's "Press Enter to close"
	}
}

// setTitle names the console window ("Clownsec Big Top 261114.0432 - running"), so a minimised
// or freshly opened window says what it is.
func setTitle(t string) {
	p, err := syscall.UTF16PtrFromString(t)
	if err != nil {
		return
	}
	syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleTitleW").Call(uintptr(unsafe.Pointer(p)))
}
