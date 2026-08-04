//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW. Building the agent with -H=windowsgui
// keeps it from having a console of its own, but says nothing about the
// processes it starts: without this flag every clipboard read opens a console
// window, which appears on screen, takes the focus off whatever was in front,
// and disappears again. Pasting an image should not make the desktop flicker.
const createNoWindow = 0x08000000

// hideWindow keeps a helper process from opening a console.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
