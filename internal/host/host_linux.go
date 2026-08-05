package host

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// readCPU takes the one-minute load average from /proc/loadavg, whose first
// field is exactly that.
func readCPU() Meter {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return Meter{}
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return Meter{}
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return Meter{}
	}
	return cpuMeter(load, runtime.NumCPU())
}

// readMem reads /proc/meminfo.
//
// MemAvailable is the number to use rather than MemFree: Linux spends every
// spare page on cache, so MemFree on a machine that has been up a while reads
// as almost nothing while most of it could be handed to a program that asked.
// MemAvailable is the kernel's own estimate of that, which is the question
// anyone looking at this row is asking.
func readMem() Meter {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Meter{}
	}
	var total, available uint64
	for _, line := range strings.Split(string(data), "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch key {
		case "MemTotal":
			total = kilobytes(rest)
		case "MemAvailable":
			available = kilobytes(rest)
		}
	}
	if total == 0 || available == 0 {
		return Meter{}
	}
	return usedMeter(available, total)
}

// kilobytes reads "  16333184 kB" as bytes.
func kilobytes(s string) uint64 {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return n * 1024
}

// readDisk asks the filesystem how much of it is left.
//
// The percentage is worked out the way df works it out, against what is used
// plus what an ordinary program could still write, rather than against the
// whole device. The difference is the blocks held back for root, and counting
// those would put berth's figure a few points under the one every other tool
// on the machine reports.
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
