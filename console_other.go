//go:build !windows

package main

func setupOutput(dataDir string, wantConsole bool) {}

// setTitle: the terminal's title (an escape sequence most terminals understand), when there is one.
func setTitle(t string) {
	if isTerminal() {
		print("\033]0;" + t + "\007")
	}
}

func hasConsoleNow() bool { return true }
