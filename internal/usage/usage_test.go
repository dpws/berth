package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowLabel(t *testing.T) {
	cases := map[int]string{
		0:     "?",
		-5:    "?",
		45:    "45m",
		60:    "1h",
		300:   "5h",
		1440:  "1d",
		10080: "week",
		20160: "14d",
	}
	for minutes, want := range cases {
		if got := windowLabel(minutes); got != want {
			t.Errorf("windowLabel(%d) = %q, want %q", minutes, want, got)
		}
	}
}

// codexLine builds one rollout record carrying a rate limit snapshot.
func codexLine(ts string, windows ...[3]any) string {
	field := func(w [3]any) string {
		return fmt.Sprintf(`{"used_percent":%v,"window_minutes":%v,"resets_at":%v}`,
			w[0], w[1], w[2])
	}
	primary, secondary := "null", "null"
	if len(windows) > 0 {
		primary = field(windows[0])
	}
	if len(windows) > 1 {
		secondary = field(windows[1])
	}
	return fmt.Sprintf(
		`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count",`+
			`"rate_limits":{"primary":%s,"secondary":%s,"plan_type":"pro"}}}`,
		ts, primary, secondary)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadCodexDirTakesLastSnapshot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "2026", "08", "03", "rollout-a.jsonl"),
		`{"type":"event_msg","payload":{"type":"other"}}`+"\n"+
			codexLine("2026-08-03T10:00:00Z", [3]any{10.0, 300, 1786000000})+"\n"+
			codexLine("2026-08-03T11:00:00Z", [3]any{42.5, 300, 1786000000})+"\n")

	got, err := readCodexDir(dir)
	if err != nil {
		t.Fatalf("readCodexDir: %v", err)
	}
	if got.Plan != "pro" {
		t.Errorf("Plan = %q, want %q", got.Plan, "pro")
	}
	if len(got.Windows) != 1 {
		t.Fatalf("got %d windows, want 1", len(got.Windows))
	}
	// The last snapshot in the file is the current one.
	if got.Windows[0].Percent != 42.5 {
		t.Errorf("Percent = %v, want 42.5", got.Windows[0].Percent)
	}
	if got.Windows[0].Label != "5h" {
		t.Errorf("Label = %q, want %q", got.Windows[0].Label, "5h")
	}
	if got.Sampled.IsZero() {
		t.Error("Sampled is zero, want the record's timestamp")
	}
}

func TestReadCodexDirOrdersWindowsByLength(t *testing.T) {
	dir := t.TempDir()
	// Codex reports the weekly window as primary on some plans, so the shorter
	// window can arrive second.
	writeFile(t, filepath.Join(dir, "rollout.jsonl"),
		codexLine("2026-08-03T11:00:00Z",
			[3]any{60.0, 10080, 1786000000},
			[3]any{20.0, 300, 1785000000})+"\n")

	got, err := readCodexDir(dir)
	if err != nil {
		t.Fatalf("readCodexDir: %v", err)
	}
	if len(got.Windows) != 2 {
		t.Fatalf("got %d windows, want 2", len(got.Windows))
	}
	if got.Windows[0].Label != "5h" || got.Windows[1].Label != "week" {
		t.Errorf("order = %q, %q; want 5h, week",
			got.Windows[0].Label, got.Windows[1].Label)
	}
	if got.Windows[0].ResetsAt.IsZero() {
		t.Error("ResetsAt is zero, want the reset time")
	}
}

func TestReadCodexDirSkipsFilesWithoutSnapshots(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.jsonl")
	writeFile(t, old, codexLine("2026-08-01T10:00:00Z", [3]any{7.0, 300, 1786000000})+"\n")
	// A newer session that has not made a request yet records no limits, so the
	// older file is still the best source.
	newer := filepath.Join(dir, "new.jsonl")
	writeFile(t, newer, `{"type":"event_msg","payload":{"type":"other"}}`+"\n")

	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(newer, later, later); err != nil {
		t.Fatal(err)
	}

	got, err := readCodexDir(dir)
	if err != nil {
		t.Fatalf("readCodexDir: %v", err)
	}
	if len(got.Windows) != 1 || got.Windows[0].Percent != 7 {
		t.Errorf("got %+v, want the snapshot from the older file", got.Windows)
	}
}

// Rollouts of an active session grow without bound, so the newest snapshot is
// found by reading the tail. The whole file is only read when that misses.
func TestReadCodexDirReadsLongRollouts(t *testing.T) {
	padding := strings.Repeat(
		`{"type":"event_msg","payload":{"type":"other"}}`+"\n",
		1+codexTailBytes/46)

	t.Run("snapshot in the tail", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "r.jsonl"),
			codexLine("2026-08-03T10:00:00Z", [3]any{1.0, 300, 1786000000})+"\n"+
				padding+
				codexLine("2026-08-03T12:00:00Z", [3]any{55.0, 300, 1786000000})+"\n")

		got, err := readCodexDir(dir)
		if err != nil {
			t.Fatalf("readCodexDir: %v", err)
		}
		if got.Windows[0].Percent != 55 {
			t.Errorf("Percent = %v, want 55", got.Windows[0].Percent)
		}
	})

	t.Run("snapshot before the tail", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "r.jsonl"),
			codexLine("2026-08-03T10:00:00Z", [3]any{33.0, 300, 1786000000})+"\n"+padding)

		got, err := readCodexDir(dir)
		if err != nil {
			t.Fatalf("readCodexDir: %v", err)
		}
		if got.Windows[0].Percent != 33 {
			t.Errorf("Percent = %v, want 33 (fallback to a full read)",
				got.Windows[0].Percent)
		}
	})
}

func TestReadCodexDirEmpty(t *testing.T) {
	if _, err := readCodexDir(t.TempDir()); err == nil {
		t.Error("want an error for a directory with no sessions")
	}
}
