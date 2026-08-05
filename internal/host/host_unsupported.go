//go:build !linux && !darwin

package host

// Everywhere else the machine keeps its accounting somewhere berth has not
// been taught to look. Nothing is reported rather than guessed, and the block
// leaves itself out.
func readCPU() Meter  { return Meter{} }
func readMem() Meter  { return Meter{} }
func readDisk() Meter { return Meter{} }
