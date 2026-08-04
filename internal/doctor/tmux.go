package doctor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// tmuxOption reads one tmux option. The scope flag is "-s" for a server option
// and "-g" for a global session one; tmux keeps them in different places and
// answers about the wrong one with an error rather than a value.
func tmuxOption(scope, name string) (string, error) {
	// The scope flag belongs to show, not to tmux: "tmux show -s name", not
	// "tmux -s show name", which tmux rejects as an unknown global flag.
	out, err := run("tmux", "show", scope, "-v", name)
	if err != nil {
		return "", err
	}
	return out, nil
}

// setTmuxOption applies an option to the running server and writes it to the
// config so it survives the server being restarted. Doing only the first would
// last until the next reboot; doing only the second would not take effect until
// then, and the point of fixing it is that shift+enter works now.
func setTmuxOption(scope, line string) func() error {
	return func() error {
		fields := strings.Fields(line)
		args := append([]string{"set", scope}, fields...)
		if _, err := run("tmux", args...); err != nil {
			return fmt.Errorf("applying it to the running server: %w", err)
		}
		return appendToTmuxConf(fmt.Sprintf("set %s %s", scope, line))
	}
}

func tmuxInstalled() Finding {
	f := Finding{
		Key: "tmux_installed", Subject: Tmux,
		Summary: "tmux is installed",
		Command: "install tmux 3.0 or newer from your package manager",
	}
	if !have("tmux") {
		f.Severity = Broken
		f.Summary = "tmux is not installed"
		f.Detail = "berth is a front end for tmux; without it there is nothing to run sessions in."
		return f
	}
	v, err := run("tmux", "-V")
	if err != nil {
		f.Severity = Unknown
		f.Detail = "tmux is on the path but would not say which version it is."
		return f
	}
	f.Severity = OK
	f.Summary = "tmux is installed (" + strings.TrimPrefix(v, "tmux ") + ")"
	return f
}

// tmuxExtendedKeys is the one that cost the most to find. Without it tmux
// hands on the same byte for enter and shift+enter, so a half-written prompt is
// submitted instead of gaining a line, and nothing in berth can tell.
func tmuxExtendedKeys() Finding {
	f := Finding{
		Key: "tmux_extended_keys", Subject: Tmux,
		Summary: "tmux passes on modified keys",
		Detail: "Without this, shift+enter reaches an agent as a plain return: " +
			"it sends the prompt instead of starting another line. alt+enter and " +
			"ctrl+j still work, since neither needs the terminal to say anything unusual.",
		Command:      "tmux set -s extended-keys on",
		NeedsRestart: true,
		fix:          setTmuxOption("-s", "extended-keys on"),
	}
	if !have("tmux") {
		f.Severity = Unknown
		return f
	}
	// This only bears on the keys berth is sent. When berth is the outermost
	// program its keys come from the terminal over its own input, and tmux is
	// downstream of that - it sees what berth chooses to write, not what was
	// pressed. Reporting the option as the reason shift+enter does nothing
	// would send someone to change a setting that was never in the way.
	if !inTmux() {
		f.Severity = OK
		f.Summary = "berth reads keys from the terminal directly, so this does not apply"
		f.Detail = "tmux only flattens modified keys on their way to a client. berth is " +
			"not running inside tmux here, so whether shift+enter arrives is between " +
			"berth and your terminal."
		return f
	}
	v, err := tmuxOption("-s", "extended-keys")
	if err != nil {
		f.Severity = Unknown
		f.Detail = "This tmux does not know the option, which means it is too old to pass modified keys on at all."
		return f
	}
	if v == "on" || v == "always" {
		f.Severity = OK
		f.Summary = "tmux passes on modified keys (" + v + ")"
		return f
	}
	f.Severity = Broken
	f.Summary = "tmux is dropping modified keys (extended-keys " + v + ")"
	return f
}

func tmuxFocusEvents() Finding {
	f := Finding{
		Key: "tmux_focus_events", Subject: Tmux,
		Summary: "tmux reports focus",
		Detail: "Without this a session never learns it has stopped being the one you " +
			"are typing into, so agents that dim or pause when unfocused never do.",
		Command: "tmux set -g focus-events on",
		fix:     setTmuxOption("-g", "focus-events on"),
	}
	if !have("tmux") {
		f.Severity = Unknown
		return f
	}
	v, err := tmuxOption("-g", "focus-events")
	if err != nil {
		f.Severity = Unknown
		return f
	}
	if v == "on" {
		f.Severity = OK
		return f
	}
	f.Severity = Degraded
	f.Summary = "tmux is not reporting focus"
	return f
}

func tmuxMouse() Finding {
	f := Finding{
		Key: "tmux_mouse", Subject: Tmux,
		Summary: "tmux takes the mouse",
		Detail: "berth forwards the wheel into the pane either way, but without this " +
			"nothing acts on it, so an agent that does not ask for mouse reporting - " +
			"Codex does not - cannot be scrolled at all.",
		Command: "tmux set -g mouse on",
		fix:     setTmuxOption("-g", "mouse on"),
	}
	if !have("tmux") {
		f.Severity = Unknown
		return f
	}
	v, err := tmuxOption("-g", "mouse")
	if err != nil {
		f.Severity = Unknown
		return f
	}
	if v == "on" {
		f.Severity = OK
		return f
	}
	f.Severity = Degraded
	f.Summary = "tmux is leaving the mouse alone"
	return f
}

// tmuxTrueColor asks whether berth will actually draw in truecolor.
//
// It is deliberately not a question about tmux's settings. Two things have to
// agree: tmux has to be willing to pass the colours on, and the environment has
// to say the terminal can take them - tmux sets TERM to tmux-256color whatever
// is underneath, so COLORTERM is what carries the answer through. Reporting on
// either alone would say the setting is right while the screen stayed flat.
func tmuxTrueColor() Finding {
	f := Finding{
		Key: "tmux_truecolor", Subject: Tmux,
		Summary: "tmux passes truecolor through",
		Detail: "Without this berth's colours are rounded to the nearest of 256. " +
			"Nothing is wrong, but the meters and the branch read better with the full range.",
		Command: `tmux set -as terminal-features ',*:RGB'`,
		fix:     setTmuxOption("-as", "terminal-features ,*:RGB"),
	}
	if !have("tmux") {
		f.Severity = Unknown
		return f
	}
	// Outside tmux there is nothing in the way, so this has nothing to say.
	if !inTmux() {
		f.Severity = OK
		f.Summary = "not running under tmux, so colour is the terminal's own"
		return f
	}
	// Ask what tmux settled on for this client rather than reading the table of
	// patterns. The table lists what every sort of terminal could have, and any
	// one entry mentioning RGB would otherwise pass a client that has none.
	drawing := colorprofile.Detect(os.Stdout, os.Environ())
	if drawing == colorprofile.TrueColor {
		f.Severity = OK
		f.Summary = "berth draws in truecolor"
		return f
	}

	// Ask what tmux settled on for this client rather than reading the table of
	// patterns. The table lists what every sort of terminal could have, and any
	// one entry mentioning RGB would otherwise pass a client that has none.
	feats, err := run("tmux", "display", "-p", "#{client_termfeatures}")
	if err != nil {
		f.Severity = Unknown
		return f
	}
	f.Severity = Degraded
	if strings.Contains(feats, "RGB") || strings.Contains(feats, "Tc") {
		// tmux is willing; nothing told berth the terminal could take it.
		f.Summary = "berth is drawing in 256 colours although tmux would pass more"
		f.Detail = "tmux will pass truecolor through, but nothing in the environment says " +
			"the terminal can take it, so berth stays within 256. COLORTERM is what " +
			"carries that, and tmux does not set it for you."
		f.Command = `set COLORTERM=truecolor in your shell profile`
		f.fix = nil // someone else's shell profile is not berth's to edit
		return f
	}
	f.Summary = "tmux is capping colour at 256"
	return f
}

// escapeTimeCeiling is where a plain escape starts to feel like a pause rather
// than a keystroke. tmux waits this long to see whether an escape is the start
// of a longer sequence, and vim and the agents both use escape on its own.
const escapeTimeCeiling = 50

func tmuxEscapeTime() Finding {
	f := Finding{
		Key: "tmux_escape_time", Subject: Tmux,
		Summary: "tmux answers escape promptly",
		Detail: "tmux waits this long to see whether an escape begins a longer sequence. " +
			"Above about 50ms, pressing esc in vim or an agent feels like it has been ignored.",
		Command: "tmux set -s escape-time 10",
		fix:     setTmuxOption("-s", "escape-time 10"),
	}
	if !have("tmux") {
		f.Severity = Unknown
		return f
	}
	v, err := tmuxOption("-s", "escape-time")
	if err != nil {
		f.Severity = Unknown
		return f
	}
	ms, err := strconv.Atoi(v)
	if err != nil {
		f.Severity = Unknown
		return f
	}
	if ms <= escapeTimeCeiling {
		f.Severity = OK
		f.Summary = fmt.Sprintf("tmux answers escape promptly (%dms)", ms)
		return f
	}
	f.Severity = Degraded
	f.Summary = fmt.Sprintf("tmux sits on escape for %dms", ms)
	return f
}
