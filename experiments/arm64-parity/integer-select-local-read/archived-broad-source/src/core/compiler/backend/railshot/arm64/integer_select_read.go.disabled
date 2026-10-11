//go:build arm64

package arm64

import "os"

var integerSelectReadEnabled = os.Getenv("WAGO_ARM64_EXPERIMENT_INTEGER_SELECT_READ") == "1"
var integerSelectReadOrdinaryEnabled = os.Getenv("WAGO_ARM64_EXPERIMENT_INTEGER_SELECT_LOCAL_ONLY") != "1"

// Unlike popValue, these operands remain on the stack until all reads are
// protected. Regional homes are leases: erasing a borrowed reference before
// pinning its physical register would let another operand evict its value.
func (f *fn) emitIntegerSelectLocalRead(a, b, cond *elem, dest Reg, wide bool) {
	saved := f.pinned
	f.pinned = f.pinned.add(dest)
	c, ownC := f.selectSinkRead(cond)
	f.pinned = f.pinned.add(c)
	br, ownB := f.selectSinkRead(b)
	f.pinned = f.pinned.add(br)
	ar, ownA := f.selectSinkRead(a)
	f.pinned = f.pinned.add(ar)
	wide = a.st.typ.is64() || b.st.typ.is64()
	f.a.CmpImm32(c, 0)
	f.a.Csel(dest, br, ar, condE, wide)
	f.pinned = saved
	if ownC {
		f.release(c)
	}
	if ownB {
		f.release(br)
	}
	if ownA {
		f.release(ar)
	}
	f.erase(cond)
	f.erase(b)
	f.erase(a)
	if !ownC || !ownB || !ownA {
		f.stats.peep("integer-select-read")
	}
}

// Read-only integer select operands need a separately owned result. Reuse a
// consumed owned source when available; otherwise allocate while every borrowed
// operand is protected. Flags are produced only after result allocation.
func (f *fn) tryIntegerSelectRead(cond *elem, flags bool) bool {
	b := f.s.prev(f.s.baseOfValentBlock(cond))
	if b == f.s.head {
		return false
	}
	a := f.s.prev(f.s.baseOfValentBlock(b))
	if a == f.s.head {
		return false
	}
	at, bt := rootMachineType(a), rootMachineType(b)
	if at.isFloat() || at.isV128() || bt.isFloat() || bt.isV128() {
		return false
	}
	gcRoot := (a.elemKind() == ekValue && a.st.hasGCRoot()) || (b.elemKind() == ekValue && b.st.hasGCRoot())
	saved := f.pinned
	c, ownC := regNone, false
	if !flags {
		c, ownC = f.selectSinkRead(cond)
		f.pinned = f.pinned.add(c)
	}
	ar, br := regNone, regNone
	ownA, ownB := false, false
	if flags {
		ar, ownA = f.selectSinkRead(a)
		f.pinned = f.pinned.add(ar)
		br, ownB = f.selectSinkRead(b)
		f.pinned = f.pinned.add(br)
	} else {
		br, ownB = f.selectSinkRead(b)
		f.pinned = f.pinned.add(br)
		ar, ownA = f.selectSinkRead(a)
		f.pinned = f.pinned.add(ar)
	}
	// Deferred comparisons carry their input width until materialization;
	// select uses the concrete result width (always I32 for comparisons).
	wide := a.st.typ.is64() || b.st.typ.is64()
	result := regNone
	switch {
	case ownA:
		result = ar
	case ownB:
		result = br
	case ownC:
		result = c
	default:
		result = f.allocReg(0)
	}
	f.pinned = f.pinned.add(result)
	cc := condE
	if flags {
		cc = invertCond(f.condenseToFlags(cond))
	} else {
		f.a.CmpImm32(c, 0)
	}
	f.a.Csel(result, br, ar, cc, wide)
	f.pinned = saved
	if ownC && c != result {
		f.release(c)
	}
	if ownB && br != result {
		f.release(br)
	}
	if ownA && ar != result {
		f.release(ar)
	}
	if !flags {
		f.erase(cond)
	}
	f.erase(b)
	f.erase(a)
	f.pushReg(result, mtI32OrWide(wide)).st.setGCRoot(gcRoot)
	if !flags && !ownC || !ownB || !ownA {
		f.stats.peep("integer-select-read")
	}
	if flags {
		f.stats.peep("select-flags")
	} else {
		f.stats.peep("select-cmov")
	}
	return true
}
