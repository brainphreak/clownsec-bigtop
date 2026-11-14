package main

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminal: is Big Top running in a terminal? (Not used for the restart on Linux, which always
// carries on in the background; kept so the shared spawn code builds.)
func isTerminal() bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return e == 0
}
