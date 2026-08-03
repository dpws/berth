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

// codexWindowFields renders a window pair as Codex writes it, filling in the
// nulls for a plan that reports fewer than two.
func codexWindowFields(windows [][3]any) (primary, secondary string) {
	field := func(w [3]any) string {
		return fmt.Sprintf(`{"used_percent":%v,"window_minutes":%v,"resets_at":%v}`,
			w[0], w[1], w[2])
	}
	primary, secondary = "null", "null"
	if len(windows) > 0 {
		primary = field(windows[0])
	}
	if len(windows) > 1 {
		secondary = field(windows[1])
	}
	return primary, secondary
}

// codexLine builds one rollout record carrying a rate limit snapshot.
func codexLine(ts string, windows ...[3]any) string {
	primary, secondary := codexWindowFields(windows)
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

// codexLine with a bucket id, for the case Codex meters a model separately.
func codexBucketLine(ts, id, name string, windows ...[3]any) string {
	primary, secondary := codexWindowFields(windows)
	return fmt.Sprintf(
		`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count",`+
			`"rate_limits":{"limit_id":%q,"limit_name":%s,"primary":%s,"secondary":%s,`+
			`"plan_type":"prolite"}}}`,
		ts, id, nullableString(name), primary, secondary)
}

func nullableString(s string) string {
	if s == "" {
		return "null"
	}
	return fmt.Sprintf("%q", s)
}

// Codex meters some models separately. Keeping only the newest snapshot meant
// a freshly touched bucket at nothing used hid a main quota most of the way
// gone - the meter read empty while the real limit was well spent.
func TestReadCodexDirKeepsEveryBucket(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "r.jsonl"),
		codexBucketLine("2026-08-03T10:00:00Z", "codex", "", [3]any{34, 10080, 1786184122})+"\n"+
			codexBucketLine("2026-08-03T11:00:00Z", "codex_bengalfox", "GPT-5.3-Codex-Spark", [3]any{0, 10080, 1786399909})+"\n")

	got, err := readCodexDir(dir)
	if err != nil {
		t.Fatalf("readCodexDir: %v", err)
	}
	if len(got.Windows) != 2 {
		t.Fatalf("got %d windows, want both buckets: %+v", len(got.Windows), got.Windows)
	}
	// The plain bucket leads: it is the one most people mean.
	if got.Windows[0].Label != "codex" || got.Windows[0].Percent != 34 {
		t.Errorf("first window = %+v, want the main quota", got.Windows[0])
	}
	if got.Windows[1].Label != "spark" || got.Windows[1].Percent != 0 {
		t.Errorf("second window = %+v, want the named bucket", got.Windows[1])
	}
}

// Most plans meter two periods per bucket. Labelling those rows by the bucket
// alone named both of them the same, so a 5-hour figure and a weekly one sat
// under identical headings with nothing to say which was which.
func TestBucketWithTwoWindowsNamesBoth(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "r.jsonl"),
		codexBucketLine("2026-08-03T10:00:00Z", "codex", "",
			[3]any{40, 300, 1786184122}, [3]any{70, 10080, 1786284122})+"\n"+
			codexBucketLine("2026-08-03T11:00:00Z", "codex_bengalfox", "GPT-5.3-Codex-Spark",
				[3]any{5, 300, 1786184122}, [3]any{9, 10080, 1786284122})+"\n")

	got, err := readCodexDir(dir)
	if err != nil {
		t.Fatalf("readCodexDir: %v", err)
	}
	want := []string{"codex 5h", "codex week", "spark 5h", "spark week"}
	if len(got.Windows) != len(want) {
		t.Fatalf("got %d windows, want %d: %+v", len(got.Windows), len(want), got.Windows)
	}
	for i, w := range want {
		if got.Windows[i].Label != w {
			t.Errorf("window %d labelled %q, want %q", i, got.Windows[i].Label, w)
		}
	}
}

// With one bucket there is nothing to tell apart, so the window keeps its own
// name rather than being labelled with the limit's.
func TestOneBucketIsLabelledByItsWindow(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "r.jsonl"),
		codexBucketLine("2026-08-03T10:00:00Z", "codex", "", [3]any{28, 10080, 1786184122})+"\n")

	got, err := readCodexDir(dir)
	if err != nil {
		t.Fatalf("readCodexDir: %v", err)
	}
	if len(got.Windows) != 1 || got.Windows[0].Label != "week" {
		t.Errorf("windows = %+v, want one labelled by its window", got.Windows)
	}
}

// A bucket's newest reading wins, wherever it was found.
func TestBucketsTakeTheirNewestReading(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "old.jsonl"),
		codexBucketLine("2026-08-03T09:00:00Z", "codex", "", [3]any{10, 10080, 1786184122})+"\n")
	writeFile(t, filepath.Join(dir, "new.jsonl"),
		codexBucketLine("2026-08-03T12:00:00Z", "codex", "", [3]any{55, 10080, 1786184122})+"\n")

	got, err := readCodexDir(dir)
	if err != nil {
		t.Fatalf("readCodexDir: %v", err)
	}
	if len(got.Windows) != 1 || got.Windows[0].Percent != 55 {
		t.Errorf("windows = %+v, want the newer reading", got.Windows)
	}
}

func TestCodexLimitLabel(t *testing.T) {
	cases := map[[2]string]string{
		{"codex_bengalfox", "GPT-5.3-Codex-Spark"}: "spark",
		{"codex_bengalfox", ""}:                    "bengalfox",
		{"codex", ""}:                              "codex",
	}
	for in, want := range cases {
		if got := codexLimitLabel(in[0], in[1]); got != want {
			t.Errorf("codexLimitLabel(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

// One bucket is written every few seconds while another goes untouched for
// hours. Taking the newest reading as the block's age let the busy bucket
// vouch for the stale one, and the whole block claimed to be current.
func TestBlockAgeIsTheOldestBucketOnShow(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "r.jsonl"),
		codexBucketLine("2026-08-03T20:00:00Z", "codex", "", [3]any{34, 10080, 1786184122})+"\n"+
			codexBucketLine("2026-08-03T22:17:00Z", "codex_bengalfox", "GPT-5.3-Codex-Spark", [3]any{0, 10080, 1786400230})+"\n")

	got, err := readCodexDir(dir)
	if err != nil {
		t.Fatalf("readCodexDir: %v", err)
	}
	want := "2026-08-03T20:00:00Z"
	if got.Sampled.UTC().Format(time.RFC3339) != want {
		t.Errorf("Sampled = %v, want the oldest reading (%s)",
			got.Sampled.UTC().Format(time.RFC3339), want)
	}
}

// A bucket recorded without a timestamp knows nothing about age. Letting it
// win the oldest-reading comparison zeroed the block's age, and the sidebar
// dropped the "as of" line that says the numbers are not live.
func TestUndatedBucketDoesNotZeroTheBlockAge(t *testing.T) {
	dir := t.TempDir()
	// The undated bucket sorts last, so it is the one that gets the last word.
	line := codexBucketLine("2026-08-03T20:00:00Z", "codex_zebra", "", [3]any{12, 10080, 1786400230})
	writeFile(t, filepath.Join(dir, "r.jsonl"),
		codexBucketLine("2026-08-03T20:00:00Z", "codex", "", [3]any{34, 10080, 1786184122})+"\n"+
			strings.Replace(line, `"timestamp":"2026-08-03T20:00:00Z",`, "", 1)+"\n")

	got, err := readCodexDir(dir)
	if err != nil {
		t.Fatalf("readCodexDir: %v", err)
	}
	if got.Sampled.IsZero() {
		t.Error("Sampled is zero, want the one bucket that was dated")
	}
}
