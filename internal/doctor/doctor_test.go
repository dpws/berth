package doctor

import (
	"strings"
	"testing"
)

// A check that could not tell is not a check that passed, and a report that
// quietly dropped it would be a worse answer than it has.
func TestUnknownIsReportedRatherThanAssumedFine(t *testing.T) {
	all := []Finding{
		{Key: "a", Severity: OK},
		{Key: "b", Severity: Unknown},
		{Key: "c", Severity: Degraded},
		{Key: "d", Severity: Broken},
	}
	got := Actionable(all, nil)
	if len(got) != 3 {
		t.Fatalf("got %d actionable, want 3 (everything but the ok one)", len(got))
	}
	for _, f := range got {
		if f.Severity == OK {
			t.Errorf("%s is fine and should not have been reported", f.Key)
		}
	}
}

// Skipping one is a decision about someone's own setup, and has to stick.
func TestSkippedFindingsAreLeftOut(t *testing.T) {
	all := []Finding{
		{Key: "tmux_mouse", Severity: Degraded},
		{Key: "tmux_extended_keys", Severity: Broken},
	}
	got := Actionable(all, []string{"tmux_mouse"})
	if len(got) != 1 || got[0].Key != "tmux_extended_keys" {
		t.Fatalf("got %+v, want only the one that was not skipped", got)
	}
	// Skipping everything leaves nothing to say.
	if n := len(Actionable(all, []string{"tmux_mouse", "tmux_extended_keys"})); n != 0 {
		t.Errorf("got %d after skipping both, want none", n)
	}
}

// Every finding needs a key that will not move, because a skip is recorded
// against it and would otherwise start pointing at a different check.
func TestEveryCheckHasAStableKeyAndSomethingToSay(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Run() {
		switch {
		case f.Key == "":
			t.Errorf("a %s finding has no key: %q", f.Subject, f.Summary)
		case seen[f.Key]:
			t.Errorf("two checks share the key %q", f.Key)
		}
		seen[f.Key] = true

		if f.Summary == "" {
			t.Errorf("%s has no summary", f.Key)
		}
		if f.Subject == "" {
			t.Errorf("%s belongs to no subject", f.Key)
		}
		// Anything not already fine has to say what it costs, or the report is
		// telling someone to change a setting without saying why.
		if f.Severity != OK && f.Detail == "" && f.Command == "" {
			t.Errorf("%s is %s but says nothing about what to do", f.Key, f.Severity)
		}
	}
}

// A finding berth cannot fix must say so rather than failing at the moment
// someone asks it to.
func TestUnfixableFindingsExplainThemselves(t *testing.T) {
	f := Finding{Key: "example", Command: "edit it by hand"}
	if f.Fixable() {
		t.Fatal("a finding with no fix reported itself as fixable")
	}
	err := f.Fix()
	if err == nil {
		t.Fatal("fixing an unfixable finding did not complain")
	}
	if !strings.Contains(err.Error(), "edit it by hand") {
		t.Errorf("the complaint does not say what to do instead: %v", err)
	}
}

// The severities are ordered so that the worst sorts first, which is what lets
// a report be read from the top.
func TestSeveritiesReadWorstFirst(t *testing.T) {
	if !(Broken < Degraded && Degraded < OK) {
		t.Error("severity order does not put the worst first")
	}
	for _, s := range []Severity{Broken, Degraded, OK, Unknown} {
		if s.String() == "" {
			t.Errorf("severity %d has no name", s)
		}
	}
}

// The terminal is worked out from what emulators announce about themselves,
// since TERM says what a terminal behaves like rather than what it is.
func TestTerminalIsRecognisedFromItsOwnAnnouncement(t *testing.T) {
	for _, tc := range []struct {
		env, val, want string
	}{
		{"TERM_PROGRAM", "iTerm.app", "iterm2"},
		{"TERM_PROGRAM", "WezTerm", "wezterm"},
		{"TERM_PROGRAM", "ghostty", "ghostty"},
		{"KITTY_WINDOW_ID", "1", "kitty"},
		{"ALACRITTY_WINDOW_ID", "1", "alacritty"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			for _, k := range []string{
				"TERM_PROGRAM", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR",
				"WEZTERM_PANE", "ALACRITTY_WINDOW_ID", "KONSOLE_VERSION", "TERM",
			} {
				t.Setenv(k, "")
			}
			t.Setenv(tc.env, tc.val)
			if got := terminalName(); got != tc.want {
				t.Errorf("terminalName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Not knowing which terminal it is has to read as not knowing, so the report
// can say to use alt+enter rather than claiming shift+enter will work.
func TestAnUnknownTerminalIsNotCalledFine(t *testing.T) {
	for _, k := range []string{
		"TERM_PROGRAM", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR",
		"WEZTERM_PANE", "ALACRITTY_WINDOW_ID", "KONSOLE_VERSION", "TERM",
	} {
		t.Setenv(k, "")
	}
	f := terminalKeyboard()
	if f.Severity == OK {
		t.Errorf("an unrecognised terminal was reported as fine: %q", f.Summary)
	}
	if !strings.Contains(f.Detail, "alt+enter") {
		t.Errorf("the report does not offer the key that always works: %q", f.Detail)
	}
}
