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

// playbackPriority: a transcode someone is watching runs above normal priority. Windows treats a
// windowless child of a tray app as background work and keeps it on a hybrid CPU's efficiency
// cores: on an i9-14900KF the moflex encoder ran at 2.8x real time that way and 4.6x above normal
// (the same as pinning it to the performance cores). Above normal is enough; it never starves
// the desktop the way high priority can.
func playbackPriority(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.CreationFlags |= 0x00008000 // ABOVE_NORMAL_PRIORITY_CLASS
}
