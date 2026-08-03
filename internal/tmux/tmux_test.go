package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"plain":           "plain",
		"with space":      "with-space",
		"dots.and:colons": "dots-and-colons",
		"  padded  ":      "padded",
		"--edges--":       "edges",
		"":                "",
		"tab\there":       "tab-here",
		"~/code/my.app":   "~/code/my-app",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// requireTmux skips a test when tmux is unavailable, so the suite still runs
// on machines without it.
func requireTmux(t *testing.T) {
	t.Helper()
	if err := Available(); err != nil {
		t.Skip("tmux not available:", err)
	}
}

func testName(t *testing.T, suffix string) string {
	t.Helper()
	return fmt.Sprintf("berth-test-%d-%s", os.Getpid(), suffix)
}

func TestSessionLifecycle(t *testing.T) {
	requireTmux(t)

	name := testName(t, "lifecycle")
	created, err := New(NewOptions{
		Name:          name,
		Dir:           t.TempDir(),
		Kind:          KindClaude,
		Command:       "sh",
		HideStatusBar: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = Kill(created) })

	if created != name {
		t.Fatalf("created session named %q, want %q", created, name)
	}
	if !Exists(created) {
		t.Fatal("session should exist after New")
	}

	got, ok := find(t, created)
	if !ok {
		t.Fatal("created session missing from List")
	}
	if !got.Managed {
		t.Error("session should be tagged as berth-managed")
	}
	if got.Kind != KindClaude {
		t.Errorf("kind = %q, want %q", got.Kind, KindClaude)
	}
	if got.Windows != 1 {
		t.Errorf("windows = %d, want 1", got.Windows)
	}

	renamed, err := Rename(created, testName(t, "renamed"))
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	t.Cleanup(func() { _ = Kill(renamed) })
	if Exists(created) {
		t.Error("old name should be gone after Rename")
	}
	if !Exists(renamed) {
		t.Error("new name should exist after Rename")
	}

	if err := Kill(renamed); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if Exists(renamed) {
		t.Error("session should be gone after Kill")
	}
}

func TestUniqueNameAvoidsCollision(t *testing.T) {
	requireTmux(t)

	name := testName(t, "dup")
	created, err := New(NewOptions{Name: name, Command: "sh"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = Kill(created) })

	second, err := New(NewOptions{Name: name, Command: "sh"})
	if err != nil {
		t.Fatalf("New (second): %v", err)
	}
	t.Cleanup(func() { _ = Kill(second) })

	if second == created {
		t.Fatalf("second session reused the name %q", second)
	}
	if want := name + "-2"; second != want {
		t.Errorf("second session named %q, want %q", second, want)
	}
}

// TestListParsesTabDelimitedOutput guards the format separator: tmux escapes
// control characters in its -F output, so anything but a literal tab silently
// produces unparseable lines.
func TestListParsesTabDelimitedOutput(t *testing.T) {
	requireTmux(t)

	name := testName(t, "parse")
	created, err := New(NewOptions{Name: name, Dir: t.TempDir(), Kind: KindShell, Command: "sh"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = Kill(created) })

	got, ok := find(t, created)
	if !ok {
		t.Fatal("session missing from List")
	}
	if got.Dir == "" {
		t.Error("session path should be populated")
	}
	if got.Created.IsZero() {
		t.Error("created timestamp should be populated")
	}
	if got.Command == "" {
		t.Error("pane command should be populated")
	}
}

func find(t *testing.T, name string) (Session, bool) {
	t.Helper()
	sessions, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, s := range sessions {
		if s.Name == name {
			return s, true
		}
	}
	return Session{}, false
}

// TestMain points every tmux command these tests run at a private socket, so
// the suite can never list, touch, or tear down the sessions you are actually
// working in. tmux derives its socket from $TMUX_TMPDIR, and the tmux
// processes we spawn inherit this environment.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "berth-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create a private tmux socket dir:", err)
		os.Exit(1)
	}
	os.Setenv("TMUX_TMPDIR", dir)
	// Never inherit an outer session: tmux would otherwise refuse to nest.
	os.Unsetenv("TMUX")

	// tmux shuts its server down when the last session goes away, and several
	// tests here kill the only session they created. Without something pinning
	// it, the next test races a server that is still exiting and its pane
	// never receives any output. One idle session keeps the server up.
	if err := exec.Command("tmux", "new-session", "-d", "-s", "berth-keepalive", "sh").Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cannot start the test tmux server:", err)
		os.Exit(1)
	}

	code := m.Run()

	// Safe to kill: this server is the one behind our own socket.
	_ = exec.Command("tmux", "kill-server").Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// PanePID is how berth matches a session to the agent running inside it, so a
// change to the -F field order that drops it must fail loudly.
func TestListReportsPanePID(t *testing.T) {
	requireTmux(t)
	name := testName(t, "panepid")
	created, err := New(NewOptions{Name: name, Kind: KindShell, Command: "sh"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer Kill(created)

	sessions, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, s := range sessions {
		if s.Name != created {
			continue
		}
		if s.PanePID <= 0 {
			t.Errorf("PanePID = %d, want the pane's process id", s.PanePID)
		}
		return
	}
	t.Fatalf("session %q not listed", created)
}
