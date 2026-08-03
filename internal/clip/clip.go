// Package clip finds an image to hand to a session. It looks in the system
// clipboard first and falls back to a drop directory, which is what makes the
// feature usable over SSH: a headless box has no clipboard, but a folder you
// can scp into works everywhere.
package clip

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Source says where an image came from, for the status line.
type Source string

const (
	SourceClipboard Source = "clipboard"
	SourceAgent     Source = "remote clipboard"
	SourceDropDir   Source = "drop folder"
)

// Result is an image on the local filesystem, ready to be referenced by path.
type Result struct {
	Path   string
	Source Source
}

// Options configures where images are looked for and cached.
type Options struct {
	// DropDir is scanned for the most recently modified image file.
	DropDir string
	// CacheDir is where clipboard images are written.
	CacheDir string
	// AgentURL is a berth-clipd serving the clipboard of the machine you
	// are sitting at, normally reached through an SSH reverse tunnel.
	AgentURL string
	// AgentToken is sent to the agent when it requires one.
	AgentToken string
}

// imageExts are the extensions recognised in the drop directory.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".bmp": true, ".svg": true,
}

// Fetch returns an image from the clipboard, or failing that the newest image
// in the drop directory. The error explains everything that was tried, since
// "nothing happened" is the worst possible feedback for a paste key.
func Fetch(o Options) (Result, error) {
	var reasons []string

	path, err := fromClipboard(o.CacheDir)
	if err == nil {
		return Result{Path: path, Source: SourceClipboard}, nil
	}
	reasons = append(reasons, "clipboard: "+err.Error())

	path, err = fromAgent(o)
	if err == nil {
		return Result{Path: path, Source: SourceAgent}, nil
	}
	reasons = append(reasons, err.Error())

	path, err = fromDropDir(o.DropDir, o.CacheDir)
	if err == nil {
		return Result{Path: path, Source: SourceDropDir}, nil
	}
	reasons = append(reasons, err.Error())

	return Result{}, errors.New(strings.Join(reasons, "; "))
}

// clipboardTool is one way of reading image data out of a system clipboard.
type clipboardTool struct {
	name string
	// targets lists the MIME types currently on the clipboard.
	targets func() *exec.Cmd
	// read fetches one MIME type as raw bytes.
	read func(mime string) *exec.Cmd
}

// tools are tried in order. xsel is deliberately absent: it has no concept of
// MIME targets and can only return text.
var tools = []clipboardTool{
	{
		name:    "wl-paste",
		targets: func() *exec.Cmd { return exec.Command("wl-paste", "--list-types") },
		read: func(mime string) *exec.Cmd {
			return exec.Command("wl-paste", "--no-newline", "--type", mime)
		},
	},
	{
		name: "xclip",
		targets: func() *exec.Cmd {
			return exec.Command("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
		},
		read: func(mime string) *exec.Cmd {
			return exec.Command("xclip", "-selection", "clipboard", "-t", mime, "-o")
		},
	},
}

func fromClipboard(cacheDir string) (string, error) {
	var missing, notes []string

	for _, tool := range tools {
		if _, err := exec.LookPath(tool.name); err != nil {
			missing = append(missing, tool.name)
			continue
		}

		out, err := tool.targets().Output()
		if err != nil {
			// Distinguish "there is no clipboard to read" from "the clipboard
			// is empty" - on a headless or plain-SSH box it is always the
			// former, and saying so saves a lot of guessing.
			if note := diagnose(tool.name, err); note != "" {
				notes = append(notes, note)
			}
			continue
		}
		mime := pickImageType(string(out))
		if mime == "" {
			continue
		}

		data, err := tool.read(mime).Output()
		if err != nil {
			return "", fmt.Errorf("%s could not read %s: %w", tool.name, mime, err)
		}
		if len(data) == 0 {
			continue
		}
		return writeCache(cacheDir, data, extForMime(mime))
	}

	if len(missing) == len(tools) {
		return "", fmt.Errorf("no image tool installed (need %s)", strings.Join(missing, " or "))
	}
	if len(notes) > 0 {
		return "", errors.New(strings.Join(notes, ", "))
	}
	return "", errors.New("no image on the clipboard")
}

// diagnose turns a clipboard tool's failure into something actionable.
// agentReachError explains why the agent could not be reached, which over an
// ssh -R tunnel is two quite different problems wearing the same coat.
//
// Refused means nothing is listening on this end: no tunnel. An empty reply
// means the tunnel accepted the connection and found nothing on the far end:
// the agent is not running on the machine you are sitting at. Telling someone
// to check the tunnel when the tunnel is fine sends them the wrong way.
func agentReachError(url string, err error) error {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Errorf("nothing is listening at %s - is the ssh -R tunnel up?", url)
	case errors.Is(err, io.EOF):
		return fmt.Errorf("the tunnel to %s is open but berth-clipd is not running "+
			"on the machine at the other end", url)
	case os.IsTimeout(err):
		return fmt.Errorf("the clipboard agent at %s did not answer in time", url)
	default:
		return fmt.Errorf("clipboard agent unreachable at %s: %w", url, err)
	}
}

func diagnose(tool string, err error) string {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return tool + ": " + err.Error()
	}
	stderr := strings.ToLower(string(exit.Stderr))
	switch {
	case strings.Contains(stderr, "display"):
		return tool + " has no display (try ssh -X)"
	case strings.Contains(stderr, "wayland"):
		return tool + " has no Wayland display"
	case stderr == "":
		return "" // an empty selection is not worth reporting
	default:
		return tool + ": " + strings.TrimSpace(string(exit.Stderr))
	}
}

// pickImageType chooses an image MIME type from a newline-separated target
// list, preferring PNG because it is lossless and universally readable.
func pickImageType(list string) string {
	var found []string
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "image/") {
			found = append(found, line)
		}
	}
	for _, want := range []string{"image/png", "image/jpeg", "image/webp", "image/gif"} {
		for _, got := range found {
			if got == want {
				return got
			}
		}
	}
	if len(found) > 0 {
		return found[0]
	}
	return ""
}

func extForMime(mime string) string {
	switch mime {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".png"
	}
}

// fromDropDir returns the most recently modified image in dir. The directory
// is created when missing so the error message names somewhere that exists.
func fromDropDir(dir, cacheDir string) (string, error) {
	if dir == "" {
		return "", errors.New("no drop folder configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("drop folder %s: %w", dir, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("drop folder %s: %w", dir, err)
	}

	type candidate struct {
		path string
		mod  time.Time
	}
	var found []candidate
	for _, e := range entries {
		if e.IsDir() || !imageExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		found = append(found, candidate{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	if len(found) == 0 {
		return "", fmt.Errorf("no image in %s", dir)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].mod.After(found[j].mod) })

	newest := found[0].path
	if !strings.ContainsAny(newest, " \t\"'") {
		return newest, nil
	}
	// A path with whitespace would be mangled when typed into a prompt, so
	// copy it somewhere with a clean name instead of rewriting the original.
	data, err := os.ReadFile(newest)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", newest, err)
	}
	return writeCache(cacheDir, data, filepath.Ext(newest))
}

// writeCache stores image bytes under a generated, whitespace-free name.
func writeCache(dir string, data []byte, ext string) (string, error) {
	if dir == "" {
		return "", errors.New("no cache directory configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Millisecond precision so two quick pastes cannot collide.
	name := fmt.Sprintf("paste-%s%s", time.Now().Format("20060102-150405.000"), ext)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	pruneCache(dir, 20)
	return path, nil
}

// pruneCache keeps the cache directory from growing without bound.
func pruneCache(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= keep {
		return
	}
	type aged struct {
		path string
		mod  time.Time
	}
	var files []aged
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "paste-") {
			continue
		}
		if info, err := e.Info(); err == nil {
			files = append(files, aged{filepath.Join(dir, e.Name()), info.ModTime()})
		}
	}
	if len(files) <= keep {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	for _, f := range files[keep:] {
		_ = os.Remove(f.path)
	}
}

// Available reports whether any clipboard image tool is installed, for help
// text and diagnostics.
func Available() []string {
	var have []string
	for _, tool := range tools {
		if _, err := exec.LookPath(tool.name); err == nil {
			have = append(have, tool.name)
		}
	}
	return have
}

// agentTimeout bounds a fetch from the clipboard agent. Reading the Windows
// clipboard means starting PowerShell, which is not fast.
const agentTimeout = 20 * time.Second

// fromAgent pulls an image from a berth-clipd running on the machine that
// owns the clipboard, normally reached through an SSH reverse tunnel.
func fromAgent(o Options) (string, error) {
	if o.AgentURL == "" {
		return "", errors.New("no clipboard agent configured")
	}

	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(o.AgentURL, "/")+"/image", nil)
	if err != nil {
		return "", fmt.Errorf("agent url %q: %w", o.AgentURL, err)
	}
	if o.AgentToken != "" {
		req.Header.Set("X-Berth-Token", o.AgentToken)
	}

	client := &http.Client{Timeout: agentTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", agentReachError(o.AgentURL, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return "", errors.New("no image on the remote clipboard")
	case http.StatusOK:
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("clipboard agent said %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAgentImageBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading from the clipboard agent: %w", err)
	}
	if len(data) == 0 {
		return "", errors.New("no image on the remote clipboard")
	}
	if len(data) > maxAgentImageBytes {
		return "", fmt.Errorf("remote clipboard image is larger than %d MB", maxAgentImageBytes>>20)
	}

	// Anything can be listening on a forwarded port. Trust the bytes, not the
	// header: a wrong service answering should not produce a "pasted" message.
	ext, ok := imageExtForBytes(data)
	if !ok {
		return "", errors.New("the clipboard agent returned something that is not an image")
	}
	return writeCache(o.CacheDir, data, ext)
}

// maxAgentImageBytes caps what berth will accept over the tunnel.
const maxAgentImageBytes = 64 << 20

// imageExtForBytes identifies an image by its magic number.
func imageExtForBytes(data []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return ".png", true
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return ".jpg", true
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return ".gif", true
	case bytes.HasPrefix(data, []byte("BM")):
		return ".bmp", true
	case len(data) > 12 && bytes.HasPrefix(data, []byte("RIFF")) &&
		bytes.Equal(data[8:12], []byte("WEBP")):
		return ".webp", true
	default:
		return "", false
	}
}
