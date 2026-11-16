package compactgc

import "unsafe"

// ReadView is a read-only native snapshot. Integer addresses do not retain this
// collector. Owners must serialize execution and retain the heap. Backing never
// moves; occupied ranges and start bytes change on allocation/collection. Every
// reference must be nonzero, aligned to16, a start byte at offset/8, and inside
// an occupied region. No derived object address may survive a helper call.
type ReadView struct {
	Arena                                        uintptr
	ArenaBytes                                   uint32
	Starts                                       uintptr
	StartCount                                   uint32
	NurseryStart, NurseryBump, OldStart, OldBump uint32
}

func (c *Collector) ReadView() ReadView {
	if c == nil || len(c.arena) == 0 {
		return ReadView{}
	}
	return ReadView{Arena: uintptr(unsafe.Pointer(&c.arena[0])), ArenaBytes: uint32(len(c.arena)), Starts: uintptr(unsafe.Pointer(&c.starts[0])), StartCount: uint32(len(c.starts)), NurseryStart: c.nurseryStart, NurseryBump: c.nurseryBump, OldStart: c.oldStart[c.active], OldBump: c.oldBump}
}
