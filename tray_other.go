//go:build !windows

package main

// macOS and Linux keep their terminal window (or the AppImage's background run + the setup page's
// Quit button); only Windows has the tray icon.

type TrayActions struct {
	Version     string
	OpenPage    func()
	UpdateLabel func() (string, bool)
	Update      func()
	ShowLog     func()
	OpenData    func()
	Restart     func()
	Quit        func()
}

func runTray(TrayActions)                         {}
func trayNotify(title, text string)               {}
func trayRemove()                                 {}
func messageBox(title, text string, isError bool) {}

const haveTray = false
