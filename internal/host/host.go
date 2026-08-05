// Package host reports what the machine berth is running on has left: how
// loaded its processors are, how much memory is in use, and how full the disk
// under it is.
//
// Everything here is read from the operating system's own accounting - /proc on
// Linux, sysctl and vm_stat on macOS - and nothing is polled over the network
// or needs privileges. A figure the system will not give up is reported as
// missing rather than guessed at, so a row that cannot be filled is left out
// instead of showing a confident zero.
package host

import "fmt"

// diskPath is the filesystem the disk row is about. It is the root rather than
// anything cleverer: a session's own directory would change as the cursor
// moved, and a row that means something different depending on what is
// selected is worse than one that always means the same thing.
const diskPath = "/"

// Meter is one measurement: how much of something is gone, and what is left of
// it in the units the thing is counted in.
type Meter struct {
	// Percent is how much is in use, from 0 to 100.
	Percent float64
	// Left is what remains, already written for a narrow column - bytes as
	// "4.8G", and for the processors the load average the percentage came from.
	Left string
	// Known is false when the system did not say, which is not the same as
	// zero.
	Known bool
}

// Stats is what berth knows about the machine right now.
type Stats struct {
	CPU  Meter
	Mem  Meter
	Disk Meter
}

// Empty reports whether nothing could be read at all, which is what an
// unsupported system looks like.
func (s Stats) Empty() bool { return !s.CPU.Known && !s.Mem.Known && !s.Disk.Known }

// Read takes one reading of the machine.
func Read() Stats {
	return Stats{CPU: readCPU(), Mem: readMem(), Disk: readDisk()}
}

// cpuMeter turns a load average into a share of the machine. Load is a queue
// length rather than a percentage - it counts what is running and what is
// waiting to - so a machine at 100% here is one whose processors are all busy,
// and it can go past that when work is stacking up behind them.
func cpuMeter(load float64, cores int) Meter {
	if cores < 1 {
		cores = 1
	}
	return Meter{
		Percent: load / float64(cores) * 100,
		Left:    fmt.Sprintf("%.2f", load),
		Known:   true,
	}
}

// usedMeter turns a total and what is free of it into a meter.
func usedMeter(free, total uint64) Meter {
	if total == 0 {
		return Meter{}
	}
	used := total - min(free, total)
	return Meter{
		Percent: float64(used) / float64(total) * 100,
		Left:    shortBytes(free),
		Known:   true,
	}
}

// shortBytes writes a size in the fewest characters that still say which scale
// it is on. Three significant figures at most, since the column is a few cells
// wide and the difference between 4.83G and 4.8G is not one anybody acts on.
func shortBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	suffix := "KMGTP"[exp : exp+1]
	if v >= 10 {
		return fmt.Sprintf("%.0f%s", v, suffix)
	}
	return fmt.Sprintf("%.1f%s", v, suffix)
}
