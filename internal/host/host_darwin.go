package host

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// probeTimeout bounds a reading. macOS has no /proc, so the numbers come from
// small programs, and berth would rather show a blank row than sit waiting on
// one that is not answering.
const probeTimeout = 2 * time.Second

// readCPU takes the one-minute load average, which sysctl prints as
// "{ 1.85 1.95 2.01 }".
func readCPU() Meter {
	out, err := sysctl("vm.loadavg")
	if err != nil {
		return Meter{}
	}
	fields := strings.Fields(strings.Trim(out, "{} "))
	if len(fields) == 0 {
		return Meter{}
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return Meter{}
	}
	return cpuMeter(load, runtime.NumCPU())
}

// readMem adds up the pages vm_stat calls free.
//
// Free alone is not the answer, the same way MemFree is not on Linux: macOS
// keeps inactive and speculative pages around as cache and hands them back the
// moment something asks. Counting those as gone would report a machine with
// room to spare as nearly full. Purgeable pages go with them - they are cache
// the system is free to drop.
func readMem() Meter {
	total, err := sysctlUint("hw.memsize")
	if err != nil || total == 0 {
		return Meter{}
	}
	out, err := run("vm_stat")
	if err != nil {
		return Meter{}
	}

	pageSize := uint64(4096)
	if _, rest, ok := strings.Cut(out, "page size of "); ok {
		if n, err := strconv.ParseUint(strings.Fields(rest)[0], 10, 64); err == nil && n > 0 {
			pageSize = n
		}
	}

	var free uint64
	for _, line := range strings.Split(out, "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Pages free", "Pages inactive", "Pages speculative", "Pages purgeable":
			n, err := strconv.ParseUint(strings.Trim(strings.TrimSpace(rest), "."), 10, 64)
			if err != nil {
				continue
			}
			free += n * pageSize
		}
	}
	if free == 0 {
		return Meter{}
	}
	return usedMeter(free, total)
}

// readDisk asks the filesystem how much of it is left. The percentage is
// worked out the way df works it out - see the Linux side for why.
func readDisk() Meter {
	var st syscall.Statfs_t
	if err := syscall.Statfs(diskPath, &st); err != nil {
		return Meter{}
	}
	bsize := uint64(st.Bsize)
	used := (st.Blocks - st.Bfree) * bsize
	avail := st.Bavail * bsize
	return usedMeter(avail, used+avail)
}

func sysctl(name string) (string, error) { return run("sysctl", "-n", name) }

func sysctlUint(name string) (uint64, error) {
	out, err := sysctl(name)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(out), 10, 64)
}

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
