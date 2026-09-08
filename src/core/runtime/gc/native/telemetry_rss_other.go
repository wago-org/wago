//go:build !linux && !darwin

package gc

// Peak RSS is not exposed by the Go standard library on this target. Product
// benchmark harnesses may fill MemoryDomains.PeakRSSBytes from platform APIs.
func peakRSSBytes() uint64 { return 0 }
