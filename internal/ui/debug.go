package ui

// A TUI owns the screen, so printf debugging has to go somewhere else. Set
// BERTH_LOG to a file path to collect a trace of attaches and failures.

import (
	"log"
	"os"
)

func init() {
	p := os.Getenv("BERTH_LOG")
	if p == "" {
		p = os.Getenv("CLAUDEMUX_LOG") // pre-rename name
	}
	if p != "" {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			log.SetOutput(f)
			log.SetFlags(log.Ltime | log.Lmicroseconds)
			debugOn = true
		}
	}
}

var debugOn bool

func dbg(format string, args ...any) {
	if debugOn {
		log.Printf(format, args...)
	}
}
