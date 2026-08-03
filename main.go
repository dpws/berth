// berth is a terminal UI for managing Claude Code and plain shell
// sessions backed by tmux: the session list lives on the left, the selected
// session's live terminal fills the rest of the screen.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/tmux"
	"github.com/dpws/berth/internal/ui"
	"github.com/dpws/berth/internal/usage"
)

// statusLineLimit bounds how much of Claude Code's payload is read. The real
// thing is a few kilobytes.
const statusLineLimit = 1 << 20

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "berth:", err)
		os.Exit(1)
	}
}

func run() error {
	showVersion := flag.Bool("version", false, "print version and exit")
	writeConfig := flag.Bool("write-config", false, "write a default config file and exit")
	flag.Usage = printUsage
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Println("berth", version)
		return nil
	case *writeConfig:
		if err := config.Default().Save(); err != nil {
			return err
		}
		fmt.Println("wrote", config.Path())
		return nil
	}

	// The status line command is driven by Claude Code, not by a terminal, and
	// has no business needing tmux or a config file.
	if flag.Arg(0) == "statusline" {
		return statusLine(flag.Args()[1:])
	}

	if err := tmux.Available(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "berth: ignoring bad config:", err)
	}

	if flag.Arg(0) == "ls" {
		return listSessions()
	}

	model := ui.New(cfg)
	opts := []tea.ProgramOption{
		tea.WithAltScreen(),
		// Focus reporting only matters once tmux has focus-events on, but
		// asking for it costs nothing when it does not.
		tea.WithReportFocus(),
	}
	if cfg.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	prog := tea.NewProgram(model, opts...)
	_, runErr := prog.Run()
	model.Close()
	return runErr
}

// statusLine is the command Claude Code runs to draw its status bar. berth
// takes what it needs out of the payload - the real rate limit windows, which
// Claude Code hands over and nothing else will - and then either prints a
// status line of its own or passes the payload on to the command you gave it,
// so wiring berth in does not cost you the status line you already had.
//
// A payload it cannot make sense of is passed over in silence: this runs
// several times a second inside someone else's UI, so berth failing should
// cost you a blank status line rather than a stream of complaints across it.
// A command you asked for that fails is a different matter, and does report.
func statusLine(args []string) error {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, statusLineLimit))
	if err != nil {
		return nil
	}
	if parsed, err := usage.ParseStatusLine(data); err == nil {
		_ = usage.SaveStatusLine(parsed, time.Now())
		if len(args) == 0 {
			if line := parsed.Render(); line != "" {
				fmt.Println(line)
			}
			return nil
		}
	}
	if len(args) == 0 {
		return nil
	}

	// "berth statusline -- jq ..." reads more clearly than without the dashes,
	// so accept it either way.
	if args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return nil
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func listSessions() error {
	sessions, err := tmux.List()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tKIND\tWINDOWS\tATTACHED\tCOMMAND\tDIR")
	for _, s := range sessions {
		kind := s.Kind
		if kind == "" {
			kind = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%t\t%s\t%s\n",
			s.Name, kind, s.Windows, s.Attached > 0, s.Command, s.Dir)
	}
	return w.Flush()
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `berth - tmux session manager for Claude Code and shells

usage:
  berth              launch the TUI
  berth ls           list sessions and exit
  berth statusline   read Claude Code's status line payload on stdin, record
                     the rate limits for berth, and print a status line;
                     append -- and a command to pass the payload on to it
  berth -write-config  write a default config to %s

flags:
`, config.Path())
	flag.PrintDefaults()
}
