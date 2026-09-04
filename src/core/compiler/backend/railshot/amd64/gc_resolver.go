//go:build amd64

package amd64

// gcResolvedObject is a transient raw-address certificate. Unlike collector
// roots, it never survives a call, allocation, control boundary, or unknown
// effect.
type gcResolvedObject struct {
	valid         bool
	local         int
	typeIndex     uint32
	requiredBytes uint32
	reg           Reg
}

// prepareGCResolvedObject is the central straight-line invalidation gate. Only
// leaves needed to carry an unchanged local into another GC operation and the
// GC prefix itself retain the transient address. Every other opcode drops it.
func (f *fn) prepareGCResolvedObject(op byte) {
	if !f.gcResolved.valid {
		return
	}
	switch op {
	case 0x1a, 0x20, 0x41, 0x42, 0x43, 0x44, 0xfb:
		return
	default:
		f.invalidateGCResolvedObject()
	}
}

func (f *fn) prepareGCResolvedFB(sub uint32) {
	if !f.gcResolved.valid {
		return
	}
	switch sub {
	case 2, 3, 4, 5, 11, 12, 13, 14, 15:
		return
	default:
		f.invalidateGCResolvedObject()
	}
}

func (f *fn) invalidateGCResolvedObject() {
	if !f.gcResolved.valid {
		return
	}
	f.pinned = f.pinned.remove(f.gcResolved.reg)
	f.gcResolved = gcResolvedObject{}
}

func (f *fn) gcResolvedRegister() Reg {
	block := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, reg := range [...]Reg{RBP, R12, R13, R14} {
		if f.regUser[reg] == nil && !block.has(reg) {
			return reg
		}
	}
	return regNone
}

func gcLocalProvenance(e *elem) (int, bool) {
	if e == nil || e.kind != ekValue {
		return 0, false
	}
	switch e.st.kind {
	case stLocalRef, stLocalReg:
		return e.st.index(), true
	case stReg:
		if e.st.slot > 0 {
			return e.st.slotIndex() - 1, true
		}
	}
	return 0, false
}

func markGCLocalProvenance(e *elem, local int) {
	if e != nil && e.kind == ekValue && e.st.kind == stReg && local >= 0 {
		e.st.slot = uint32(local + 1)
	}
}

func (f *fn) invalidateGCLoadFactsForLocal(local int) {
	if f.gcResolved.valid && f.gcResolved.local == local {
		f.invalidateGCResolvedObject()
	}
}

func (f *fn) invalidateGCMutableLoadFacts() {
	f.invalidateGCResolvedObject()
}
