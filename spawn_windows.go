//go:build windows

package main

import "os/exec"

// spawn starts the updated Big Top: a tray app, it puts its own icon up (this one goes when we
// exit). Started with -console, it opens its own console window.
func spawn(path string, args []string, dataDir string) error {
	return exec.Command(path, args...).Start()
}
