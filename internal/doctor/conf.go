package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// marker labels the block berth appends, so a later read can tell its own
// additions from the lines someone wrote themselves, and so anyone opening the
// file can see where they came from.
const marker = "# added by berth doctor"

// appendToTmuxConf adds a line to ~/.tmux.conf.
func appendToTmuxConf(line string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding your home directory: %w", err)
	}
	return appendLine(filepath.Join(home, ".tmux.conf"), line)
}

// appendLine adds one line to a config file, having first copied the file
// aside.
//
// The copy is the whole safety story here: berth is editing a file it did not
// write and cannot parse, so the only honest promise is that the original is
// still there afterwards. A line already present is left alone rather than
// repeated, since these files are read top to bottom and a second copy of a
// setting is at best noise and at worst an argument with the first.
func appendLine(path, line string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if hasLine(string(existing), line) {
		return nil
	}

	// Back up only the first time berth touches this file. Several findings
	// are usually fixed in one go, and copying the file before each of them
	// would leave a "backup" containing berth's own earlier additions - which
	// is not the thing anyone wants back. The marker is what says berth has
	// been here before.
	if len(existing) > 0 && !strings.Contains(string(existing), marker) {
		if err := backup(path, existing); err != nil {
			return err
		}
	}

	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	if !strings.Contains(string(existing), marker) {
		b.WriteString("\n" + marker + "\n")
	}
	b.WriteString(line + "\n")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	// Written whole and renamed over, so an interrupted write cannot leave
	// someone with half a config file.
	tmp := path + ".berth-tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// hasLine reports whether the file already says this, ignoring leading and
// trailing space and any line that has been commented out.
func hasLine(body, line string) bool {
	want := strings.Join(strings.Fields(line), " ")
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.Join(strings.Fields(l), " ") == want {
			return true
		}
	}
	return false
}

// backupSuffix is what a saved copy is called. One copy, not a numbered series:
// the point is to be able to undo the change just made, and a directory filling
// with .berth-bak.7 files is its own kind of mess.
const backupSuffix = ".berth-bak"

func backup(path string, body []byte) error {
	if err := os.WriteFile(path+backupSuffix, body, 0o644); err != nil {
		return fmt.Errorf("backing up %s: %w", path, err)
	}
	return nil
}

// BackupPath is where the copy of a file berth edited is kept, so a report can
// say where to look.
func BackupPath(path string) string { return path + backupSuffix }
