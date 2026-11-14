package main

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminal: is Big Top running in a terminal window? (A character device isn't enough --
// /dev/null is one too.)
func isTerminal() bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)))
	return e == 0
}
