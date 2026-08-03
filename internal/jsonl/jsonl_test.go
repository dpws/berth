package jsonl

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func collect(t *testing.T, tail *Tailer, path string) []string {
	t.Helper()
	var got []string
	if err := tail.Read(path, 0, func(line []byte) {
		got = append(got, strings.TrimRight(string(line), "\n"))
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got
}

func TestTailerReadsOnlyWhatIsNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, path, "one\ntwo\n")

	var tail Tailer
	if got := collect(t, &tail, path); len(got) != 2 {
		t.Fatalf("first read = %q, want both lines", got)
	}
	if got := collect(t, &tail, path); len(got) != 0 {
		t.Errorf("second read = %q, want nothing new", got)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("three\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got := collect(t, &tail, path)
	if len(got) != 1 || got[0] != "three" {
		t.Errorf("after append = %q, want just the new line", got)
	}
}

// A record still being written must not be parsed in half.
func TestTailerLeavesAPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, path, "one\ntwo-partial")

	var tail Tailer
	got := collect(t, &tail, path)
	if len(got) != 1 || got[0] != "one" {
		t.Fatalf("got %q, want only the complete line", got)
	}

	// Once finished, the line arrives whole rather than from the middle.
	write(t, path, "one\ntwo-partial-now-complete\n")
	got = collect(t, &tail, path)
	if len(got) != 1 || got[0] != "two-partial-now-complete" {
		t.Errorf("got %q, want the completed line in full", got)
	}
}

func TestTailerRestartsOnTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, path, "one\ntwo\nthree\n")

	var tail Tailer
	collect(t, &tail, path)

	write(t, path, "fresh\n")
	got := collect(t, &tail, path)
	if len(got) != 1 || got[0] != "fresh" {
		t.Errorf("got %q, want the replaced file read from the start", got)
	}
}

// An over-long line is skipped rather than held in memory, but the reader must
// still step over it or it would stall on that file forever.
func TestTailerStepsOverAnOverlongLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	write(t, path, "short\n"+strings.Repeat("x", 5000)+"\nafter\n")

	var tail Tailer
	var got []string
	if err := tail.Read(path, 64, func(line []byte) {
		got = append(got, strings.TrimRight(string(line), "\n"))
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 2 || got[0] != "short" || got[1] != "after" {
		t.Errorf("got %q, want the short lines with the long one skipped", got)
	}
}

func TestTailerMissingFile(t *testing.T) {
	var tail Tailer
	err := tail.Read(filepath.Join(t.TempDir(), "absent"), 0, func([]byte) {
		t.Error("callback ran for a missing file")
	})
	if err == nil {
		t.Error("want an error for a missing file")
	}
}

func TestReadLine(t *testing.T) {
	r := bufio.NewReaderSize(strings.NewReader("ab\n"+strings.Repeat("x", 20)+"\ncd\nef"), 16)

	line, n, err := readLine(r, 8)
	if err != nil || string(line) != "ab\n" || n != 3 {
		t.Fatalf("first = %q, %d, %v; want \"ab\\n\", 3, nil", line, n, err)
	}

	// Too long to hold, but still stepped over so the next line is reachable.
	line, n, err = readLine(r, 8)
	if err != nil || line != nil || n != 21 {
		t.Fatalf("long = %q, %d, %v; want nil, 21, nil", line, n, err)
	}

	line, n, err = readLine(r, 8)
	if err != nil || string(line) != "cd\n" || n != 3 {
		t.Fatalf("third = %q, %d, %v; want \"cd\\n\", 3, nil", line, n, err)
	}

	// An unterminated trailing line reports an error so its bytes are dropped.
	if _, _, err = readLine(r, 8); err == nil {
		t.Error("want an error for a line with no terminator")
	}
}
