//go:build windows

package media

import (
	"os/exec"
	"syscall"
)

// hideWindow: Big Top is a tray app with no console, so each ffmpeg/ffprobe would otherwise
// flash a console window of its own.
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
