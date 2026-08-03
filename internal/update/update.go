// Package update checks for newer berth releases and installs them when
// asked.
//
// It never installs on its own. Checking is a GitHub API call that reports a
// version and nothing about you; installing replaces the binary you are
// running, which is not something a program should decide for itself. So the
// check runs quietly in the background and says what it found, and "berth
// update" is what acts on it.
package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// repo is where releases come from.
	repo = "dpws/berth"
	// maxDownload bounds an archive. Releases are around ten megabytes.
	maxDownload = 128 << 20
	// httpTimeout bounds the whole exchange, including the download.
	httpTimeout = 2 * time.Minute
	// checkTimeout bounds the version check, which is one small request and
	// happens while you are trying to use berth.
	checkTimeout = 5 * time.Second
)

// Release is the part of a GitHub release berth reads.
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// Asset is one published file.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// AssetNamed returns the asset with the given name.
func (r Release) AssetNamed(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// ArchiveName is the archive holding berth for this machine.
func ArchiveName(tag string) string {
	return fmt.Sprintf("berth_%s_%s_%s.tar.gz", tag, runtime.GOOS, runtime.GOARCH)
}

// Latest asks GitHub for the newest release.
func Latest(ctx context.Context, baseURL string) (Release, error) {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimSuffix(baseURL, "/"), repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github said %s", resp.Status)
	}

	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return Release{}, err
	}
	if rel.Tag == "" {
		return Release{}, errors.New("no release found")
	}
	return rel, nil
}

// Newer reports whether tag is a later version than current.
//
// A build that is not a plain release - "dev", or a tag with commits on top -
// is never told it is out of date: it is probably ahead, not behind, and being
// nagged to downgrade would be worse than saying nothing.
func Newer(current, tag string) bool {
	cur, ok := parseVersion(current)
	if !ok || isDevBuild(current) {
		return false
	}
	next, ok := parseVersion(tag)
	if !ok {
		return false
	}
	for i := range cur {
		if next[i] != cur[i] {
			return next[i] > cur[i]
		}
	}
	return false
}

// isDevBuild reports whether a version came from anything but a clean tag.
// git describe adds a commit count and hash once there are commits after the
// tag, and "-dirty" when the tree has changes.
func isDevBuild(v string) bool {
	v = strings.TrimPrefix(v, "v")
	return v == "" || v == "dev" || strings.Contains(v, "-")
}

// parseVersion reads vMAJOR.MINOR.PATCH, ignoring anything after.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Download fetches an asset, checking it against the release's checksums file
// before it is trusted. An unsigned binary from the internet is exactly the
// thing worth checking, and the release publishes the sums for it.
func Download(ctx context.Context, rel Release, name string) ([]byte, error) {
	asset, ok := rel.AssetNamed(name)
	if !ok {
		return nil, fmt.Errorf("this release has no %s", name)
	}
	sums, ok := rel.AssetNamed("checksums.txt")
	if !ok {
		return nil, errors.New("this release publishes no checksums")
	}

	sumData, err := fetch(ctx, sums.URL, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("fetching checksums: %w", err)
	}
	want, ok := checksumFor(string(sumData), name)
	if !ok {
		return nil, fmt.Errorf("%s is not listed in the checksums", name)
	}

	data, err := fetch(ctx, asset.URL, maxDownload)
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("%s does not match its published checksum", name)
	}
	return data, nil
}

// checksumFor finds a file's sum in a sha256sum-style listing.
func checksumFor(listing, name string) (string, bool) {
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		// sha256sum writes "*name" for a binary-mode file.
		if strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", filepath.Base(url), resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// ExtractBinary pulls one file out of a berth release archive.
func ExtractBinary(archive []byte, name string) ([]byte, error) {
	zr, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("the archive holds no %s", name)
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Archives are built with "tar -C dir .", so entries arrive as "./berth".
		if filepath.Base(filepath.Clean(hdr.Name)) != name {
			continue
		}
		return io.ReadAll(io.LimitReader(tr, maxDownload))
	}
}

// Replace swaps the file at path for new content.
//
// The new file is written beside the old one and renamed over it, which on
// Unix is atomic and is allowed even for the binary currently running: the
// process keeps the inode it started from, and the next start gets the new
// one. Writing in place would corrupt a running berth.
func Replace(path string, content []byte) error {
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".berth-update-*")
	if err != nil {
		return fmt.Errorf("%s is not writable: %w", dir, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	// Rename orders itself against other renames, not against the write behind
	// it. Without this the directory entry can reach the disk while the bytes
	// have not, and a machine that loses power mid-update comes back with a
	// berth that is the right size and all zeroes.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
