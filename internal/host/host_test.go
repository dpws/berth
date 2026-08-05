package host

import (
	"runtime"
	"testing"
)

func TestShortBytes(t *testing.T) {
	cases := []struct {
		n    uint64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{10 * 1024, "10K"},
		{5 * 1024 * 1024 * 1024, "5.0G"},
		{406 * 1024 * 1024 * 1024, "406G"},
		{3 * 1024 * 1024 * 1024 * 1024, "3.0T"},
	}
	for _, c := range cases {
		if got := shortBytes(c.n); got != c.want {
			t.Errorf("shortBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// Load is a queue length rather than a share of anything, so it only becomes a
// percentage once it is put against the number of processors there are to run
// it. The same 4.0 is a busy machine on four cores and a drowning one on one.
func TestCPUMeterIsLoadPerCore(t *testing.T) {
	if got := cpuMeter(2, 4); got.Percent != 50 {
		t.Errorf("2.0 on four cores = %.0f%%, want 50", got.Percent)
	}
	if got := cpuMeter(2, 1); got.Percent != 200 {
		t.Errorf("2.0 on one core = %.0f%%, want 200 - work is stacking up", got.Percent)
	}
	// A machine that will not say how many processors it has is one, not zero:
	// dividing by that would report every load as infinite.
	if got := cpuMeter(1, 0); got.Percent != 100 {
		t.Errorf("with no core count = %.0f%%, want it treated as one", got.Percent)
	}
	// The figure beside the meter is the load itself, which is what makes the
	// percentage checkable against uptime.
	if got := cpuMeter(7.755, 4).Left; got != "7.75" && got != "7.76" {
		t.Errorf("Left = %q, want the load average", got)
	}
}

func TestUsedMeter(t *testing.T) {
	const gb = 1024 * 1024 * 1024
	got := usedMeter(2*gb, 8*gb)
	if got.Percent != 75 {
		t.Errorf("Percent = %.0f, want 75", got.Percent)
	}
	// What is left, not what is gone: the percentage already says how much is
	// gone, and the number worth acting on is the headroom.
	if got.Left != "2.0G" {
		t.Errorf("Left = %q, want what remains", got.Left)
	}
	if !got.Known {
		t.Error("a reading that came back was reported as unknown")
	}

	// Nothing to divide by is not a machine with no memory used.
	if usedMeter(0, 0).Known {
		t.Error("a total of zero was reported as a known reading")
	}
	// More free than there is: clamped rather than turned into a negative bar.
	if got := usedMeter(9*gb, 8*gb); got.Percent != 0 {
		t.Errorf("Percent = %.0f, want 0 rather than a negative", got.Percent)
	}
}

// The numbers have to come from the machine berth is on, not from a struct
// berth filled in. This checks they are there and sane rather than checking
// values, which no test can know.
func TestReadTakesARealReading(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("no reader for %s", runtime.GOOS)
	}
	s := Read()
	if s.Empty() {
		t.Fatal("nothing at all was read from this machine")
	}
	for _, c := range []struct {
		name  string
		meter Meter
	}{{"cpu", s.CPU}, {"mem", s.Mem}, {"disk", s.Disk}} {
		if !c.meter.Known {
			t.Errorf("%s was not read", c.name)
			continue
		}
		if c.meter.Percent < 0 {
			t.Errorf("%s is %.1f%%, want nothing negative", c.name, c.meter.Percent)
		}
		if c.meter.Left == "" {
			t.Errorf("%s says nothing about what is left", c.name)
		}
	}
	// Memory and disk are shares of a fixed total, so those cannot pass 100.
	// Load can, and is the one meter that is allowed to.
	if s.Mem.Percent > 100 || s.Disk.Percent > 100 {
		t.Errorf("mem %.1f%% disk %.1f%%, want both within their totals",
			s.Mem.Percent, s.Disk.Percent)
	}
}
