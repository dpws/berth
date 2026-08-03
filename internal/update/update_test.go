package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := map[string]struct {
		want [3]int
		ok   bool
	}{
		"v1.2.3":        {[3]int{1, 2, 3}, true},
		"1.2.3":         {[3]int{1, 2, 3}, true},
		"v0.3.0":        {[3]int{0, 3, 0}, true},
		"v1.2.3-7-gabc": {[3]int{1, 2, 3}, true},
		"v1.2":          {[3]int{}, false},
		"dev":           {[3]int{}, false},
		"":              {[3]int{}, false},
		"v1.2.x":        {[3]int{}, false},
	}
	for in, want := range cases {
		got, ok := parseVersion(in)
		if ok != want.ok || (ok && got != want.want) {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", in, got, ok, want.want, want.ok)
		}
	}
}

// A build from source is often ahead of the newest tag, not behind it. Telling
// someone to "update" to an older version would be worse than saying nothing.
func TestNewerIgnoresBuildsFromSource(t *testing.T) {
	for _, current := range []string{"dev", "v0.3.0-7-gabc1234", "v0.3.0-7-gabc-dirty", ""} {
		if Newer(current, "v9.9.9") {
			t.Errorf("a %q build was told v9.9.9 is newer", current)
		}
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		current, tag string
		want         bool
	}{
		{"v0.3.0", "v0.4.0", true},
		{"v0.3.0", "v0.3.1", true},
		{"v0.3.0", "v1.0.0", true},
		{"v0.3.0", "v0.3.0", false},
		{"v0.4.0", "v0.3.0", false},
		{"v1.0.0", "v0.9.9", false},
		{"v0.3.0", "rubbish", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.tag); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.tag, got, c.want)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	listing := "abc123  berth_v1_linux_amd64.tar.gz\ndef456 *berth_v1_darwin_arm64.tar.gz\n\nrubbish\n"
	if got, ok := checksumFor(listing, "berth_v1_linux_amd64.tar.gz"); !ok || got != "abc123" {
		t.Errorf("got %q, %v", got, ok)
	}
	// sha256sum marks binary mode with a leading star.
	if got, ok := checksumFor(listing, "berth_v1_darwin_arm64.tar.gz"); !ok || got != "def456" {
		t.Errorf("binary-mode entry: got %q, %v", got, ok)
	}
	if _, ok := checksumFor(listing, "absent.tar.gz"); ok {
		t.Error("found a checksum for a file that is not listed")
	}
}

// buildArchive makes a release archive shaped like the real ones, which are
// built with "tar -C dir ." and so carry "./berth" rather than "berth".
func buildArchive(t *testing.T, contents map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range contents {
		if err := tw.WriteHeader(&tar.Header{
			Name: "./" + name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func TestExtractBinary(t *testing.T) {
	archive := buildArchive(t, map[string]string{
		"berth": "the binary", "berth-clipd": "the agent", "README.md": "docs",
	})

	got, err := ExtractBinary(archive, "berth")
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	if string(got) != "the binary" {
		t.Errorf("got %q", got)
	}
	// "berth" must not match "berth-clipd".
	got, err = ExtractBinary(archive, "berth-clipd")
	if err != nil || string(got) != "the agent" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := ExtractBinary(archive, "absent"); err == nil {
		t.Error("want an error for a file the archive does not hold")
	}
}

// release serves a fake GitHub release, so the download path is exercised
// without reaching the network.
func release(t *testing.T, archive []byte, corruptSum bool) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(archive)
	hexSum := hex.EncodeToString(sum[:])
	if corruptSum {
		hexSum = strings.Repeat("0", 64)
	}

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	name := ArchiveName("v9.9.9")
	mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) { w.Write(archive) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hexSum, name)
	})
	mux.HandleFunc("/repos/dpws/berth/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{
			Tag: "v9.9.9",
			Assets: []Asset{
				{Name: name, URL: srv.URL + "/archive"},
				{Name: "checksums.txt", URL: srv.URL + "/sums"},
			},
		})
	})
	return srv
}

func TestLatestAndDownload(t *testing.T) {
	archive := buildArchive(t, map[string]string{"berth": "new binary"})
	srv := release(t, archive, false)

	rel, err := Latest(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.Tag != "v9.9.9" {
		t.Fatalf("tag = %q", rel.Tag)
	}

	got, err := Download(context.Background(), rel, ArchiveName(rel.Tag))
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !bytes.Equal(got, archive) {
		t.Error("the downloaded archive does not match what was served")
	}
}

// An archive that does not match its published checksum must be refused. This
// is the whole reason the checksums are published.
func TestDownloadRefusesAWrongChecksum(t *testing.T) {
	archive := buildArchive(t, map[string]string{"berth": "new binary"})
	srv := release(t, archive, true)

	rel, err := Latest(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Download(context.Background(), rel, ArchiveName(rel.Tag))
	if err == nil {
		t.Fatal("an archive that failed its checksum was accepted")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error = %v, want it to name the checksum", err)
	}
}

func TestDownloadNeedsChecksums(t *testing.T) {
	rel := Release{Tag: "v1.0.0", Assets: []Asset{{Name: "berth.tar.gz", URL: "http://example"}}}
	if _, err := Download(context.Background(), rel, "berth.tar.gz"); err == nil {
		t.Error("a release with no checksums was trusted")
	}
}

// Replacing the running binary has to leave a complete file with the same
// permissions, and must go through a rename rather than a write in place.
func TestReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "berth")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := Replace(path, []byte("new and longer")); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new and longer" {
		t.Errorf("content = %q", got)
	}
	after, _ := os.Stat(path)
	if after.Mode().Perm() != before.Mode().Perm() {
		t.Errorf("permissions changed from %v to %v", before.Mode().Perm(), after.Mode().Perm())
	}

	// Nothing half-written left behind if it is interrupted next time.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".berth-update-") {
			t.Errorf("left a temporary file: %s", e.Name())
		}
	}
}

func TestReplaceReportsAnUnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which can write anywhere")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "berth")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if err := Replace(path, []byte("new")); err == nil {
		t.Error("want an error when the directory cannot be written")
	}
}

// The cache is what keeps starting berth twenty times in an afternoon from
// being twenty requests.
func TestAvailableUsesTheCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))

	archive := buildArchive(t, map[string]string{"berth": "x"})
	var asked int
	srv := release(t, archive, false)
	// Count the calls that reach the release endpoint.
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		http.Redirect(w, r, srv.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(base.Close)

	if got := Available(context.Background(), "v0.1.0", base.URL); got != "v9.9.9" {
		t.Fatalf("first check = %q, want the newer tag", got)
	}
	if asked != 1 {
		t.Fatalf("first check made %d requests", asked)
	}

	// The second answer comes from the cache.
	if got := Available(context.Background(), "v0.1.0", base.URL); got != "v9.9.9" {
		t.Errorf("second check = %q", got)
	}
	if asked != 1 {
		t.Errorf("the cache was not used: %d requests", asked)
	}

	// A build already newer than the release is told nothing.
	if got := Available(context.Background(), "v99.0.0", base.URL); got != "" {
		t.Errorf("a newer build was told about %q", got)
	}
}

// Being unable to reach GitHub should cost a notice, not an error: berth is a
// session manager, not an updater.
func TestAvailableIsQuietWhenTheCheckFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	if got := Available(context.Background(), "v0.1.0", srv.URL); got != "" {
		t.Errorf("a failed check returned %q", got)
	}
}

func TestAvailableSaysNothingToBuildsFromSource(t *testing.T) {
	if got := Available(context.Background(), "dev", "http://127.0.0.1:1"); got != "" {
		t.Errorf("a dev build was told about %q", got)
	}
}
