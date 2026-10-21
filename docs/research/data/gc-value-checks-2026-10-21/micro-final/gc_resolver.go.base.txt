//go:build amd64

package amd64

// gcResolvedObject is a transient raw-address certificate. Unlike collector
// roots, it never survives a call, allocation, or unknown effect. A bounded,
// effect-free if may retain an unchanged entry certificate.
type gcResolvedObject struct {
	valid         bool
	local         int
	typeIndex     uint32
	requiredBytes uint32
	reg           Reg
}

// prepareGCResolvedObject is the central invalidation gate. Apart from proven
// pure void ifs, only
// noncollecting integer operations, local reads, and assignments to other
// locals retain the transient address. Assigning its source local invalidates
// it in setLocal. Calls, other control boundaries, and unknown effects drop it.
func (f *fn) prepareGCResolvedObject(op byte) {
	if !f.gcResolved.valid {
		return
	}
	if f.gcPureIfEndPC != 0 && f.wasmPC > f.gcPureIfEndPC {
		f.gcPureIfEndPC = 0
	}
	if f.gcPureIfEndPC != 0 && f.wasmPC <= f.gcPureIfEndPC && (op == 0x04 || op == 0x05 || op == 0x0b) {
		return
	}
	switch op {
	case 0x1a, 0x20, 0x21, 0x22, 0x41, 0x42, 0x43, 0x44, 0xfb,
		0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
		0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
		0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
		0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a:
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
	f.gcPureIfEndPC = 0
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

// Only borrowed local reads identify the current local value. An owned
// register is a snapshot that can survive assignment to its source local.
func gcLocalProvenance(e *elem) (int, bool) {
	if e == nil || !e.isValue() {
		return 0, false
	}
	switch e.st.kind {
	case stLocalRef, stLocalReg:
		return e.st.index(), true
	}
	return 0, false
}

func markGCLocalProvenance(e *elem, local int) {
	if e != nil && e.isValue() && e.st.kind == stReg && local >= 0 {
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
	f.invalidateGCFreshStructFact()
}
