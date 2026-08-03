// Package jsonl reads newly appended lines from the JSONL logs the agents
// keep. Those files only ever grow, so a reader that remembers where it got to
// can be polled often without re-reading megabytes each time.
package jsonl

import (
	"bufio"
	"errors"
	"io"
	"os"
)

// DefaultMaxLine is a generous ceiling for one record. Agent logs hold whole
// messages on a line, so they run far past bufio's default 64 KiB.
const DefaultMaxLine = 8 << 20

// Tailer follows one append-only file.
//
// The zero value starts from the beginning of the file. A Tailer is not safe
// for concurrent use.
type Tailer struct {
	offset int64
}

// Offset is how far into the file the Tailer has read.
func (t *Tailer) Offset() int64 { return t.offset }

// Reset sends the Tailer back to the start of the file.
func (t *Tailer) Reset() { t.offset = 0 }

// Read calls fn for each complete line added since the last Read. The slice
// handed to fn is only valid for that call.
//
// A file that shrank was truncated or replaced, so reading starts over. A
// trailing line with no newline is left for the next Read, so a record being
// written right now is never parsed in half.
func (t *Tailer) Read(path string, maxLine int, fn func(line []byte)) error {
	if maxLine <= 0 {
		maxLine = DefaultMaxLine
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < t.offset {
		t.offset = 0
	}
	if info.Size() == t.offset {
		return nil
	}
	if t.offset > 0 {
		if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
			return err
		}
	}

	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, n, err := readLine(r, maxLine)
		if err != nil {
			// Either the end of the file or a half-written trailing line.
			// Neither is counted, so the next Read picks it up whole.
			return nil
		}
		t.offset += n
		if line != nil {
			fn(line)
		}
	}
}

// readLine reads through the next newline, returning the line and how many
// bytes it consumed. A line longer than max is still consumed, but comes back
// as nil rather than being held in memory - skipping its contents is better
// than stalling on it forever. An error means the line has no terminator yet,
// in which case the count must be discarded so the line can be re-read whole.
func readLine(r *bufio.Reader, max int) ([]byte, int64, error) {
	var (
		out     []byte
		read    int64
		tooLong bool
	)
	for {
		chunk, err := r.ReadSlice('\n')
		read += int64(len(chunk))
		if !tooLong {
			if len(out)+len(chunk) > max {
				out, tooLong = nil, true
			} else {
				out = append(out, chunk...)
			}
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil:
			return nil, read, err
		case tooLong:
			return nil, read, nil
		default:
			return out, read, nil
		}
	}
}
