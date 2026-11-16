// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package directgc

import "unsafe"

// ReadView exposes a trusted host-only snapshot for admitted native reads. These
// integer addresses do not retain the collector: its owner must keep it alive.
// The arena and start table have fixed backing; ranges change after allocation
// and collection. Access is serialized; no cached object address survives GC.
// Start bytes are 0/1, indexed by offset/8. Consumers must also require nonzero
// 16-byte-aligned offsets in the current nursery or active-old occupied range.
// All pointers are read-only. Mutating them bypasses collector invariants.
// Dirty bytes are an additional READ-ONLY certificate: one on a valid old
// object means its parent is registered in the remembered queue or overflow
// scan. Native writers may reuse that registration but may NEVER set this byte
// directly. A clean old parent requires the checked Remember helper first.
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

// AllocationWords exposes the authoritative nursery cursor for a trusted native
// allocator. The owner must retain this Collector and serialize all accesses.
// A writer must initialize the complete object and its start byte before
// advancing Bump, must not cross End, and must not call or safepoint mid-publish.
// This is not a reservation API and must never be exposed to guest code.
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
