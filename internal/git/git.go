// Package git reads the state of a working tree: which branch it is on, how
// far that branch has drifted from its upstream, and how much in it is
// uncommitted. berth shows this for the directory the selected session is
// sitting in, so an agent left mid-change says so without you going to look.
package git

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// readTimeout bounds one read. git status walks the whole worktree, and a
// session parked on a network mount or a very large repository can take long
// enough that abandoning the answer beats holding the refresh open.
const readTimeout = 3 * time.Second

// shortOID is how much of an object id identifies a detached head. Seven is
// what git itself abbreviates to by default.
const shortOID = 7

// Status is one directory's worth of repository state.
type Status struct {
	// Branch is the branch name, or an abbreviated object id when the head is
	// detached. Empty on a branch that has no commits yet and no name to give.
	Branch   string
	Detached bool

	// Ahead and Behind count commits against the branch's upstream. Both stay
	// zero when there is no upstream to compare against.
	Ahead  int
	Behind int

	// Modified, Added and Deleted count paths, not lines: what the bar answers
	// is "is there uncommitted work here", and a file count says that in less
	// room than a diffstat. Untracked paths count as added, since work that was
	// never staged is still work that is not committed.
	Modified int
	Added    int
	Deleted  int

	// LinesAdded and LinesDeleted count lines rather than files, and only the
	// unstaged ones: the size of what is still in the working tree, in front of
	// whoever is about to keep or throw it away. Staged work has already been
	// decided about, and untracked files have no diff to measure.
	LinesAdded   int
	LinesDeleted int
}

// Clean reports whether nothing in the worktree is uncommitted.
func (s Status) Clean() bool { return s.Modified == 0 && s.Added == 0 && s.Deleted == 0 }

// Unstaged reports whether any tracked file differs from the index, which is
// the question the line counts answer.
func (s Status) Unstaged() bool { return s.LinesAdded > 0 || s.LinesDeleted > 0 }

// InSync reports whether the branch is level with its upstream. A branch with
// no upstream at all is in sync: there is nothing it could be behind.
func (s Status) InSync() bool { return s.Ahead == 0 && s.Behind == 0 }

// Read reports the state of the worktree at dir.
//
// The second return is false when dir is not inside a repository, which is not
// an error worth reporting: most directories a shell session starts in are not
// repositories, and the bar simply has nothing to say about them.
func Read(dir string) (Status, bool) {
	if dir == "" {
		return Status{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	// porcelain=v2 answers the branch, its drift and the file counts from one
	// walk of the worktree, and is the format git documents as stable. The v1
	// output carries no ahead/behind line at all.
	out, err := run(ctx, dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return Status{}, false
	}
	s := parse(out)

	// The line counts need their own command - status counts files, and there
	// is no flag that makes it count lines. A diff of the unstaged changes only,
	// so this never walks history.
	if out, err := run(ctx, dir, "diff", "--numstat"); err == nil {
		s.LinesAdded, s.LinesDeleted = parseNumstat(out)
	}
	return s, true
}

// run invokes git in dir and returns its standard output.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// GIT_OPTIONAL_LOCKS=0 stops these taking the index lock. berth reads on a
	// timer, and an agent running its own git in that directory should never
	// find itself waiting behind a read berth asked for on its own account.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")

	out, err := cmd.Output()
	return string(out), err
}

// parseNumstat totals the added and deleted columns of git diff --numstat.
// Binary files report their two counts as "-", carry no line count anyone
// could add up, and are skipped.
func parseNumstat(out string) (added, deleted int) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		a, errA := strconv.Atoi(fields[0])
		d, errD := strconv.Atoi(fields[1])
		if errA != nil || errD != nil {
			continue
		}
		added += a
		deleted += d
	}
	return added, deleted
}

// parse reads porcelain v2. Lines beginning "# " are headers carrying the
// branch and its drift; every other line is one path, and its first field says
// what kind of change it is.
func parse(out string) Status {
	var s Status
	var oid string

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			oid = strings.TrimPrefix(line, "# branch.oid ")

		case strings.HasPrefix(line, "# branch.head "):
			name := strings.TrimPrefix(line, "# branch.head ")
			if name == "(detached)" {
				s.Detached = true
			} else {
				s.Branch = name
			}

		case strings.HasPrefix(line, "# branch.ab "):
			s.Ahead, s.Behind = parseAheadBehind(strings.TrimPrefix(line, "# branch.ab "))

		case strings.HasPrefix(line, "? "):
			s.Added++

		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			// Field 1 is the two status letters, staged then unstaged. Reading
			// only that field means a path with spaces or quoting in it cannot
			// throw the count off.
			if fields := strings.Fields(line); len(fields) >= 2 {
				s.count(fields[1])
			}

		case strings.HasPrefix(line, "u "):
			// Unmerged. However it got that way it is a file needing attention,
			// which is what the modified count is for.
			s.Modified++
		}
	}

	// A detached head has no name to show, so it borrows the object id it is
	// sitting on - the same thing git itself prints.
	if s.Detached && len(oid) >= shortOID {
		s.Branch = oid[:shortOID]
	}
	return s
}

// count classifies one path by its two status letters. A path can be both
// staged and changed again since, so this asks what happened to it overall
// rather than trying to report both halves in a bar this size.
func (s *Status) count(xy string) {
	switch {
	case strings.ContainsRune(xy, 'D'):
		s.Deleted++
	case strings.ContainsRune(xy, 'A'):
		s.Added++
	default:
		// M, R and C all land here: the file is still there and differs from
		// what was committed.
		s.Modified++
	}
}

// parseAheadBehind reads the "+2 -1" of a branch.ab header. git writes both
// counts with a sign always present, so anything else is a format we do not
// know and is better read as no drift than as a wrong number.
func parseAheadBehind(s string) (ahead, behind int) {
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return 0, 0
	}
	a, err := strconv.Atoi(strings.TrimPrefix(fields[0], "+"))
	if err != nil {
		return 0, 0
	}
	b, err := strconv.Atoi(strings.TrimPrefix(fields[1], "-"))
	if err != nil {
		return 0, 0
	}
	return a, b
}
