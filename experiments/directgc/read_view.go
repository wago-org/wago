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
