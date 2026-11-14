//go:build windows

package main

import (
	"os"
	"os/exec"
)

// spawn starts the updated Big Top in THIS console window: it shares our input and output, so
// when we exit the window stays open with the new version running in it (a console lives while
// any program is attached). A separate window left two behind -- this one, and the new one, blank.
func spawn(path string, args []string, dataDir string) error {
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Start()
}
