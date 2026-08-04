package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// repo builds a throwaway repository with one commit, and returns its path.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available:", err)
	}
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// A developer's own git config could set a default branch, sign every
		// commit, or install hooks; none of that should reach these.
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=berth", "GIT_AUTHOR_EMAIL=berth@example.com",
			"GIT_COMMITTER_NAME=berth", "GIT_COMMITTER_EMAIL=berth@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "--initial-branch=main")
	write(t, dir, "kept.txt", "one\n")
	run("add", ".")
	run("commit", "-m", "first")
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=berth", "GIT_AUTHOR_EMAIL=berth@example.com",
		"GIT_COMMITTER_NAME=berth", "GIT_COMMITTER_EMAIL=berth@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestCleanRepositoryReadsAsCleanAndInSync(t *testing.T) {
	dir := repo(t)

	st, ok := Read(dir)
	if !ok {
		t.Fatal("a repository read as not being one")
	}
	if st.Branch != "main" {
		t.Errorf("branch = %q, want main", st.Branch)
	}
	if !st.Clean() {
		t.Errorf("a fresh commit left the tree dirty: %+v", st)
	}
	if !st.InSync() {
		t.Errorf("no upstream should read as in sync, got ahead %d behind %d", st.Ahead, st.Behind)
	}
}

// The three counts are what the bar spends its width on, so each has to mean
// what it says rather than all changes landing in one bucket.
func TestChangesAreCountedByKind(t *testing.T) {
	dir := repo(t)

	write(t, dir, "kept.txt", "one\ntwo\n") // modified
	write(t, dir, "fresh.txt", "new\n")     // untracked, counts as added
	git(t, dir, "rm", "-q", "--cached", "kept.txt")

	// Staging the removal of kept.txt makes it deleted-and-untracked at once,
	// which is not what this is measuring; restore the index and delete a file
	// outright instead.
	git(t, dir, "reset", "-q")
	write(t, dir, "gone.txt", "bye\n")
	git(t, dir, "add", "gone.txt")
	git(t, dir, "commit", "-m", "second")
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	st, ok := Read(dir)
	if !ok {
		t.Fatal("not a repository")
	}
	if st.Modified != 1 {
		t.Errorf("modified = %d, want 1 (%+v)", st.Modified, st)
	}
	if st.Added != 1 {
		t.Errorf("added = %d, want 1 (%+v)", st.Added, st)
	}
	if st.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (%+v)", st.Deleted, st)
	}
	if st.Clean() {
		t.Error("a tree with three changes in it reported clean")
	}
}

func TestDirectoryOutsideARepositoryIsNotAnError(t *testing.T) {
	// t.TempDir is not inside a repository, unless the temp root happens to be
	// in one - which is what the ok flag is for either way.
	dir := t.TempDir()
	if _, ok := Read(dir); ok {
		t.Skip("the temp directory is itself inside a repository")
	}
	if _, ok := Read(""); ok {
		t.Error("an empty path read as a repository")
	}
}

// The header parsing is where a format change would bite first, and it is
// cheap to pin without building a repository for every case.
func TestParseReadsTheHeaders(t *testing.T) {
	out := "# branch.oid a92db72ff00dcafe1234\n" +
		"# branch.head feature/thing\n" +
		"# branch.upstream origin/feature/thing\n" +
		"# branch.ab +3 -2\n"

	st := parse(out)
	if st.Branch != "feature/thing" {
		t.Errorf("branch = %q", st.Branch)
	}
	if st.Ahead != 3 || st.Behind != 2 {
		t.Errorf("ahead/behind = %d/%d, want 3/2", st.Ahead, st.Behind)
	}
	if st.Detached {
		t.Error("a named branch read as detached")
	}
}

// A detached head has no name, so it shows the commit it is sitting on the way
// git itself does.
func TestDetachedHeadShowsTheObjectID(t *testing.T) {
	st := parse("# branch.oid a92db72ff00dcafe1234\n# branch.head (detached)\n")
	if !st.Detached {
		t.Fatal("(detached) did not read as detached")
	}
	if st.Branch != "a92db72" {
		t.Errorf("branch = %q, want the abbreviated id a92db72", st.Branch)
	}
}

// A path with a space in it must not shift the field the status letters are
// read from, or the counts drift with the filenames.
func TestPathsWithSpacesDoNotShiftTheCount(t *testing.T) {
	out := "# branch.head main\n" +
		"1 .M N... 100644 100644 100644 abc def my notes.txt\n" +
		"1 A. N... 000000 100644 100644 abc def another file.md\n" +
		"1 .D N... 100644 100644 000000 abc def gone for good.txt\n"

	st := parse(out)
	if st.Modified != 1 || st.Added != 1 || st.Deleted != 1 {
		t.Errorf("counts = %+v, want one of each", st)
	}
}

// Untracked files are work that is not committed, which is the question the
// bar is answering.
func TestUntrackedCountsAsAdded(t *testing.T) {
	st := parse("# branch.head main\n? one.txt\n? two.txt\n")
	if st.Added != 2 {
		t.Errorf("added = %d, want 2", st.Added)
	}
	if st.Modified != 0 || st.Deleted != 0 {
		t.Errorf("untracked files landed in another bucket: %+v", st)
	}
}

// A branch.ab that is not the shape we expect should read as no drift rather
// than as some number picked out of a line we did not understand.
func TestUnreadableAheadBehindIsNoDrift(t *testing.T) {
	for _, line := range []string{
		"# branch.ab\n",
		"# branch.ab +1\n",
		"# branch.ab up down\n",
		"# branch.ab +1 -2 -3\n",
	} {
		st := parse("# branch.head main\n" + line)
		if st.Ahead != 0 || st.Behind != 0 {
			t.Errorf("%q gave ahead %d behind %d, want 0/0", line, st.Ahead, st.Behind)
		}
	}
}

// The line counts are of the unstaged changes only: what is still in the
// working tree in front of whoever is deciding whether to keep it.
func TestUnstagedLinesAreCounted(t *testing.T) {
	dir := repo(t)

	write(t, dir, "kept.txt", "one\ntwo\nthree\n") // +2 unstaged

	st, ok := Read(dir)
	if !ok {
		t.Fatal("not a repository")
	}
	if st.LinesAdded != 2 || st.LinesDeleted != 0 {
		t.Errorf("lines = +%d/-%d, want +2/-0", st.LinesAdded, st.LinesDeleted)
	}
	if !st.Unstaged() {
		t.Error("a modified working tree did not read as unstaged")
	}

	// Staging it moves the work out of the unstaged diff, so the line counts
	// go quiet while the file count does not.
	git(t, dir, "add", "kept.txt")
	st, _ = Read(dir)
	if st.LinesAdded != 0 || st.LinesDeleted != 0 {
		t.Errorf("staged work still counted as unstaged lines: +%d/-%d",
			st.LinesAdded, st.LinesDeleted)
	}
	if st.Modified != 1 {
		t.Errorf("staged file should still count as modified, got %d", st.Modified)
	}
	if st.Unstaged() {
		t.Error("a fully staged tree still reported unstaged work")
	}
}

// A deletion is lines lost, and has to be told apart from lines gained.
func TestDeletedLinesAreCountedSeparately(t *testing.T) {
	dir := repo(t)
	write(t, dir, "kept.txt", "one\ntwo\nthree\nfour\n")
	git(t, dir, "commit", "-qam", "grow")

	write(t, dir, "kept.txt", "one\n") // -3 unstaged

	st, _ := Read(dir)
	if st.LinesAdded != 0 || st.LinesDeleted != 3 {
		t.Errorf("lines = +%d/-%d, want +0/-3", st.LinesAdded, st.LinesDeleted)
	}
}

// Binary files report their counts as "-" and cannot be added up. Skipping
// them must not throw off the files that can.
func TestBinaryFilesDoNotBreakTheLineCount(t *testing.T) {
	added, deleted := parseNumstat("12\t3\ttext.go\n-\t-\timage.png\n7\t1\tmore.go\n")
	if added != 19 || deleted != 4 {
		t.Errorf("numstat totals = +%d/-%d, want +19/-4", added, deleted)
	}
}

func TestEmptyNumstatIsZero(t *testing.T) {
	for _, in := range []string{"", "\n", "   \n"} {
		if a, d := parseNumstat(in); a != 0 || d != 0 {
			t.Errorf("parseNumstat(%q) = +%d/-%d, want zero", in, a, d)
		}
	}
}
