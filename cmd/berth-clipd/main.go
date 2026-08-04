// Command berth-clipd serves the local clipboard's image over HTTP so a
// remote berth can paste it into a session.
//
// It exists because a clipboard can only be read by a process on the machine
// that owns it, and no terminal or SSH feature carries image bytes. Run this
// on your workstation, forward the port to the box berth runs on
//
//	ssh -R 8377:localhost:8377 you@remote
//
// and berth's paste key pulls real image bytes through the tunnel.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	defaultAddr = "127.0.0.1:8377"
	// readTimeout bounds a clipboard read. Shelling out to PowerShell is slow
	// to start, so this is generous.
	readTimeout = 15 * time.Second
	// maxImageBytes caps what a single clipboard read may return.
	maxImageBytes = 64 << 20
)

func main() {
	addr := flag.String("addr", defaultAddr, "address to listen on; must be loopback unless -token is set")
	token := flag.String("token", "", "shared secret required in the X-Berth-Token header")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("berth-clipd", version)
		return
	}

	if err := run(*addr, *token); err != nil {
		log.Fatalln("berth-clipd:", err)
	}
}

func run(addr, token string) error {
	// A clipboard server that anything on the network can query is a much
	// wider door than it looks. Loopback is the intended deployment; anything
	// else has to be deliberate and authenticated.
	if !isLoopback(addr) && token == "" {
		return fmt.Errorf("refusing to serve the clipboard on %s without -token", addr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "berth-clipd %s on %s\n", version, runtime.GOOS)
	})
	mux.HandleFunc("/image", func(w http.ResponseWriter, r *http.Request) {
		serveImage(w, r, token)
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listeners, err := listenLoopback(addr)
	if err != nil {
		return err
	}
	errs := make(chan error, len(listeners))
	for _, ln := range listeners {
		log.Printf("serving the clipboard on http://%s (%s)", ln.Addr(), runtime.GOOS)
		go func(ln net.Listener) { errs <- server.Serve(ln) }(ln)
	}
	return <-errs
}

// listenLoopback opens the listening sockets.
//
// A loopback address gets both families, because "ssh -R 8377:localhost:8377"
// resolves localhost on the machine running ssh, and on some systems that is
// ::1 first. Binding only 127.0.0.1 there leaves the tunnel forwarding to a
// port nothing is on, which surfaces at the far end as an agent that accepts
// the connection and then says nothing at all - the confusing failure this
// exists to prevent. Both are still loopback, so nothing new is exposed.
// Anything else binds exactly as written.
func listenLoopback(addr string) ([]net.Listener, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if !isLoopback(addr) {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, err
		}
		return []net.Listener{ln}, nil
	}

	var listeners []net.Listener
	for _, host := range []string{"127.0.0.1", "::1"} {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
		if err != nil {
			// A machine with IPv6 switched off should still serve on IPv4.
			continue
		}
		listeners = append(listeners, ln)
	}
	if len(listeners) == 0 {
		return nil, fmt.Errorf("could not listen on either loopback address at port %s", port)
	}
	return listeners, nil
}

func serveImage(w http.ResponseWriter, r *http.Request, token string) {
	if token != "" && r.Header.Get("X-Berth-Token") != token {
		http.Error(w, "bad or missing token", http.StatusForbidden)
		return
	}

	data, err := readClipboard()
	switch {
	case errors.Is(err, errNoImage):
		// Not an error: the clipboard simply holds something else.
		w.WriteHeader(http.StatusNoContent)
		return
	case err != nil:
		log.Println("clipboard read failed:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	if _, err := w.Write(data); err != nil {
		log.Println("write failed:", err)
	}
}

// errNoImage means the clipboard works but holds no image.
var errNoImage = errors.New("no image on the clipboard")

// readClipboard is a variable so tests can stand in for a real clipboard,
// which no CI machine has.
var readClipboard = readClipboardImage

// readClipboardImage returns PNG (or original format, for a copied file) bytes
// from the system clipboard.
func readClipboardImage() ([]byte, error) {
	switch runtime.GOOS {
	case "windows":
		return readWindows()
	case "darwin":
		return readDarwin()
	default:
		return readUnix()
	}
}

// powershellScript reads an image from the Windows clipboard, or the first
// image among copied files, and writes it as base64. Base64 avoids PowerShell
// mangling raw bytes on the way through stdout.
const powershellScript = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$out = ''
if ([System.Windows.Forms.Clipboard]::ContainsImage()) {
  $img = [System.Windows.Forms.Clipboard]::GetImage()
  $ms = New-Object System.IO.MemoryStream
  $img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
  $out = [Convert]::ToBase64String($ms.ToArray())
} elseif ([System.Windows.Forms.Clipboard]::ContainsFileDropList()) {
  foreach ($f in [System.Windows.Forms.Clipboard]::GetFileDropList()) {
    if ($f -match '\.(png|jpg|jpeg|gif|bmp|webp)$') {
      $out = [Convert]::ToBase64String([IO.File]::ReadAllBytes($f))
      break
    }
  }
}
[Console]::Out.Write($out)
`

func readWindows() ([]byte, error) {
	// -EncodedCommand sidesteps every layer of Windows command-line quoting.
	// -Sta is required: the clipboard API is single-threaded-apartment only,
	// and a multi-threaded host returns nothing.
	out, err := runCmd("powershell.exe",
		"-NoProfile", "-NonInteractive", "-Sta",
		"-EncodedCommand", encodeUTF16LE(powershellScript))
	if err != nil {
		return nil, fmt.Errorf("powershell: %w", err)
	}
	encoded := strings.TrimSpace(string(out))
	if encoded == "" {
		return nil, errNoImage
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding powershell output: %w", err)
	}
	return data, nil
}

func readDarwin() ([]byte, error) {
	if _, err := exec.LookPath("pngpaste"); err != nil {
		return nil, errors.New("pngpaste not installed (brew install pngpaste)")
	}
	out, err := runCmd("pngpaste", "-")
	if err != nil || len(out) == 0 {
		return nil, errNoImage
	}
	return out, nil
}

func readUnix() ([]byte, error) {
	type reader struct {
		name    string
		targets []string
		read    func(mime string) []string
	}
	readers := []reader{
		{"wl-paste", []string{"--list-types"}, func(mime string) []string {
			return []string{"--no-newline", "--type", mime}
		}},
		{"xclip", []string{"-selection", "clipboard", "-t", "TARGETS", "-o"}, func(mime string) []string {
			return []string{"-selection", "clipboard", "-t", mime, "-o"}
		}},
	}
	for _, r := range readers {
		if _, err := exec.LookPath(r.name); err != nil {
			continue
		}
		targets, err := runCmd(r.name, r.targets...)
		if err != nil {
			continue
		}
		// Ask for a type the clipboard actually offers. Seeing "image/" and
		// then demanding image/png regardless meant a browser offering only
		// image/jpeg was reported as no image on the clipboard at all.
		mime := pickImageType(string(targets))
		if mime == "" {
			continue
		}
		data, err := runCmd(r.name, r.read(mime)...)
		if err != nil || len(data) == 0 {
			continue
		}
		return data, nil
	}
	return nil, errNoImage
}

// pickImageType chooses an image type from a newline-separated target list,
// preferring PNG because it is lossless and universally readable. berth has its
// own copy of this in internal/clip; berth-clipd is deliberately a standalone
// binary that imports nothing from the rest of the tree.
func pickImageType(list string) string {
	var found []string
	for _, line := range strings.Split(list, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "image/") {
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

// runCmd runs a clipboard helper with a timeout and returns its stdout.
func runCmd(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	out := &capped{max: maxImageBytes}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = out, &stderr
	hideWindow(cmd)
	err := cmd.Run()

	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s timed out after %s", name, readTimeout)
	}
	if out.over {
		return nil, fmt.Errorf("clipboard image is larger than %d bytes", maxImageBytes)
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s: %s", name, msg)
		}
		return nil, err
	}
	return out.buf.Bytes(), nil
}

// capped collects a helper's output up to a ceiling and swallows the rest.
//
// Buffering the whole of it and measuring afterwards is no ceiling at all: a
// several-hundred-megabyte clipboard entry is held in memory in full - twice
// over, once base64-encoded - before anything objects. The tail is still read
// rather than refused, because a helper blocked on a pipe nobody is draining
// would sit there until the timeout instead of failing now.
type capped struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room < len(p) {
		c.over = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

// encodeUTF16LE renders a script the way PowerShell's -EncodedCommand expects.
func encodeUTF16LE(s string) string {
	units := utf16.Encode([]rune(s))
	buf := make([]byte, 0, len(units)*2)
	for _, u := range units {
		buf = append(buf, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// isLoopback reports whether addr binds only the loopback interface.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
