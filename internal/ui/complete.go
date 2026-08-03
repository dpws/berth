package ui

import (
	"os"
	"path/filepath"
	"strings"
)

// maxCompletions bounds what is offered. A directory of a thousand entries is
// not a menu, and the dialog has a handful of rows to spare.
const maxCompletions = 8

// completeDir works out what a half-typed directory path could become.
//
// It returns the text the field should hold - the longest extension that is
// still unambiguous, in the manner of a shell - and the directories that
// match, for showing underneath. Only directories are offered: this is the
// field that says where a session starts.
func completeDir(input string) (completed string, matches []string) {
	expanded := expandHome(input)

	dir, prefix := filepath.Split(expanded)
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return input, nil
	}

	for _, e := range entries {
		name := e.Name()
		if !isDir(dir, e) {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		// Hidden directories stay out of the way until they are asked for.
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		matches = append(matches, name)
	}
	if len(matches) == 0 {
		return input, nil
	}

	// Extending to the common prefix is what makes repeated tabs feel like a
	// shell: each one gets as far as it can without guessing.
	grown := commonPrefix(matches)
	completed = filepath.Join(dir, grown)
	if len(matches) == 1 {
		// One answer, so the separator can be added and the next level typed.
		completed += string(filepath.Separator)
	}
	return unexpandHome(completed, input), matches
}

// isDir reports whether an entry is a directory, following symlinks, since a
// link to a directory is a perfectly good place to start a session.
func isDir(dir string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && info.IsDir()
}

// commonPrefix returns the longest prefix shared by every name.
func commonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	out := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, out) {
			out = out[:len(out)-1]
			if out == "" {
				return ""
			}
		}
	}
	return out
}

// expandHome turns a leading ~ into the home directory.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home + string(filepath.Separator)
	}
	return filepath.Join(home, p[2:])
}

// unexpandHome puts the ~ back, so completing a path that was typed with one
// does not silently rewrite it into something longer.
func unexpandHome(completed, original string) string {
	if !strings.HasPrefix(original, "~") {
		return completed
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return completed
	}
	if completed == home {
		return "~"
	}
	if rest := strings.TrimPrefix(completed, home+string(filepath.Separator)); rest != completed {
		return "~" + string(filepath.Separator) + rest
	}
	return completed
}
