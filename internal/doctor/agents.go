package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// claudeSettings is where Claude Code keeps its settings on every platform it
// runs on. It is the same path throughout: Claude Code puts its own directory
// in the home directory rather than following each system's conventions.
func claudeSettings() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, ".claude", "settings.json"), true
}

// claudeStatusline checks whether Claude Code has been pointed at berth's
// status line hook.
//
// This is the one check for something berth offers rather than something berth
// needs: without it everything works, and with it Claude Code's own status bar
// gains the rate limit figures berth reads. It is reported because there is no
// way to discover it exists from inside berth.
func claudeStatusline() Finding {
	f := Finding{
		Key: "claude_statusline", Subject: Agents,
		Summary: "Claude Code shows berth's status line",
		Detail: "berth can draw Claude Code's status bar, adding the real rate limit " +
			"windows it reads from the session logs. Without it nothing is missing " +
			"from berth; Claude Code just keeps whatever status line it had.",
		Command: `set "statusLine": {"type": "command", "command": "berth statusline"} in ~/.claude/settings.json`,
	}

	path, ok := claudeSettings()
	if !ok {
		f.Severity = Unknown
		return f
	}
	body, err := os.ReadFile(path)
	if err != nil {
		// No Claude Code settings at all is not a problem to report: it most
		// likely means Claude Code is not in use on this machine.
		f.Severity = OK
		f.Summary = "Claude Code is not set up here, so it has no status line to change"
		return f
	}

	var settings struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		f.Severity = Unknown
		f.Summary = "could not read ~/.claude/settings.json"
		f.Detail = "It did not parse as JSON, so berth left it alone rather than guessing."
		return f
	}
	if strings.Contains(settings.StatusLine.Command, "berth") {
		f.Severity = OK
		return f
	}
	if settings.StatusLine.Command != "" {
		// Someone has already chosen a status line. Replacing it is not
		// berth's call, and berth's own can pass the payload on to it anyway.
		f.Severity = Degraded
		f.Summary = "Claude Code has a status line of its own"
		f.Detail = "berth's can wrap it rather than replace it - it passes the payload " +
			"on to the command you give it - but swapping yours out is not something " +
			"berth will do behind your back."
		f.Command = `set "statusLine".command to: berth statusline ` + settings.StatusLine.Command
		return f
	}

	f.Severity = Degraded
	f.Summary = "Claude Code is not showing berth's status line"
	return f
}
