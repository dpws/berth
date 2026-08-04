//go:build !windows

package main

import "os/exec"

// hideWindow has nothing to do anywhere but Windows: no other platform opens a
// window for a process that never asked for one.
func hideWindow(*exec.Cmd) {}
