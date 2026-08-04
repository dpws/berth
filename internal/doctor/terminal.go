package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// terminalName is a best guess at which emulator berth is running under.
//
// There is no reliable way to ask. TERM says what the terminal claims to
// behave like, not what it is, and inside tmux it says tmux. What the emulators
// worth knowing about have in common is that they announce themselves in the
// environment, and tmux passes that through.
func terminalName() string {
	// tmux sets TERM_PROGRAM to its own name, so inside tmux this says "tmux"
	// however the real terminal announced itself. The emulators worth knowing
	// about set a variable of their own as well, and tmux passes those through,
	// so the answer is further down rather than missing.
	if p := os.Getenv("TERM_PROGRAM"); p != "" && !strings.EqualFold(p, "tmux") {
		switch strings.ToLower(p) {
		case "iterm.app":
			return "iterm2"
		case "apple_terminal":
			return "apple terminal"
		case "wezterm":
			return "wezterm"
		case "ghostty":
			return "ghostty"
		case "vscode":
			return "vscode"
		}
		return strings.ToLower(p)
	}
	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "":
		return "kitty"
	case os.Getenv("GHOSTTY_RESOURCES_DIR") != "":
		return "ghostty"
	case os.Getenv("WEZTERM_PANE") != "":
		return "wezterm"
	case os.Getenv("ALACRITTY_WINDOW_ID") != "":
		return "alacritty"
	case os.Getenv("KONSOLE_VERSION") != "":
		return "konsole"
	case strings.Contains(os.Getenv("TERM"), "foot"):
		return "foot"
	}
	return ""
}

// kittyProtocol lists the terminals that can report shift+enter apart from
// enter, which they do by way of the keyboard protocol kitty defined.
var kittyProtocol = map[string]bool{
	"kitty": true, "ghostty": true, "wezterm": true, "foot": true, "iterm2": true,
}

// terminalKeyboard says whether the terminal berth is under can tell berth that
// shift was held. tmux passing the key on is only half of it: something has to
// have made the distinction in the first place.
func terminalKeyboard() Finding {
	name := terminalName()
	f := Finding{
		Key: "terminal_keyboard", Subject: Terminal,
		Summary: "the terminal can report shift+enter",
		Detail: "Terminals send the same byte for enter and shift+enter unless they " +
			"speak the keyboard protocol kitty defined. Where they cannot, alt+enter " +
			"and ctrl+j do the same job and need nothing configured.",
	}
	switch {
	case name == "":
		f.Severity = Unknown
		f.Summary = "could not tell which terminal this is"
		f.Detail = "Nothing in the environment named it, which is usual over SSH: ssh " +
			"carries TERM and little else, so the variables a terminal announces itself " +
			"with do not survive the hop. Run \"berth doctor --keys\" and press " +
			"shift+enter to see what actually arrives. If it reads the same as enter, " +
			"use alt+enter, which needs nothing from the terminal."
	case kittyProtocol[name]:
		f.Severity = OK
		f.Summary = "the terminal can report shift+enter (" + name + ")"
	default:
		f.Severity = Degraded
		f.Summary = name + " cannot report shift+enter"
		f.Command = "use alt+enter or ctrl+j instead, or switch to kitty, ghostty, WezTerm or foot"
	}
	return f
}

// clipboardConf is where a terminal keeps the setting that lets a program put
// something on the clipboard, per terminal and per operating system.
func clipboardConf(name string) (path, line string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", false
	}
	switch name {
	case "kitty":
		return filepath.Join(home, ".config", "kitty", "kitty.conf"),
			"clipboard_control write-clipboard write-primary read-clipboard read-primary", true
	case "ghostty":
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support",
				"com.mitchellh.ghostty", "config"), "clipboard-write = allow", true
		}
		return filepath.Join(home, ".config", "ghostty", "config"),
			"clipboard-write = allow", true
	}
	return "", "", false
}

// terminalClipboard checks that copying out of a session can actually reach the
// clipboard. berth hands a selection to the terminal with OSC 52 rather than
// writing it locally, which is what makes copying work over SSH - and which
// several terminals refuse until told otherwise.
func terminalClipboard() Finding {
	name := terminalName()
	f := Finding{
		Key: "terminal_clipboard", Subject: Terminal,
		Summary: "the terminal accepts a copy",
		Detail: "berth hands a dragged selection to your terminal with OSC 52, so it " +
			"lands on the clipboard of the machine you are sitting at rather than the " +
			"one berth is running on. Terminals that refuse it copy nothing, silently.",
	}

	path, line, known := clipboardConf(name)
	if !known {
		f.Severity = Unknown
		if name == "" {
			f.Summary = "could not tell whether the terminal accepts a copy"
		} else {
			f.Summary = "cannot check whether " + name + " accepts a copy"
			f.Command = "look for its OSC 52 or clipboard setting; iTerm2 calls it " +
				`"applications may access the clipboard", xterm allowWindowOps`
		}
		return f
	}

	body, err := os.ReadFile(path)
	if err != nil {
		f.Severity = Degraded
		f.Summary = name + " has no clipboard setting yet"
		f.Command = "add to " + path + ":\n    " + line
		f.fix = func() error { return appendLine(path, line) }
		return f
	}
	if hasLine(string(body), line) || strings.Contains(string(body), strings.Fields(line)[0]) {
		f.Severity = OK
		f.Summary = "the terminal accepts a copy (" + name + ")"
		return f
	}
	f.Severity = Degraded
	f.Summary = name + " may be refusing copies"
	f.Command = "add to " + path + ":\n    " + line
	f.fix = func() error { return appendLine(path, line) }
	return f
}
