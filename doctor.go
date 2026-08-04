package main

import (
	"fmt"
	"strings"

	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/doctor"
)

// runDoctor prints what the software berth sits on is set to, and what each
// setting costs while it is wrong.
//
// Without --fix it changes nothing at all, and prints the command for each
// finding so the work can be done by hand. That is the default on purpose: most
// of these live in files berth did not write, and reading a report is a much
// smaller thing to agree to than letting a program edit your tmux.conf.
func runDoctor(args []string) error {
	var fix bool
	for _, a := range args {
		switch a {
		case "--fix", "-fix":
			fix = true
		case "--keys", "-keys":
			// Whether a terminal can tell shift+enter from enter is the one
			// thing here that cannot be worked out by looking; it has to be
			// pressed.
			return doctor.ProbeKeys()
		default:
			return fmt.Errorf("unknown option %q; berth doctor takes --fix or --keys", a)
		}
	}

	cfg, _ := config.Load()
	all := doctor.Run()

	var fixed, failed, manual int
	subject := doctor.Subject("")
	for _, f := range all {
		if f.Subject != subject {
			subject = f.Subject
			fmt.Printf("\n%s\n", strings.ToUpper(string(subject)))
		}

		fmt.Printf("  %-9s %s\n", mark(f.Severity), f.Summary)
		if f.Severity == doctor.OK {
			continue
		}
		if skipped(cfg, f.Key) {
			fmt.Printf("            skipped in your config\n")
			continue
		}
		if f.Detail != "" {
			fmt.Printf("            %s\n", wrap(f.Detail, 12))
		}

		switch {
		case !fix && f.Command != "":
			fmt.Printf("            fix: %s\n", wrap(f.Command, 17))
		case !fix:
			manual++
		case f.Fixable():
			if err := f.Fix(); err != nil {
				failed++
				fmt.Printf("            could not fix: %v\n", err)
				break
			}
			fixed++
			fmt.Printf("            fixed\n")
		default:
			manual++
			if f.Command != "" {
				fmt.Printf("            by hand: %s\n", wrap(f.Command, 21))
			}
		}
	}

	fmt.Println()
	switch {
	case fix && fixed > 0:
		fmt.Printf("fixed %d. Any file berth edited was copied to <file>%s first.\n",
			fixed, ".berth-bak")
		if manual > 0 {
			fmt.Printf("%d still need doing by hand.\n", manual)
		}
		fmt.Println("tmux options are applied to the running server as well as written,")
		fmt.Println("so they take effect now; a terminal setting needs the terminal restarted.")
	case fix:
		fmt.Println("nothing berth could fix on its own.")
	default:
		n := len(doctor.Actionable(all, cfg.DoctorSkipped))
		if n == 0 {
			fmt.Println("nothing to do.")
		} else {
			fmt.Printf("%d worth looking at. Run \"berth doctor --fix\" to apply what berth can.\n", n)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d could not be fixed", failed)
	}
	return nil
}

func skipped(cfg config.Config, key string) bool {
	for _, k := range cfg.DoctorSkipped {
		if k == key {
			return true
		}
	}
	return false
}

// mark labels a line so the report can be read down the left edge.
func mark(s doctor.Severity) string {
	switch s {
	case doctor.OK:
		return "ok"
	case doctor.Broken:
		return "BROKEN"
	case doctor.Degraded:
		return "could be"
	default:
		return "?"
	}
}

// wrap folds a long line to the terminal's width, indenting what it folds so
// the report keeps its columns.
func wrap(s string, indent int) string {
	const width = 78
	limit := width - indent
	if limit < 20 {
		limit = 20
	}

	var out strings.Builder
	col := 0
	for i, word := range strings.Fields(s) {
		switch {
		case i == 0:
		case col+1+len(word) > limit:
			out.WriteString("\n" + strings.Repeat(" ", indent))
			col = 0
		default:
			out.WriteString(" ")
			col++
		}
		out.WriteString(word)
		col += len(word)
	}
	return out.String()
}
