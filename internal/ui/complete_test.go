package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const tea_KeyTab = tea.KeyTab

func keyRune(r rune) tea.KeyMsg        { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func tree(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCompleteDirExtendsToTheOnlyMatch(t *testing.T) {
	root := tree(t, "projects")
	got, matches := completeDir(filepath.Join(root, "pro"))

	want := filepath.Join(root, "projects") + string(filepath.Separator)
	if got != want {
		t.Errorf("completed to %q, want %q", got, want)
	}
	if len(matches) != 1 {
		t.Errorf("matches = %q, want one", matches)
	}
}

// With several candidates it goes as far as they agree and no further, which
// is what makes repeated tabs behave like a shell.
func TestCompleteDirStopsAtTheCommonPrefix(t *testing.T) {
	root := tree(t, "berth", "berth-drop", "berth-notes", "other")

	got, matches := completeDir(filepath.Join(root, "be"))
	if want := filepath.Join(root, "berth"); got != want {
		t.Errorf("completed to %q, want %q", got, want)
	}
	if len(matches) != 3 {
		t.Errorf("matches = %q, want the three berth directories", matches)
	}

	// No common ground left, so the text is returned untouched.
	got2, _ := completeDir(got)
	if got2 != got {
		t.Errorf("completing again changed %q to %q", got, got2)
	}
}

func TestCompleteDirIgnoresFilesAndHiddenDirectories(t *testing.T) {
	root := tree(t, "visible", ".hidden")
	if err := os.WriteFile(filepath.Join(root, "vfile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	_, matches := completeDir(root + string(filepath.Separator))
	for _, m := range matches {
		if m == "vfile" {
			t.Error("a file was offered as a directory")
		}
		if m == ".hidden" {
			t.Error("a hidden directory was offered unasked")
		}
	}

	// Asked for by name, it shows up. Built by hand rather than with
	// filepath.Join, which would clean the trailing dot away.
	_, matches = completeDir(root + string(filepath.Separator) + ".")
	if len(matches) != 1 || matches[0] != ".hidden" {
		t.Errorf("matches = %q, want the hidden directory", matches)
	}
}

func TestCompleteDirKeepsTheTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	got, matches := completeDir("~/")
	if len(matches) == 0 {
		t.Skip("nothing in the home directory to complete")
	}
	if !strings.HasPrefix(got, "~") {
		t.Errorf("completed to %q, want the ~ kept", got)
	}
}

func TestCompleteDirOnRubbish(t *testing.T) {
	got, matches := completeDir("/definitely/not/here/xyz")
	if got != "/definitely/not/here/xyz" || matches != nil {
		t.Errorf("got %q, %q; want the input back untouched", got, matches)
	}
}

func TestCommonPrefix(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"one"}, "one"},
		{[]string{"berth", "berth-drop"}, "berth"},
		{[]string{"alpha", "beta"}, ""},
	}
	for _, c := range cases {
		if got := commonPrefix(c.in); got != c.want {
			t.Errorf("commonPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Tab on the directory field completes; on the others it moves on.
func TestTabCompletesTheDirectoryField(t *testing.T) {
	root := tree(t, "projects")
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.Update(keyRune('n'))

	m.formField = fieldDir
	m.syncFormFocus()
	m.dirInput.SetValue(filepath.Join(root, "pro"))
	m.Update(keyType(tea_KeyTab))

	if m.formField != fieldDir {
		t.Error("tab left the directory field instead of completing")
	}
	if want := filepath.Join(root, "projects") + string(filepath.Separator); m.dirInput.Value() != want {
		t.Errorf("dir = %q, want %q", m.dirInput.Value(), want)
	}

	// Nothing further to add, so the next tab moves on.
	m.Update(keyType(tea_KeyTab))
	if m.formField == fieldDir {
		t.Error("tab did not move on once the path was complete")
	}
}
