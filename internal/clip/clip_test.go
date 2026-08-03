package clip

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeImage(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("not really an image"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	mod := time.Now().Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

func TestDropDirPicksTheNewestImage(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "old.png", time.Hour)
	newest := writeImage(t, dir, "new.jpg", time.Minute)
	writeImage(t, dir, "notes.txt", 0) // not an image
	writeImage(t, dir, "ancient.gif", 24*time.Hour)

	got, err := fromDropDir(dir, t.TempDir())
	if err != nil {
		t.Fatalf("fromDropDir: %v", err)
	}
	if got != newest {
		t.Errorf("picked %q, want %q", got, newest)
	}
}

func TestDropDirIgnoresNonImages(t *testing.T) {
	dir := t.TempDir()
	writeImage(t, dir, "notes.txt", 0)
	writeImage(t, dir, "archive.tar.gz", 0)

	if _, err := fromDropDir(dir, t.TempDir()); err == nil {
		t.Fatal("a directory with no images should be an error")
	} else if !strings.Contains(err.Error(), dir) {
		t.Errorf("error should name the directory, got %q", err)
	}
}

// A path with spaces would be split apart when typed into a prompt, so it is
// copied to a clean name instead.
func TestDropDirRewritesPathsWithSpaces(t *testing.T) {
	dir, cache := t.TempDir(), t.TempDir()
	writeImage(t, dir, "screen shot.png", 0)

	got, err := fromDropDir(dir, cache)
	if err != nil {
		t.Fatalf("fromDropDir: %v", err)
	}
	if strings.ContainsAny(got, " \t") {
		t.Errorf("returned path still contains whitespace: %q", got)
	}
	if filepath.Dir(got) != cache {
		t.Errorf("copy should live in the cache dir, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "screen shot.png")); err != nil {
		t.Error("the original file should be left alone")
	}
}

func TestDropDirIsCreatedWhenMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-yet")
	if _, err := fromDropDir(dir, t.TempDir()); err == nil {
		t.Fatal("expected an error for an empty directory")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("drop directory should have been created: %v", err)
	}
}

func TestPickImageTypePrefersPng(t *testing.T) {
	cases := []struct {
		targets string
		want    string
	}{
		{"TARGETS\nimage/jpeg\nimage/png\ntext/plain", "image/png"},
		{"text/plain\nimage/webp\nimage/jpeg", "image/jpeg"},
		{"image/tiff", "image/tiff"},
		{"TARGETS\ntext/plain\nUTF8_STRING", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := pickImageType(c.targets); got != c.want {
			t.Errorf("pickImageType(%q) = %q, want %q", c.targets, got, c.want)
		}
	}
}

func TestFetchExplainsEverythingItTried(t *testing.T) {
	dir := t.TempDir()
	_, err := Fetch(Options{DropDir: dir, CacheDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error when nothing has an image")
	}
	if !strings.Contains(err.Error(), "clipboard") {
		t.Errorf("error should mention the clipboard attempt, got %q", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error should name the drop folder, got %q", err)
	}
}

func TestCachePruningKeepsTheNewest(t *testing.T) {
	cache := t.TempDir()
	for i := range 5 {
		writeImage(t, cache, "paste-"+string(rune('a'+i))+".png", time.Duration(i)*time.Hour)
	}
	pruneCache(cache, 2)

	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("pruned to %d files, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Name() != "paste-a.png" && e.Name() != "paste-b.png" {
			t.Errorf("pruning kept the wrong file: %s", e.Name())
		}
	}
}

func TestDiagnoseExplainsAMissingDisplay(t *testing.T) {
	cases := []struct {
		stderr string
		want   string
	}{
		{"Error: Can't open display: (null)", "xclip has no display (try ssh -X)"},
		{"failed to connect to a Wayland server", "xclip has no Wayland display"},
		{"", ""},
		{"something else broke", "xclip: something else broke"},
	}
	for _, c := range cases {
		err := &exec.ExitError{ProcessState: nil, Stderr: []byte(c.stderr)}
		if got := diagnose("xclip", err); got != c.want {
			t.Errorf("diagnose(%q) = %q, want %q", c.stderr, got, c.want)
		}
	}
}

func agentServing(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

var fakePNG = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("p", 64))

func TestAgentImageIsCachedAndReturned(t *testing.T) {
	cache := t.TempDir()
	url := agentServing(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image" {
			t.Errorf("agent was asked for %q, want /image", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(fakePNG)
	})

	got, err := fromAgent(Options{AgentURL: url, CacheDir: cache})
	if err != nil {
		t.Fatalf("fromAgent: %v", err)
	}
	if filepath.Ext(got) != ".png" {
		t.Errorf("cached file should be a .png, got %q", got)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("reading the cached image: %v", err)
	}
	if string(data) != string(fakePNG) {
		t.Error("cached bytes do not match what the agent served")
	}
}

func TestAgentSendsTheToken(t *testing.T) {
	var seen string
	url := agentServing(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Berth-Token")
		w.Write(fakePNG)
	})

	if _, err := fromAgent(Options{AgentURL: url, CacheDir: t.TempDir(), AgentToken: "sekrit"}); err != nil {
		t.Fatalf("fromAgent: %v", err)
	}
	if seen != "sekrit" {
		t.Errorf("agent received token %q, want %q", seen, "sekrit")
	}
}

func TestAgentEmptyClipboard(t *testing.T) {
	url := agentServing(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	_, err := fromAgent(Options{AgentURL: url, CacheDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "no image") {
		t.Fatalf("204 should report an empty clipboard, got %v", err)
	}
}

// A forwarded port can have anything behind it. Bytes that are not an image
// must not end up in a session as a "pasted" file.
func TestAgentRejectsNonImageBytes(t *testing.T) {
	cache := t.TempDir()
	url := agentServing(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png") // lying header
		w.Write([]byte("<html>wrong service on this port</html>"))
	})

	if _, err := fromAgent(Options{AgentURL: url, CacheDir: cache}); err == nil {
		t.Fatal("non-image bytes should be rejected even with an image content type")
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 0 {
		t.Errorf("nothing should have been cached, found %d files", len(entries))
	}
}

func TestAgentUnreachableExplainsTheTunnel(t *testing.T) {
	// Port 1 on loopback: nothing is listening, and connecting fails fast.
	_, err := fromAgent(Options{AgentURL: "http://127.0.0.1:1", CacheDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error when the agent is not running")
	}
	if !strings.Contains(err.Error(), "ssh -R") {
		t.Errorf("error should hint at the tunnel, got %q", err)
	}
}

func TestAgentErrorStatusIsReported(t *testing.T) {
	url := agentServing(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad or missing token", http.StatusForbidden)
	})

	_, err := fromAgent(Options{AgentURL: url, CacheDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("a 403 should surface the agent's message, got %v", err)
	}
}

func TestAgentIsSkippedWhenNotConfigured(t *testing.T) {
	if _, err := fromAgent(Options{CacheDir: t.TempDir()}); err == nil {
		t.Fatal("an empty agent url should be an error, not a nil result")
	}
}

func TestImageExtForBytes(t *testing.T) {
	cases := []struct {
		data []byte
		ext  string
		ok   bool
	}{
		{[]byte("\x89PNG\r\n\x1a\nrest"), ".png", true},
		{[]byte("\xff\xd8\xffrest"), ".jpg", true},
		{[]byte("GIF89arest"), ".gif", true},
		{[]byte("BMrest"), ".bmp", true},
		{[]byte("RIFF____WEBPrest"), ".webp", true},
		{[]byte("not an image"), "", false},
		{nil, "", false},
	}
	for _, c := range cases {
		ext, ok := imageExtForBytes(c.data)
		if ok != c.ok || ext != c.ext {
			t.Errorf("imageExtForBytes(%q) = (%q, %v), want (%q, %v)",
				c.data, ext, ok, c.ext, c.ok)
		}
	}
}

// The agent sits between the local clipboard and the drop folder, and a
// failure at any stage must fall through rather than stop the search.
func TestFetchFallsThroughAgentToDropDir(t *testing.T) {
	drop, cache := t.TempDir(), t.TempDir()
	want := filepath.Join(drop, "fallback.png")
	if err := os.WriteFile(want, fakePNG, 0o644); err != nil {
		t.Fatal(err)
	}
	url := agentServing(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	got, err := Fetch(Options{DropDir: drop, CacheDir: cache, AgentURL: url})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Path != want {
		t.Errorf("Fetch returned %q, want %q", got.Path, want)
	}
	if got.Source != SourceDropDir {
		t.Errorf("source = %q, want %q", got.Source, SourceDropDir)
	}
}

func TestFetchPrefersTheAgentOverTheDropDir(t *testing.T) {
	drop, cache := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(drop, "stale.png"), fakePNG, 0o644); err != nil {
		t.Fatal(err)
	}
	url := agentServing(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(fakePNG)
	})

	got, err := Fetch(Options{DropDir: drop, CacheDir: cache, AgentURL: url})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Source != SourceAgent {
		t.Errorf("source = %q, want %q - a live clipboard should win over an old file",
			got.Source, SourceAgent)
	}
}
