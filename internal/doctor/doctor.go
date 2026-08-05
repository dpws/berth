// Package doctor inspects the software berth leans on and says what is not set
// the way berth needs it.
//
// berth is a thin thing sitting on top of tmux, a terminal emulator and a
// coding agent, and it only works as well as they are configured. The failures
// are quiet ones: shift+enter submits a half-written prompt because tmux will
// not pass the key through, colour is rounded to 256 because tmux was never
// told the terminal can do better, a session never learns it lost the keyboard.
// None of them look like a berth bug, and all of them are one line of
// configuration away from being fixed.
//
// Nothing here changes anything unless Fix is called on a finding.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// probeTimeout bounds one check. Everything here shells out to something else,
// and berth would rather report an unknown than sit waiting on a program that
// is not answering.
const probeTimeout = 3 * time.Second

// Severity says how much a finding matters.
type Severity int

const (
	// Broken means something berth offers does not work at all.
	Broken Severity = iota
	// Degraded means it works, but not as well as it could.
	Degraded
	// OK means there is nothing to do.
	OK
	// Unknown means the check could not tell, which is not the same as being
	// fine and is never reported as such.
	Unknown
)

func (s Severity) String() string {
	switch s {
	case Broken:
		return "broken"
	case Degraded:
		return "degraded"
	case OK:
		return "ok"
	default:
		return "unknown"
	}
}

// Subject groups findings by which piece of software they are about, so a
// report reads as a few short lists rather than one long one.
type Subject string

const (
	Tmux     Subject = "tmux"
	Terminal Subject = "terminal"
	Agents   Subject = "agents"
)

// Finding is one thing the doctor looked at.
type Finding struct {
	// Key names the check. It goes in the config when a finding is skipped, so
	// it has to stay stable across versions once it exists.
	Key      string
	Subject  Subject
	Severity Severity

	// Summary is the one line a report shows.
	Summary string
	// Detail says what goes wrong while it is unfixed, in the terms of
	// something the user would notice rather than the setting's own.
	Detail string
	// Command is what a person would run to fix it themselves, shown whether
	// or not berth can do it.
	Command string

	// NeedsRestart marks a setting berth will not feel until it is started
	// again. tmux settles what it sends a client when the client attaches, so
	// turning a key option on from inside berth changes what the next berth
	// receives, not this one.
	NeedsRestart bool

	// fix applies the change. Nil means berth cannot do this one - some things
	// can only be described, because the file that would have to change is one
	// berth has no business editing.
	fix func() error
}

// Fixable reports whether berth can apply this itself.
func (f Finding) Fixable() bool { return f.fix != nil }

// Fix applies the change.
func (f Finding) Fix() error {
	if f.fix == nil {
		return fmt.Errorf("%s has to be done by hand: %s", f.Key, f.Command)
	}
	return f.fix()
}

// Check looks at one thing and reports what it found.
type Check func() Finding

// Run works through every check and returns what each one found, in a stable
// order so a report does not shuffle between runs.
func Run() []Finding {
	checks := []Check{
		tmuxInstalled,
		tmuxExtendedKeys,
		tmuxFocusEvents,
		tmuxMouse,
		tmuxTrueColor,
		tmuxEscapeTime,
		tmuxSetClipboard,
		terminalKeyboard,
		terminalClipboard,
		claudeStatusline,
	}
	out := make([]Finding, 0, len(checks))
	for _, c := range checks {
		out = append(out, c())
	}
	return out
}

// Actionable returns the findings worth telling someone about: the ones that
// are not already fine, less any the user has said to leave alone.
//
// Unknown is included. A check that could not tell is not the same as a check
// that passed, and quietly dropping it would make the report a worse answer
// than it is.
func Actionable(all []Finding, skipped []string) []Finding {
	skip := make(map[string]bool, len(skipped))
	for _, k := range skipped {
		skip[k] = true
	}
	out := make([]Finding, 0, len(all))
	for _, f := range all {
		if f.Severity == OK || skip[f.Key] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// run executes a command and returns its trimmed standard output.
func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.Output()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(probeTimeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", fmt.Errorf("%s did not answer within %s", name, probeTimeout)
	}
	return strings.TrimSpace(string(out)), err
}

// have reports whether a program is on the path.
func have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// inTmux reports whether berth is running inside tmux, which is the case the
// server options actually matter for.
func inTmux() bool { return os.Getenv("TMUX") != "" }
