package main

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 32))

func stubClipboard(t *testing.T, data []byte, err error) {
	t.Helper()
	original := readClipboard
	readClipboard = func() ([]byte, error) { return data, err }
	t.Cleanup(func() { readClipboard = original })
}

func get(t *testing.T, token, sendToken string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/image", nil)
	if sendToken != "" {
		req.Header.Set("X-Berth-Token", sendToken)
	}
	rec := httptest.NewRecorder()
	serveImage(rec, req, token)
	return rec.Result()
}

func TestServesClipboardImage(t *testing.T) {
	stubClipboard(t, pngBytes, nil)

	resp := get(t, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("content type = %q, want image/png", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(pngBytes) {
		t.Error("body did not round-trip the clipboard bytes")
	}
}

func TestEmptyClipboardIsNotAnError(t *testing.T) {
	stubClipboard(t, nil, errNoImage)

	if resp := get(t, "", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

func TestTokenIsEnforced(t *testing.T) {
	stubClipboard(t, pngBytes, nil)

	if resp := get(t, "sekrit", ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a missing token gave %d, want 403", resp.StatusCode)
	}
	if resp := get(t, "sekrit", "wrong"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a wrong token gave %d, want 403", resp.StatusCode)
	}
	if resp := get(t, "sekrit", "sekrit"); resp.StatusCode != http.StatusOK {
		t.Errorf("the right token gave %d, want 200", resp.StatusCode)
	}
}

// Serving a clipboard to the whole network is a much bigger deal than it
// looks, so a non-loopback bind without a token must refuse to start.
func TestRefusesUnauthenticatedNonLoopbackBind(t *testing.T) {
	if err := run("0.0.0.0:8377", ""); err == nil {
		t.Fatal("binding 0.0.0.0 without a token should be refused")
	}
	if err := run("100.121.192.122:8377", ""); err == nil {
		t.Fatal("binding a tailnet address without a token should be refused")
	}
}

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8377":       true,
		"localhost:8377":       true,
		"[::1]:8377":           true,
		"0.0.0.0:8377":         false,
		"100.121.192.122:8377": false,
		"192.168.1.233:8377":   false,
		"not-an-address":       false,
	}
	for addr, want := range cases {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

// PowerShell's -EncodedCommand expects base64 of UTF-16LE, and getting this
// wrong fails in a way that is very hard to read from the other machine.
func TestEncodeUTF16LEMatchesPowerShellsExpectation(t *testing.T) {
	encoded := encodeUTF16LE("echo hi")

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("not valid base64: %v", err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("UTF-16 needs an even byte count, got %d", len(raw))
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8) // little endian
	}
	if got := string(utf16.Decode(units)); got != "echo hi" {
		t.Errorf("round-trip gave %q", got)
	}
}

func TestPowershellScriptCoversImagesAndCopiedFiles(t *testing.T) {
	for _, want := range []string{"ContainsImage", "ContainsFileDropList", "ImageFormat]::Png"} {
		if !strings.Contains(powershellScript, want) {
			t.Errorf("the clipboard script no longer mentions %q", want)
		}
	}
}
