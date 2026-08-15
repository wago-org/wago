//go:build arm64

package arm64

// gcResolvedObject is a transient certificate for one exact object address.
// The compact local remains authoritative and rooted; this raw address never
// crosses an operation that could mutate the local or collector backing.
type gcResolvedObject struct {
	valid       bool
	local       int
	typeIndex   uint32
	required    uint32
	reg         Reg
	arrayExtent bool
}

func gcLocalProvenance(e *elem) (int, bool) {
	if e == nil || e.kind != ekValue {
		return 0, false
	}
	switch e.st.kind {
	case stLocalRef, stLocalReg:
		return e.st.idx, true
	case stReg:
		if e.st.slot > 0 {
			return e.st.slot - 1, true
		}
	}
	return 0, false
}

// prepareGCResolvedObject retains the certificate only through the narrow
// opcode envelope needed for repeated struct reads. The GC subopcode gate below
// performs the second half of the check.
func (f *fn) prepareGCResolvedObject(op byte) {
	if !f.gcResolved.valid {
		return
	}
	switch op {
	case 0x1a, 0x20, 0x41, 0x42, 0x43, 0x44, 0xd0, 0xfb: // drop, local.get, constants, ref.null, GC prefix
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
	case 2, 3, 4, 5, // struct.get, struct.get_s, struct.get_u, struct.set
		11, 12, 13, // array.get, array.get_s, array.get_u
		14,     // array.set
		22, 23, // ref.cast, ref.cast_null; a helper fallback flushes the cache
		28: // ref.i31 is pure, nontrapping, and cannot move collector backing
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
