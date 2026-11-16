package compactgc

import "unsafe"

// ReadView is a read-only native snapshot. Integer addresses do not retain this
// collector. Owners must serialize execution and retain the heap. Backing never
// moves; occupied ranges and start bytes change on allocation/collection. Every
// reference must be nonzero, aligned to16, a start byte at offset/8, and inside
// an occupied region. No derived object address may survive a helper call.
// Dirty is READ-ONLY: dirty1 on a valid old parent certifies remembered queue
// or overflow registration. Native code may reuse it but must never set it.
// A clean old parent requires the complete helper store/barrier before mutation.
type ReadView struct {
	Arena                                        uintptr
	ArenaBytes                                   uint32
	Starts                                       uintptr
	StartCount                                   uint32
	Dirty                                        uintptr
	DirtyCount                                   uint32
	NurseryStart, NurseryBump, OldStart, OldBump uint32
}

func (c *Collector) ReadView() ReadView {
	if c == nil || len(c.arena) == 0 {
		return ReadView{}
	}
	return ReadView{Arena: uintptr(unsafe.Pointer(&c.arena[0])), ArenaBytes: uint32(len(c.arena)), Starts: uintptr(unsafe.Pointer(&c.starts[0])), StartCount: uint32(len(c.starts)), Dirty: uintptr(unsafe.Pointer(&c.dirty[0])), DirtyCount: uint32(len(c.dirty)), NurseryStart: c.nurseryStart, NurseryBump: c.nurseryBump, OldStart: c.oldStart[c.active], OldBump: c.oldBump}
}

// AllocationWords exposes the authoritative Layout16 nursery cursor for admitted
// native construction. The caller retains/serializes this collector. Before any
// write it checks capacity and all guards, then initializes header, complete
// payload and padding, publishes the start byte, and advances both this cursor
// and the native view's occupied bound. No call/trap/safepoint during publication.
// Allocation accounting is deferred to retirement/Stats; no counter is exposed.
type AllocationWords struct {
	Bump *uint32
	End  uint32
}

func (c *Collector) AllocationWords() AllocationWords {
	if c == nil || len(c.arena) == 0 {
		return AllocationWords{}
	}
	return AllocationWords{Bump: &c.nurseryBump, End: c.nurseryEnd}
}
