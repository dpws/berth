// berth is a terminal UI for managing Claude Code and plain shell
// sessions backed by tmux: the session list lives on the left, the selected
// session's live terminal fills the rest of the screen.
package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/tmux"
	"github.com/dpws/berth/internal/ui"
)

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
	flag.Usage = usage
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

func usage() {
	fmt.Fprintf(os.Stderr, `berth - tmux session manager for Claude Code and shells

usage:
  berth              launch the TUI
  berth ls           list sessions and exit
  berth -write-config  write a default config to %s

flags:
`, config.Path())
	flag.PrintDefaults()
}
