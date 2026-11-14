//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// spawn starts the updated Big Top in its own console window (this one closes when we exit).
func spawn(path string, args []string, dataDir string) error {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
	return cmd.Start()
}
