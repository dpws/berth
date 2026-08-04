package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// berth is editing a file it did not write and cannot parse, so the one thing
// it has to promise is that the original is still there afterwards.
func TestTheOriginalIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmux.conf")
	original := "set -g mouse on\nset -g history-limit 50000\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := appendLine(path, "set -s extended-keys on"); err != nil {
		t.Fatal(err)
	}

	if got := read(t, BackupPath(path)); got != original {
		t.Errorf("the copy is not what was there before:\n%q", got)
	}
	body := read(t, path)
	if !strings.HasPrefix(body, original) {
		t.Errorf("what was already there did not survive:\n%q", body)
	}
	if !strings.Contains(body, "set -s extended-keys on") {
		t.Errorf("the line was not added:\n%q", body)
	}
	if !strings.Contains(body, marker) {
		t.Errorf("berth's additions are not labelled:\n%q", body)
	}
}

// These files are read top to bottom, so a second copy of a setting is at best
// noise and at worst an argument with the first.
func TestALineAlreadyThereIsNotRepeated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmux.conf")
	original := "set -s extended-keys on\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := appendLine(path, "set -s extended-keys on"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != original {
		t.Errorf("the file was touched when it already said the right thing:\n%q", got)
	}
	// Nothing changed, so there was nothing to back up.
	if _, err := os.Stat(BackupPath(path)); err == nil {
		t.Error("a copy was made although the file was left alone")
	}
}

// Spacing is not meaning in these files.
func TestSpacingDoesNotMakeALineNew(t *testing.T) {
	for _, existing := range []string{
		"set -s extended-keys on",
		"  set  -s   extended-keys  on  ",
		"\tset -s extended-keys on",
	} {
		if !hasLine(existing, "set -s extended-keys on") {
			t.Errorf("%q was not recognised as already saying it", existing)
		}
	}
	// A commented-out line is not in force, so it does not count.
	if hasLine("# set -s extended-keys on", "set -s extended-keys on") {
		t.Error("a commented-out line was taken as already set")
	}
	// And a different setting is not the same setting.
	if hasLine("set -s extended-keys off", "set -s extended-keys on") {
		t.Error("a different value was taken as a match")
	}
}

func TestAMissingFileIsCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config")
	if err := appendLine(path, "clipboard-write = allow"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); !strings.Contains(got, "clipboard-write = allow") {
		t.Errorf("the file was not written:\n%q", got)
	}
	// Nothing was there to lose, so nothing was copied aside.
	if _, err := os.Stat(BackupPath(path)); err == nil {
		t.Error("a copy was made of a file that did not exist")
	}
}

// A file without a trailing newline must not have berth's line run onto the end
// of its last one.
func TestALineIsNotJoinedToTheLastOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmux.conf")
	if err := os.WriteFile(path, []byte("set -g mouse on"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendLine(path, "set -s extended-keys on"); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(read(t, path), "\n") {
		if strings.Count(line, "set ") > 1 {
			t.Errorf("two settings ran together on one line: %q", line)
		}
	}
}

// Several findings are usually fixed in one go. The copy has to be the file as
// it was before berth touched it at all, not as it was before the last of
// them - a backup containing berth's own additions is not the thing anyone
// wants back.
func TestTheCopyIsOfTheFileBeforeBerthTouchedIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmux.conf")
	original := "set -g history-limit 50000\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, line := range []string{
		"set -s extended-keys on",
		"set -g focus-events on",
		"set -g mouse on",
	} {
		if err := appendLine(path, line); err != nil {
			t.Fatal(err)
		}
	}

	if got := read(t, BackupPath(path)); got != original {
		t.Errorf("the copy has berth's own work in it:\n%q\nwant:\n%q", got, original)
	}
	body := read(t, path)
	for _, line := range []string{"extended-keys on", "focus-events on", "mouse on"} {
		if !strings.Contains(body, line) {
			t.Errorf("%q did not make it into the file:\n%s", line, body)
		}
	}
	// One marker, not one per line.
	if n := strings.Count(body, marker); n != 1 {
		t.Errorf("the file carries %d markers, want 1", n)
	}
}
