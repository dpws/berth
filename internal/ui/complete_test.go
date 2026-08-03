package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	tea_KeyTab   = tea.KeyTab
	tea_KeyEnter = tea.KeyEnter
	tea_KeyEsc   = tea.KeyEsc
	tea_KeyRight = tea.KeyRight
)

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

// The directory field starts on the configured default rather than showing it
// as a hint, so it can be completed further and is visibly what you will get.
func TestNewSessionFormPrefillsTheDefaultDirectory(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.cfg.DefaultDir = "/home/someone/code"

	m.Update(keyRune('n'))

	want := "/home/someone/code" + string(os.PathSeparator)
	if got := m.dirInput.Value(); got != want {
		t.Errorf("dir field = %q, want %q", got, want)
	}
	// The cursor sits at the end, ready for the next path segment.
	if m.dirInput.Position() != len([]rune(want)) {
		t.Errorf("cursor at %d, want the end of %q", m.dirInput.Position(), want)
	}
}

// Changing the default in settings has to reach the dialog, which held a copy
// taken when the input was first built.
func TestChangingTheDefaultDirectoryReachesTheForm(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))

	m.Update(keyRune(','))
	cursorTo(t, m, "default_dir")
	m.Update(keyType(tea_KeyEnter))
	m.settingInput.SetValue("/var/tmp/elsewhere")
	m.Update(keyType(tea_KeyEnter))
	m.Update(keyType(tea_KeyEsc))

	m.Update(keyRune('n'))
	want := "/var/tmp/elsewhere" + string(os.PathSeparator)
	if got := m.dirInput.Value(); got != want {
		t.Errorf("dir field = %q, want the new default %q", got, want)
	}
	if m.dirInput.Placeholder != "/var/tmp/elsewhere" {
		t.Errorf("placeholder = %q, want it refreshed", m.dirInput.Placeholder)
	}
}

func TestDefaultDirValue(t *testing.T) {
	sep := string(os.PathSeparator)
	cases := map[string]string{
		"":              "",
		"/home/x":       "/home/x" + sep,
		"/home/x" + sep: "/home/x" + sep,
	}
	for in, want := range cases {
		if got := defaultDirValue(in); got != want {
			t.Errorf("defaultDirValue(%q) = %q, want %q", in, got, want)
		}
	}
}

// Completion may only add. A directory whose children share no prefix would
// otherwise lose its trailing separator, and the next tab would look among its
// siblings instead of inside it.
func TestCompleteDirNeverShortensTheInput(t *testing.T) {
	root := tree(t, "alpha", "beta", "gamma")
	typed := root + string(filepath.Separator)

	got, matches := completeDir(typed)
	if got != typed {
		t.Errorf("completing %q gave %q, want it unchanged", typed, got)
	}
	if len(matches) != 3 {
		t.Errorf("matches = %q, want all three offered", matches)
	}

	// And it is still the same directory being offered on the next press.
	again, matchesAgain := completeDir(got)
	if again != typed || len(matchesAgain) != 3 {
		t.Errorf("second tab gave %q with %q", again, matchesAgain)
	}
}

// A path written with a "~" must descend as well as one written in full.
// filepath.Join cleaned the trailing separator off the expansion, so
// "~/projects/" was completed among the neighbours of projects, not its
// contents - and no amount of tabbing could get further in.
func TestCompleteDirDescendsThroughATilde(t *testing.T) {
	root := tree(t, filepath.Join("projects", "berth"), filepath.Join("projects", "notes"))
	t.Setenv("HOME", root)

	typed := "~" + string(filepath.Separator) + "projects" + string(filepath.Separator)
	got, matches := completeDir(typed)
	if len(matches) != 2 {
		t.Fatalf("matches = %q, want the two directories inside projects", matches)
	}
	if got != typed {
		t.Errorf("completing %q gave %q, want it unchanged", typed, got)
	}

	// And one candidate inside completes into it rather than beside it.
	one, _ := completeDir(typed + "be")
	if want := typed + "berth" + string(filepath.Separator); one != want {
		t.Errorf("completed to %q, want %q", one, want)
	}
}

// The shared prefix gives way a character at a time. Trimming bytes left half
// a rune in the field for names that part inside a multibyte character - every
// accented Latin letter shares its lead byte with the others.
func TestCommonPrefixKeepsWholeCharacters(t *testing.T) {
	cases := []struct {
		names []string
		want  string
	}{
		{[]string{"Bücher", "Bäume"}, "B"},
		{[]string{"süß", "süßer"}, "süß"},
		{[]string{"alpha", "beta"}, ""},
	}
	for _, c := range cases {
		got := commonPrefix(c.names)
		if got != c.want {
			t.Errorf("commonPrefix(%q) = %q, want %q", c.names, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("commonPrefix(%q) = %q, which is not valid UTF-8", c.names, got)
		}
	}
}
