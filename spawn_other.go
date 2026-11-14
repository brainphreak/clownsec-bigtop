//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// spawn starts the updated Big Top. A Mac running us in Terminal gets the new one in a new
// Terminal window; otherwise (an AppImage, a service, a closed terminal) it runs on its own,
// writing to bigtop.log in the data folder -- the setup page's Quit button stops it.
func spawn(path string, args []string, dataDir string) error {
	if runtime.GOOS == "darwin" && isTerminal() {
		q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
		line := q(path)
		for _, a := range args {
			line += " " + q(a)
		}
		script := `tell application "Terminal" to do script "` + strings.ReplaceAll(strings.ReplaceAll(line, `\`, `\\`), `"`, `\"`) + `"`
		return exec.Command("osascript", "-e", script).Run()
	}
	logf, err := os.OpenFile(filepath.Join(dataDir, "bigtop.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // survives this process and its terminal
	return cmd.Start()
}

func isTerminal() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
