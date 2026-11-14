//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

func openBrowser(u string) {
	if runtime.GOOS == "darwin" {
		exec.Command("open", u).Start()
		return
	}
	exec.Command("xdg-open", u).Start()
}
