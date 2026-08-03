package term

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/dpws/berth/internal/tmux"
)

// waitFor polls until cond holds or the deadline passes. Terminal output
// arrives asynchronously, so every assertion about the screen needs this. The
// timeouts are generous because the whole suite runs its tmux clients in
// parallel, which on a small machine can push a redraw well past a second.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func screen(p *Pane) string {
	return strings.Join(p.Render(false, nil), "\n")
}

func newTestSession(t *testing.T, suffix string) string {
	t.Helper()
	if err := tmux.Available(); err != nil {
		t.Skip("tmux not available:", err)
	}
	name, err := tmux.New(tmux.NewOptions{
		Name:          fmt.Sprintf("berth-pane-%d-%s", os.Getpid(), suffix),
		Dir:           t.TempDir(),
		Kind:          tmux.KindShell,
		Command:       "sh",
		HideStatusBar: true,
		// Pin the session's own mouse setting so a global "mouse on" in the
		// developer's ~/.tmux.conf cannot make tmux swallow forwarded events
		// and put the pane into copy mode mid-test.
		Options: []string{"mouse off"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = tmux.Kill(name) })
	return name
}

func TestPaneRendersSessionOutput(t *testing.T) {
	name := newTestSession(t, "render")

	pane, err := Attach(name, 80, 24)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer pane.Close()

	pane.SendText("echo marker-one\n")
	if !waitFor(t, 15*time.Second, func() bool {
		return strings.Contains(screen(pane), "marker-one")
	}) {
		t.Fatalf("output never appeared; screen was:\n%s", screen(pane))
	}

	lines := pane.Render(false, nil)
	if len(lines) != 24 {
		t.Errorf("Render returned %d lines, want 24", len(lines))
	}
}

func TestPaneResizeReachesTheShell(t *testing.T) {
	name := newTestSession(t, "resize")

	pane, err := Attach(name, 80, 24)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer pane.Close()

	// Let the client settle before resizing, otherwise tmux may still be
	// drawing at the original size.
	pane.SendText("echo ready\n")
	waitFor(t, 15*time.Second, func() bool { return strings.Contains(screen(pane), "ready") })

	pane.Resize(60, 15)
	if w, h := pane.Size(); w != 60 || h != 15 {
		t.Fatalf("pane size = %dx%d, want 60x15", w, h)
	}

	pane.SendText("tput cols\n")
	if !waitFor(t, 15*time.Second, func() bool {
		return strings.Contains(screen(pane), "60")
	}) {
		t.Fatalf("shell never saw the new width; screen was:\n%s", screen(pane))
	}

	if len(pane.Render(false, nil)) != 15 {
		t.Errorf("Render should follow the new height")
	}
}

func TestPaneCloseLeavesTheSessionRunning(t *testing.T) {
	name := newTestSession(t, "detach")

	pane, err := Attach(name, 80, 24)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	pane.SendText("echo alive\n")
	waitFor(t, 15*time.Second, func() bool { return strings.Contains(screen(pane), "alive") })

	if err := pane.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !pane.Closed() {
		t.Error("pane should report itself closed")
	}
	if !tmux.Exists(name) {
		t.Error("closing the pane must not kill the tmux session")
	}

	// Closing twice is a no-op, and sends after close are ignored rather than
	// panicking on a closed pty.
	if err := pane.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	pane.SendText("echo ignored\n")
}

func TestPaneNoticesAKilledSession(t *testing.T) {
	name := newTestSession(t, "killed")

	pane, err := Attach(name, 80, 24)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer pane.Close()

	pane.SendText("echo up\n")
	waitFor(t, 15*time.Second, func() bool { return strings.Contains(screen(pane), "up") })

	if err := tmux.Kill(name); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if !waitFor(t, 15*time.Second, pane.Exited) {
		t.Error("pane should report the tmux client exiting")
	}
}

// A session that never asked for focus or mouse reporting must not receive
// any: stray escape sequences would land in the shell as typed garbage.
func TestFocusAndMouseAreSilentUntilRequested(t *testing.T) {
	name := newTestSession(t, "quiet")

	pane, err := Attach(name, 80, 24)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	defer pane.Close()

	pane.SendText("echo first\n")
	waitFor(t, 15*time.Second, func() bool { return strings.Contains(screen(pane), "first") })

	pane.SetFocus(true)
	pane.SetFocus(false)
	pane.SendMouse(uv.MouseClickEvent{X: 4, Y: 2, Button: uv.MouseLeft})
	pane.SendMouse(uv.MouseWheelEvent{X: 4, Y: 2, Button: uv.MouseWheelUp})

	pane.SendText("echo second\n")
	if !waitFor(t, 15*time.Second, func() bool { return strings.Contains(screen(pane), "second") }) {
		t.Fatalf("second command never ran; screen was:\n%s", screen(pane))
	}

	got := screen(pane)
	for _, junk := range []string{"[I", "[O", "[<0;", "[M"} {
		if strings.Contains(got, junk) {
			t.Errorf("stray %q reached the session; screen was:\n%s", junk, got)
		}
	}
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

// tmux refuses to attach a session from inside one, so a berth run in a tmux
// pane would start a client that died at once - and start another, and another.
func TestAttachEnvironmentHasNoTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	t.Setenv("TMUX_PANE", "%7")
	t.Setenv("BERTH_ENV_PROBE", "kept")

	var kept bool
	for _, kv := range envWithoutTmux() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			t.Errorf("%q survived into the attach environment", kv)
		}
		if kv == "BERTH_ENV_PROBE=kept" {
			kept = true
		}
	}
	if !kept {
		t.Error("the rest of the environment was dropped along with TMUX")
	}
}
