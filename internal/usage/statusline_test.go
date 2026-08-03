package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dpws/berth/internal/tmux"
)

// The payload shape is Claude Code's, documented with rate_limits percentages
// from 0 to 100 and resets_at in Unix epoch seconds.
const samplePayload = `{
  "model": {"display_name": "Opus 5"},
  "context_window": {"used_percentage": 8},
  "rate_limits": {
    "five_hour": {"used_percentage": 23.5, "resets_at": 1738425600},
    "seven_day": {"used_percentage": 41.2, "resets_at": 1738857600}
  }
}`

// useCacheDir points the limits cache at a temp directory.
func useCacheDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	if CachePath() == "" {
		t.Fatal("no cache path")
	}
}

func TestParseStatusLine(t *testing.T) {
	s, err := ParseStatusLine([]byte(samplePayload))
	if err != nil {
		t.Fatalf("ParseStatusLine: %v", err)
	}
	if s.Model.DisplayName != "Opus 5" {
		t.Errorf("model = %q", s.Model.DisplayName)
	}
	if s.RateLimits == nil || s.RateLimits.FiveHour == nil {
		t.Fatal("rate limits missing")
	}
	if got := s.RateLimits.FiveHour.UsedPercentage; got != 23.5 {
		t.Errorf("five_hour = %v, want 23.5", got)
	}
	if got := s.RateLimits.SevenDay.ResetsAt; got != 1738857600 {
		t.Errorf("seven_day resets_at = %d", got)
	}
}

func TestSaveAndReadStatusLine(t *testing.T) {
	useCacheDir(t)
	s, err := ParseStatusLine([]byte(samplePayload))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := SaveStatusLine(s, now); err != nil {
		t.Fatalf("SaveStatusLine: %v", err)
	}

	got, err := readCachedClaude()
	if err != nil {
		t.Fatalf("nothing read back: %v", err)
	}
	if len(got.Windows) != 2 {
		t.Fatalf("got %d windows, want 2", len(got.Windows))
	}
	if got.Windows[0].Label != "5h" || got.Windows[0].Percent != 23.5 {
		t.Errorf("5h window = %+v", got.Windows[0])
	}
	if got.Windows[1].Label != "week" || got.Windows[1].Percent != 41.2 {
		t.Errorf("week window = %+v", got.Windows[1])
	}
	if got.Windows[1].ResetsAt.Unix() != 1738857600 {
		t.Errorf("resets at %v, want the epoch seconds decoded", got.Windows[1].ResetsAt)
	}
	if got.Sampled.Unix() != now.Unix() {
		t.Errorf("Sampled = %v, want when it was recorded", got.Sampled)
	}
}

// rate_limits only appears for subscribers, and only after the session's first
// response. Writing those empty early payloads would erase good numbers.
func TestSaveStatusLineKeepsExistingNumbers(t *testing.T) {
	useCacheDir(t)
	good, _ := ParseStatusLine([]byte(samplePayload))
	if err := SaveStatusLine(good, time.Now()); err != nil {
		t.Fatal(err)
	}

	for _, payload := range []string{
		`{"model":{"display_name":"Opus 5"}}`,
		`{"model":{"display_name":"Opus 5"},"rate_limits":{}}`,
	} {
		empty, err := ParseStatusLine([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if err := SaveStatusLine(empty, time.Now()); err != nil {
			t.Fatalf("SaveStatusLine: %v", err)
		}
		if got, err := readCachedClaude(); err != nil || len(got.Windows) != 2 {
			t.Fatalf("payload %q wiped the cache", payload)
		}
	}
}

func TestSaveStatusLineWritesAtomically(t *testing.T) {
	useCacheDir(t)
	s, _ := ParseStatusLine([]byte(samplePayload))
	if err := SaveStatusLine(s, time.Now()); err != nil {
		t.Fatal(err)
	}

	// Claude Code kills the command mid-run when a new update lands, so no
	// partial files may be left lying around next to the cache.
	entries, err := os.ReadDir(filepath.Dir(CachePath()))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("left a temporary file behind: %s", e.Name())
		}
	}
}

func TestReadCachedClaudeIgnoresRubbish(t *testing.T) {
	useCacheDir(t)
	if _, err := readCachedClaude(); err == nil {
		t.Error("read something from an empty cache directory")
	}

	path := CachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"not json", `{}`, `{"sampled_unix":1}`} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readCachedClaude(); err == nil {
			t.Errorf("accepted %q as usable limits", body)
		}
	}
}

// Claude usage exists only once the status line hook has run; until then the
// block says what to do about it rather than reporting a number berth guessed.
func TestTrackerReportsOnlyWhatTheHookProvided(t *testing.T) {
	useCacheDir(t)

	before := NewTracker().Refresh()[tmux.KindClaude]
	if before.Err == nil {
		t.Error("without the cache, Claude usage should report why it is empty")
	}
	if !before.Empty() {
		t.Errorf("without the cache, expected no windows, got %+v", before.Windows)
	}

	s, _ := ParseStatusLine([]byte(samplePayload))
	if err := SaveStatusLine(s, time.Now()); err != nil {
		t.Fatal(err)
	}

	after := NewTracker().Refresh()[tmux.KindClaude]
	if after.Err != nil {
		t.Errorf("with the cache, usage should be available: %v", after.Err)
	}
	if len(after.Windows) == 0 || after.Windows[0].Percent != 23.5 {
		t.Fatalf("expected the official windows, got %+v", after.Windows)
	}
}

func TestStatusLineRender(t *testing.T) {
	s, _ := ParseStatusLine([]byte(samplePayload))
	got := s.Render()
	for _, want := range []string{"Opus 5", "ctx 8%", "5h 24%", "week 41%"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() = %q, want it to mention %q", got, want)
		}
	}

	// A payload with nothing useful renders nothing rather than stray marks.
	empty, _ := ParseStatusLine([]byte(`{}`))
	if got := empty.Render(); got != "" {
		t.Errorf("Render() = %q, want empty", got)
	}
}

func TestReadStatusLineRejectsGarbage(t *testing.T) {
	if _, err := ReadStatusLine(strings.NewReader("not json")); err == nil {
		t.Error("want an error for a payload that is not JSON")
	}
}
