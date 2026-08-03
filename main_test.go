package main

import (
	"bytes"
	"strings"
	"testing"
)

// A command nobody can find may as well not exist. This fails when one is
// added without a line in the usage text.
func TestUsageListsEverySubcommand(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	got := buf.String()

	for _, cmd := range []string{"ls", "update", "help", "statusline"} {
		if !strings.Contains(got, "berth "+cmd) {
			t.Errorf("usage does not mention %q", cmd)
		}
	}
	// The keys and the settings screen are only reachable from inside, so the
	// way in has to be said somewhere a new reader will look.
	for _, hint := range []string{"?", ",", "-version", "-write-config"} {
		if !strings.Contains(got, hint) {
			t.Errorf("usage does not mention %q", hint)
		}
	}
}

func TestUsageNamesTheConfigPath(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	if !strings.Contains(buf.String(), "config.json") {
		t.Error("usage does not say where the config lives")
	}
}
