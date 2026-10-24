//go:build amd64

package amd64

import (
	"fmt"
	"math"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// This plan owns one complete prefix, not a general call/control exemption:
// final cast/get; selected integer comparison inline; if { trap } else {
// same-local final cast/get }. No native call or moving-GC point is emitted
// while its one-use address is live. The enclosing if continues normally after
// the second load, with the address already released.
type gcTrappingGuardPlan struct {
	firstEnd, elsePC, secondPC, end     int
	local                               int
	firstOffset, secondOffset, required uint32
	secondScalar                        directGCScalar
	secondRef, secondSigned             bool
	cache                               Reg
}

func (f *fn) gcGuardComparison(idx uint32) bool {
	if int(idx) < f.importedFunctionCount() {
		return false
	}
	t := f.selectedInlineTarget(idx)
	if t == nil || t.hasCtrl || t.recursive() || t.paramCount() != 2 || t.localTypeEnd-t.typeStart != 2 || len(t.body) > 16 {
		return false
	}
	ft, ok := f.functionSignature(idx)
	if !ok || len(ft.Params) != 2 || ft.Params[0] != wasm.I32 || ft.Params[1] != wasm.I32 || len(ft.Results) != 1 || ft.Results[0] != wasm.I32 {
		return false
	}
	r := wasm.ReaderFrom(t.body)
	for i := uint32(0); i < 2; i++ {
		op, e := r.Byte()
		if e != nil || op != 0x20 {
			return false
		}
		n, e := r.U32()
		if e != nil || n != i {
			return false
		}
	}
	op, e := r.Byte()
	if e != nil || op < 0x46 || op > 0x4f {
		return false
	}
	op, e = r.Byte()
	return e == nil && op == 0x0b && r.BytesLeft() == 0
}

// Inspect indexed block signatures before ordinary blockType can allocate a
// frame-type slice. A rejected large signature must not expand this scan budget.
func (f *fn) gcGuardBlockType(r *wasm.Reader) bool {
	b, ok := r.Peek()
	if !ok {
		return false
	}
	if b == 0x40 || isValByte(b) || b == 0x63 || b == 0x64 {
		_, _, _, _, err := f.blockType(r)
		return err == nil
	}
	idx, err := r.I64()
	if err != nil || idx < 0 || idx > math.MaxUint32 {
		return false
	}
	ft, ok := f.m.TypeFunc(uint32(idx))
	return ok && len(ft.Params) == 0 && len(ft.Results) <= 1
}

func (f *fn) planGCTrappingGuard(typeIndex uint32, reader *wasm.Reader) (p gcTrappingGuardPlan, ok bool) {
	if f.unreachable || f.inlineDepth != 0 || f.localBase != 0 || f.moduleEH || len(f.customInstructions) != 0 || f.intervalControl || f.vectorRegion.enabled || f.gcGuardSites >= 16 {
		return p, false
	}
	local, hasLocal := gcLocalProvenance(f.s.back())
	if !hasLocal {
		return p, false
	}
	st, final := f.stagedStructType(typeIndex)
	if !final || !st.Final {
		return p, false
	}
	cache := f.gcResolvedRegister()
	if cache == regNone {
		return p, false
	}
	f.gcGuardSites++
	rr := *reader
	window, e := rr.Bytes(min(64, rr.BytesLeft()))
	if e != nil {
		return p, false
	}
	r := wasm.ReaderFrom(window)
	byteIs := func(want byte) bool { b, e := r.Byte(); return e == nil && b == want }
	readField := func() (sub, field uint32, ok bool) {
		if !byteIs(0xfb) {
			return 0, 0, false
		}
		sub, e := r.U32()
		if e != nil || sub < 2 || sub > 4 {
			return 0, 0, false
		}
		typ, e := r.U32()
		if e != nil || typ != typeIndex {
			return 0, 0, false
		}
		field, e = r.U32()
		return sub, field, e == nil
	}
	sub, field, yes := readField()
	if !yes || sub != 2 {
		return p, false
	}
	first, _, yes := f.gcStructFieldLayout(typeIndex, field)
	if !yes || first.Size != 4 {
		return p, false
	}
	fld, yes := f.stagedStructField(typeIndex, field)
	if !yes || fld.Storage().Packed() || fld.Storage().Val() != wasm.I32 {
		return p, false
	}
	base := reader.Offset()
	p.firstEnd = base + r.Offset()
	if !byteIs(0x10) {
		return p, false
	}
	idx, e := r.U32()
	if e != nil || !f.gcGuardComparison(idx) {
		return p, false
	}
	if !byteIs(0x04) {
		return p, false
	}
	if !f.gcGuardBlockType(&r) {
		return p, false
	}
	if !byteIs(0x00) {
		return p, false
	}
	// Eight fixed operations include both casts/loads, call, if, else and local
	// read. At most 24 trap/nop operations keep the whole prefix within 32.
	ops := 9
	for {
		p.elsePC = base + r.Offset()
		op, e := r.Byte()
		if e != nil {
			return p, false
		}
		if op == 0x05 {
			break
		}
		if (op != 0x00 && op != 0x01) || ops >= 32 {
			return p, false
		}
		ops++
	}
	if !byteIs(0x20) {
		return p, false
	}
	again, e := r.U32()
	if e != nil || int(again) != local {
		return p, false
	}
	p.secondPC = base + r.Offset()
	if !byteIs(0xfb) {
		return p, false
	}
	cast, e := r.U32()
	if e != nil || (cast != 22 && cast != 23) {
		return p, false
	}
	heap, exact, e := readRefHeapTypeImmediate(&r)
	if e != nil || exact || heap != int64(typeIndex) {
		return p, false
	}
	sub, field, yes = readField()
	if !yes {
		return p, false
	}
	second, _, yes := f.gcStructFieldLayout(typeIndex, field)
	if !yes {
		return p, false
	}
	fld, yes = f.stagedStructField(typeIndex, field)
	if !yes || (sub == 2) == fld.Storage().Packed() {
		return p, false
	}
	scalar, direct := directGCScalarStorage(fld.Storage())
	ref := second.CollectorRef && second.Size == 4
	if !ref && (!direct || scalar.typ.isFloat() || scalar.typ == mtV128 || second.Size != uint32(scalar.size)) {
		return p, false
	}
	firstBytes := uint64(gc.PayloadOffset) + uint64(first.Offset) + 4
	secondBytes := uint64(gc.PayloadOffset) + uint64(second.Offset) + uint64(second.Size)
	if firstBytes > math.MaxInt32 || secondBytes > firstBytes {
		return p, false
	}
	p.local, p.cache, p.required = local, cache, uint32(firstBytes)
	p.firstOffset, p.secondOffset = first.Offset, second.Offset
	p.secondScalar, p.secondRef, p.secondSigned = scalar, ref, sub == 3
	p.end = base + r.Offset()
	return p, true
}

func (f *fn) tryGCTrappingGuard(typeIndex uint32, nullable bool, r *wasm.Reader) (bool, error) {
	p, ok := f.planGCTrappingGuard(typeIndex, r)
	if !ok {
		return false, nil
	}
	// Ordinary first-read materialization flushes the stack and clears transient
	// pins. Establish the certificate and its lease together after that flush
	// and the full resolver have returned, before any later instruction.
	if err := f.emitNativeFinalCastStructFieldAddress(typeIndex, p.firstOffset, 4, nullable); err != nil {
		return true, err
	}
	if f.regUser[p.cache] != nil || f.pinned.union(f.pinnedLocalMask).union(f.reserved).has(p.cache) {
		return true, fmt.Errorf("amd64: trapping guard register %d is no longer free after first resolution", p.cache)
	}
	f.a.MovReg64(p.cache, RAX)
	f.gcResolved = gcResolvedObject{valid: true, local: p.local, typeIndex: typeIndex, requiredBytes: p.required, reg: p.cache}
	f.pinned = f.pinned.add(p.cache)
	defer f.invalidateGCResolvedObject()
	f.a.Load32(RAX, p.cache, int32(gc.PayloadOffset+p.firstOffset))
	f.pushReg(RAX, mtI32)
	f.stats.peep("final-cast-scalar-get-fuse")
	if err := r.JumpTo(p.firstEnd); err != nil {
		return true, err
	}
	oldPC := f.wasmPC
	defer func() { f.wasmPC = oldPC }()
	// The normal source checker excludes GC bodies. Profile each emitted source
	// operation and keep reader PCs exact; callOp consumes its source root mask
	// exactly once through the same selected-inline policy used by the plan.
	for r.Offset() <= p.elsePC {
		f.wasmPC = f.tracePCBase + uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			return true, err
		}
		var previous profileOrigin
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			previous = f.enterProfileInstruction()
		}
		switch op {
		case 0x10:
			f.prepareStoreForward(op)
			f.invalidateGCFreshStructFact()
			err = f.callOp(r)
		case 0x04:
			err = f.opBlock(r, op)
		case 0x00:
			if !f.unreachable {
				f.flush()
				f.trapAlways(trapUnreachable)
				f.unreachable = true
			}
		case 0x01:
		case 0x05:
			err = f.opElse()
		default:
			err = fmt.Errorf("amd64: trapping guard plan diverged at opcode %#x", op)
		}
		if profileEnabled && f.stats != nil && f.stats.RecordSources {
			f.switchProfileOrigin(previous)
		}
		if err != nil {
			return true, err
		}
		if !f.gcResolved.valid || f.gcResolved.reg != p.cache || !f.pinned.has(p.cache) {
			return true, fmt.Errorf("amd64: trapping guard lost address register %d after opcode %#x at %d (valid=%t current=%d pinned=%#x)", p.cache, op, r.Offset(), f.gcResolved.valid, f.gcResolved.reg, f.pinned)
		}
	}
	// The else edge is dominated by the first successful field read. No source
	// local write or collecting instruction occurs in this complete prefix.
	// The checked extent already covers this load; the repeated casts cannot trap.
	f.wasmPC = f.tracePCBase + uint32(p.secondPC)
	var previous profileOrigin
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		previous = f.enterProfileInstruction()
	}
	out := f.allocReg(maskOf(p.cache))
	if p.secondRef {
		f.a.Load32(out, p.cache, int32(gc.PayloadOffset+p.secondOffset))
		f.setStackGCRoot(f.pushReg(out, mtI64), true)
	} else {
		scalar := p.secondScalar
		f.a.LoadIdx(out, p.cache, RSP, int32(gc.PayloadOffset+p.secondOffset), scalar.size, p.secondSigned, scalar.typ == mtI64)
		f.pushReg(out, scalar.typ)
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		f.switchProfileOrigin(previous)
	}
	if err := r.JumpTo(p.end); err != nil {
		return true, err
	}
	f.stats.peep("gc-trapping-guard-reuse")
	switch p.cache {
	case RBP:
		f.stats.peep("gc-trapping-guard-rbp")
	case R12:
		f.stats.peep("gc-trapping-guard-r12")
	case R13:
		f.stats.peep("gc-trapping-guard-r13")
	case R14:
		f.stats.peep("gc-trapping-guard-r14")
	}
	return true, nil
}
