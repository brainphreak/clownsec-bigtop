//go:build !windows

package main

func fixConsole() {}

// setTitle: the terminal's title (an escape sequence most terminals understand), when there is one.
func setTitle(t string) {
	if isTerminal() {
		print("\033]0;" + t + "\007")
	}
}
