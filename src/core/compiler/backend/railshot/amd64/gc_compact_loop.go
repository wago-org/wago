//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Reserve only a lazy certificate. No receiver or metadata is read at entry,
// so zero-trip loops and earlier trap ordering are unchanged.
func (f *fn) prepareCompactLoop(reader *wasm.Reader) {
	if !gcCompactLoop || f.directLoop.endPC != 0 {
		return
	}
	available := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, r := range gpAlloc {
		if !blocked.has(r) {
			available++
		}
	}
	if available < 7 {
		return
	}
	p, ok := f.planGCLoopResolverMode(reader, true)
	if !ok || p.accesses == 0 || p.accesses > 255 {
		return
	}
	if _, _, valid := f.compactScalarArrayLayout(p.typeIndex); !valid {
		return
	}
	obj := f.directLoopRegister()
	if obj == regNone {
		return
	}
	f.pinned = f.pinned.add(obj)
	length := f.directLoopRegister()
	if length == regNone {
		f.pinned = f.pinned.remove(obj)
		return
	}
	f.pinned = f.pinned.add(length)
	f.a.MovImm32(obj, 0)
	f.a.MovImm32(length, 0)
	f.directLoop = directLoopLease{p.endPC, uint32(p.local), p.typeIndex, obj, length, uint8(p.accesses), loopLeaseCompactPending}
	f.stats.peep("gc-compact-loop-plan")
}

// Only the first static access emits the lazy check. The bounded straight-line
// proof makes it dominate every later access and every backedge. No safepoint,
// receiver rebind, header mutation or unknown instruction can occur in the lease.
func (f *fn) useCompactLoop(object *elem, t, size, payload uint32, local int) (Reg, func()) {
	lease := &f.directLoop
	if !gcCompactLoop || lease.remaining == 0 || f.wasmPC >= lease.endPC || t != lease.typeIndex || (local >= 0 && uint32(local) != lease.local) || (lease.kind != loopLeaseCompactPending && lease.kind != loopLeaseCompactActive) {
		panic("amd64: invalid compact loop certificate")
	}
	lease.remaining--
	if lease.kind == loopLeaseCompactPending {
		f.a.TestSelf(lease.object, true)
		ready := f.a.JccPlaceholder(condNE)
		obj, done := f.emitCheckedCompactOffset(object, t, 8, 0, payload, size)
		f.a.MovReg64(lease.object, obj)
		f.a.Load32(lease.length, obj, 4)
		done()
		f.a.PatchRel32(ready, f.a.Len())
		lease.kind = loopLeaseCompactActive
		f.stats.peep("gc-compact-loop-resolve")
	} else {
		ref := f.materialize(object)
		f.release(ref)
	}
	f.stats.peep("gc-compact-loop-reuse")
	return lease.object, func() {}
}
