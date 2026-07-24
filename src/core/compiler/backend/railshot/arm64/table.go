//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const (
	offFuncRefDescPtr    = abi.FuncRefDescPtrOffset
	offPassiveElemPtr    = abi.PassiveElemPtrOffset
	offTableDirPtr       = abi.TableDirPtrOffset
	offImportDispatchPtr = abi.ImportDispatchPtrOffset
	offGlobalsPtr        = abi.GlobalsPtrOffset
)

func (f *fn) loadTableDescriptor(dst Reg, tableIdx uint32) {
	if tableIdx == 0 {
		f.ld64(dst, linMemReg, -int32(offTablePtr))
		return
	}
	f.ld64(dst, linMemReg, -int32(offTableDirPtr))
	f.ld64(dst, dst, int32(tableIdx*8))
}

// The table bulk operations use a fixed set of scratch registers, mirroring the
// amd64 backend's use of the x86 string/scratch registers (RDI/RSI/RCX/RAX/RDX/R8).
// AArch64 has no `rep movsb`, so the copy/fill loops are emitted explicitly (§4f)
// over these registers; they are the caller-saved / call-scratch registers the
// `usesBulkMem` hint already reserves out of the pinned-local pool, exactly as
// memory.copy/fill do:
//
//	amd64 RDI (dst)   -> X9
//	amd64 RSI (src)   -> X10
//	amd64 RCX (count) -> X11
//	amd64 RAX (temp)  -> X12
//	amd64 RDX (temp)  -> X13
//	amd64 R8  (base)  -> X14

func readSingleTableIndex(r *wasm.Reader) (uint32, error) {
	return r.U32()
}

func readTablePairIndexes(r *wasm.Reader) (uint32, uint32, error) {
	idx0, err := r.U32()
	if err != nil {
		return 0, 0, err
	}
	idx1, err := r.U32()
	if err != nil {
		return 0, 0, err
	}
	return idx0, idx1, nil
}

// tableEntryAddr computes dst = tbl.entries + dst*TableEntryBytes. x86 folded the
// scale/base/disp into an LEA; on arm64 there is no memory operand or LEA, so it
// lowers to a shift + add + displacement add (§4e).
func (f *fn) tableEntryAddr(dst, tbl Reg) {
	f.shiftImm(shLSL, dst, 5, true) // dst *= 32 (TableEntryBytes); was ShiftImm(4,…)
	f.a.Add64(dst, dst, tbl)        // dst += tbl (3-operand in-place)
	f.leaDisp(dst, dst, 8, true)    // dst += 8 (skip the header to the entries array)
}

func (f *fn) tableIsExternref(tableIdx uint32) bool {
	tt, ok := f.m.TableType(tableIdx)
	return ok && wasm.EqualValType(wasm.RefVal(tt.Ref), wasm.ExternRef)
}

func (f *fn) typedTableEntryAddr(dst, tbl Reg, tableIdx uint32) {
	shift := byte(5)
	if f.tableIsExternref(tableIdx) {
		shift = 3
	}
	f.shiftImm(shLSL, dst, shift, true)
	f.a.Add64(dst, dst, tbl)
	f.leaDisp(dst, dst, 8, true)
}

func entryStrideShift(externref bool) byte {
	if externref {
		return 3
	}
	return 5
}

func (f *fn) entryArrayAddr(dst, base Reg, externref bool) {
	f.shiftImm(shLSL, dst, entryStrideShift(externref), true)
	f.a.Add64(dst, dst, base)
}

func (f *fn) trapTableUnlessLE(value, limit Reg) {
	f.cmpRR(value, limit, true)
	f.trapIf(condA, trapTableOOB)
}

func (f *fn) tableSize(r *wasm.Reader) error {
	tableIdx, err := readSingleTableIndex(r)
	if err != nil {
		return err
	}
	tbl := f.allocReg(0)
	f.loadTableDescriptor(tbl, tableIdx)
	f.ld32(tbl, tbl, 0)
	f.pushReg(tbl, mtI32)
	return nil
}

func (f *fn) tableInit(r *wasm.Reader) error {
	elemIdx, err := r.U32()
	if err != nil {
		return err
	}
	tableIdx, err := readSingleTableIndex(r)
	if err != nil {
		return err
	}
	f.materializePendingLoads()
	types, argsSlot := f.flushSuffix(3)
	f.ld64(X9, SP, f.spillOff(argsSlot))    // dst table offset
	f.ld64(X10, SP, f.spillOff(argsSlot+1)) // src element offset
	f.ld64(X11, SP, f.spillOff(argsSlot+2)) // n entries

	f.loadTableDescriptor(X14, tableIdx)
	f.ld32(X12, X14, 0)
	f.leaScaled(X13, X9, X11, 0, 0, true)
	f.trapTableUnlessLE(X13, X12)
	externref := f.tableIsExternref(tableIdx)
	f.typedTableEntryAddr(X9, X14, tableIdx)

	disp := int32(elemIdx) * runtime.PassiveElemDescBytes
	f.ld64(X14, linMemReg, -int32(offPassiveElemPtr))
	f.ld32(X12, X14, disp+8)
	f.leaScaled(X13, X10, X11, 0, 0, true)
	f.trapTableUnlessLE(X13, X12)
	f.ld64(X14, X14, disp)
	f.entryArrayAddr(X10, X14, externref)
	f.shiftImm(shLSL, X11, entryStrideShift(externref), true)
	f.copyFwdLoop(X9, X10, X11) // was RepMovsb — forward byte copy (§4f)
	f.dropFlushedSuffix(types, 3)
	return nil
}

func (f *fn) elemDrop(r *wasm.Reader) error {
	elemIdx, err := r.U32()
	if err != nil {
		return err
	}
	f.materializePendingLoads()
	f.flush()
	disp := int32(elemIdx)*runtime.PassiveElemDescBytes + 8
	f.ld64(X14, linMemReg, -int32(offPassiveElemPtr))
	f.st32(X14, disp, ZR) // store WZR — arm64 has no store-immediate (was StoreImm32Mem …,0)
	return nil
}

func (f *fn) tableCopy(r *wasm.Reader) error {
	dstTableIdx, srcTableIdx, err := readTablePairIndexes(r)
	if err != nil {
		return err
	}
	f.materializePendingLoads()
	types, argsSlot := f.flushSuffix(3)
	f.ld64(X9, SP, f.spillOff(argsSlot))
	f.ld64(X10, SP, f.spillOff(argsSlot+1))
	f.ld64(X11, SP, f.spillOff(argsSlot+2))
	f.loadTableDescriptor(X14, dstTableIdx)
	f.ld32(X12, X14, 0)
	f.leaScaled(X13, X9, X11, 0, 0, true)
	f.trapTableUnlessLE(X13, X12)
	f.typedTableEntryAddr(X9, X14, dstTableIdx)
	f.loadTableDescriptor(X14, srcTableIdx)
	f.ld32(X12, X14, 0)
	f.leaScaled(X13, X10, X11, 0, 0, true)
	f.trapTableUnlessLE(X13, X12)
	f.typedTableEntryAddr(X10, X14, srcTableIdx)
	f.shiftImm(shLSL, X11, entryStrideShift(f.tableIsExternref(dstTableIdx)), true)
	f.cmpRR(X9, X10, true)
	fwd := f.a.Bcond(condBE) // dst <= src → a forward copy cannot overwrite unread source
	f.leaScaled(X13, X10, X11, 0, 0, true)
	f.cmpRR(X9, X13, true)
	fwdDisjoint := f.a.Bcond(condAE) // dst >= src+n → disjoint, forward-safe
	// dst ahead of src and overlapping → copy backward. copyBackLoop takes the
	// start pointers and walks from the last byte down, so unlike amd64 (which
	// pre-adjusted RDI/RSI to the last byte for `std; rep movsb`) no LEA-to-last-byte
	// fixup is emitted here.
	f.copyBackLoop(X9, X10, X11)
	done := f.a.Branch() // unconditional (imm26)
	f.a.PatchBranch19(fwd, f.a.Len())
	f.a.PatchBranch19(fwdDisjoint, f.a.Len())
	f.copyFwdLoop(X9, X10, X11) // forward byte copy (was RepMovsb)
	f.a.PatchBranch26(done, f.a.Len())
	f.dropFlushedSuffix(types, 3)
	return nil
}

func (f *fn) tableFill(r *wasm.Reader) error {
	tableIdx, err := readSingleTableIndex(r)
	if err != nil {
		return err
	}
	if f.tableIsExternref(tableIdx) {
		return f.externrefTableFill(tableIdx)
	}
	f.materializePendingLoads()
	types, argsSlot := f.flushSuffix(3)
	valSlot := f.allocSpillSlots(runtime.TableEntryBytes / 8)
	f.ld64(X9, SP, f.spillOff(argsSlot))
	f.ld64(X12, SP, f.spillOff(argsSlot+1))
	f.ld64(X11, SP, f.spillOff(argsSlot+2))
	f.loadTableDescriptor(X14, tableIdx)
	f.ld32(X13, X14, 0)
	f.leaScaled(X9, X9, X11, 0, 0, true)
	f.trapTableUnlessLE(X9, X13)
	f.ld64(X9, SP, f.spillOff(argsSlot))
	f.tableEntryAddr(X9, X14)
	// snapshotFuncrefDescriptor uses the register allocator internally. Keep the
	// fixed destination/count registers live across it so descriptor snapshotting
	// cannot clobber the table.fill loop operands.
	f.pinned = f.pinned.add(X9).add(X11)
	f.snapshotFuncrefDescriptor(X12, valSlot)
	f.fillTableEntries(X9, X11, valSlot)
	f.pinned = f.pinned.remove(X11).remove(X9)
	f.dropFlushedSuffix(types, 3)
	return nil
}

func (f *fn) externrefTableFill(tableIdx uint32) error {
	f.materializePendingLoads()
	types, argsSlot := f.flushSuffix(3)
	f.ld64(X9, SP, f.spillOff(argsSlot))
	f.ld64(X12, SP, f.spillOff(argsSlot+1))
	f.ld64(X11, SP, f.spillOff(argsSlot+2))
	f.loadTableDescriptor(X14, tableIdx)
	f.ld32(X13, X14, 0)
	f.leaScaled(X9, X9, X11, 0, 0, true)
	f.trapTableUnlessLE(X9, X13)
	f.ld64(X9, SP, f.spillOff(argsSlot))
	f.typedTableEntryAddr(X9, X14, tableIdx)
	f.fillExternrefEntries(X9, X11, X12)
	f.dropFlushedSuffix(types, 3)
	return nil
}

func (f *fn) tableGrow(r *wasm.Reader) error {
	tableIdx, err := readSingleTableIndex(r)
	if err != nil {
		return err
	}
	if f.tableIsExternref(tableIdx) {
		return f.externrefTableGrow(tableIdx)
	}
	f.materializePendingLoads()
	f.flush()
	delta := f.materialize(f.popValue())
	f.pinned = f.pinned.add(delta)
	ref := f.materialize(f.popValue())
	f.pinned = f.pinned.add(ref)
	valSlot := f.allocSpillSlots(runtime.TableEntryBytes / 8)
	tbl := f.allocReg(maskOf(delta).add(ref))
	f.loadTableDescriptor(tbl, tableIdx)
	old := f.allocReg(maskOf(delta).add(ref).add(tbl))
	f.ld32(old, tbl, 0)
	nw := f.allocReg(maskOf(delta).add(ref).add(tbl).add(old))
	f.a.MovReg32(nw, old) // zero-extend old into nw (was MovRegReg32)
	// nw = old + delta, checking for 32-bit unsigned overflow. On arm64 the add
	// must be the flag-setting ADDS form, and the carry-out condition is CondCS
	// (carry set) — the opposite of the compare-borrow CondCC that condB maps to:
	// after an ADD, C=1 means unsigned overflow, whereas after a SUB/CMP, C=1 means
	// no-borrow. So this branch uses a64.CondCS explicitly, not condB.
	f.a.Adds32(nw, nw, delta)
	failOverflow := f.a.Bcond(a64.CondCS)
	max := f.allocReg(maskOf(delta).add(ref).add(tbl).add(old).add(nw))
	f.ld32(max, tbl, 4)
	f.cmpRR(nw, max, false)
	failMax := f.a.Bcond(condA)
	f.release(max)
	// table.grow keeps the descriptor pointer, old length, and new length live
	// across descriptor snapshotting and the fill loop. Those helpers allocate
	// scratch registers internally, so protect these fixed live temporaries just
	// like table.fill protects its destination/count registers.
	f.pinned = f.pinned.add(tbl).add(old).add(nw)
	f.snapshotFuncrefDescriptor(ref, valSlot)
	dst := f.allocReg(maskOf(delta).add(ref).add(tbl).add(old).add(nw))
	f.a.MovReg32(dst, old)
	f.tableEntryAddr(dst, tbl)
	f.fillTableEntries(dst, delta, valSlot)
	f.st32(tbl, 0, nw)
	f.pinned = f.pinned.remove(nw).remove(old).remove(tbl)
	done := f.a.Branch()
	f.a.PatchBranch19(failOverflow, f.a.Len())
	f.a.PatchBranch19(failMax, f.a.Len())
	f.a.MovImm64(old, 0xFFFFFFFF) // -1 as i32 (was MovImm32(old,-1))
	f.a.PatchBranch26(done, f.a.Len())
	f.pinned = f.pinned.remove(delta)
	f.pinned = f.pinned.remove(ref)
	f.release(delta)
	f.release(ref)
	f.release(tbl)
	f.release(nw)
	f.release(dst)
	f.pushReg(old, mtI32)
	return nil
}

func (f *fn) externrefTableGrow(tableIdx uint32) error {
	f.materializePendingLoads()
	f.flush()
	delta := f.materialize(f.popValue())
	f.pinned = f.pinned.add(delta)
	ref := f.materialize(f.popValue())
	f.pinned = f.pinned.add(ref)
	tbl := f.allocReg(maskOf(delta).add(ref))
	f.loadTableDescriptor(tbl, tableIdx)
	old := f.allocReg(maskOf(delta).add(ref).add(tbl))
	f.ld32(old, tbl, 0)
	nw := f.allocReg(maskOf(delta).add(ref).add(tbl).add(old))
	f.a.MovReg32(nw, old)
	f.a.Adds32(nw, nw, delta)
	failOverflow := f.a.Bcond(a64.CondCS)
	max := f.allocReg(maskOf(delta).add(ref).add(tbl).add(old).add(nw))
	f.ld32(max, tbl, 4)
	f.cmpRR(nw, max, false)
	failMax := f.a.Bcond(condA)
	f.release(max)
	f.pinned = f.pinned.add(tbl).add(old).add(nw)
	dst := f.allocReg(maskOf(delta).add(ref).add(tbl).add(old).add(nw))
	f.a.MovReg32(dst, old)
	f.typedTableEntryAddr(dst, tbl, tableIdx)
	f.fillExternrefEntries(dst, delta, ref)
	f.st32(tbl, 0, nw)
	f.pinned = f.pinned.remove(nw).remove(old).remove(tbl)
	done := f.a.Branch()
	f.a.PatchBranch19(failOverflow, f.a.Len())
	f.a.PatchBranch19(failMax, f.a.Len())
	f.a.MovImm64(old, 0xFFFFFFFF)
	f.a.PatchBranch26(done, f.a.Len())
	f.pinned = f.pinned.remove(delta).remove(ref)
	f.release(delta)
	f.release(ref)
	f.release(tbl)
	f.release(nw)
	f.release(dst)
	f.pushReg(old, mtI32)
	return nil
}

func (f *fn) tableGet(r *wasm.Reader) error {
	tableIdx, err := readSingleTableIndex(r)
	if err != nil {
		return err
	}
	entry, tbl := f.checkedTableEntryAddr(f.materialize(f.popValue()), tableIdx)
	f.pinned = f.pinned.add(entry)
	slot := f.allocReg(0)
	if f.tableIsExternref(tableIdx) {
		f.ld64(slot, entry, 0)
	} else {
		f.ld64(slot, entry, runtime.TableEntryRefSlotOffset)
	}
	f.pinned = f.pinned.remove(entry)
	f.release(entry)
	f.release(tbl)
	f.pushReg(slot, mtI64)
	return nil
}

func (f *fn) tableSet(r *wasm.Reader) error {
	tableIdx, err := readSingleTableIndex(r)
	if err != nil {
		return err
	}
	ref := f.materialize(f.popValue())
	f.pinned = f.pinned.add(ref)
	entry, tbl := f.checkedTableEntryAddr(f.materialize(f.popValue()), tableIdx)
	f.pinned = f.pinned.add(entry)
	if f.tableIsExternref(tableIdx) {
		f.st64(entry, 0, ref)
	} else {
		f.copyFuncrefToEntry(ref, entry)
	}
	f.pinned = f.pinned.remove(entry)
	f.pinned = f.pinned.remove(ref)
	f.release(entry)
	f.release(tbl)
	f.release(ref)
	return nil
}

func (f *fn) refNull(r *wasm.Reader) error {
	if err := skipRefHeapTypeImmediate(r); err != nil {
		return err
	}
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: 0})
	return nil
}

// Funcref values are descriptor pointers (0 = null, non-zero = immutable
// descriptor for a function). Using a globally meaningful handle lets funcrefs
// round-trip through imported/shared tables: table.get returns the producing
// instance's descriptor pointer and table.set/fill/grow copy that descriptor
// back without reinterpreting it in the current instance's function index space.
func (f *fn) refFunc(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	ref := f.allocReg(0)
	f.ld64(ref, linMemReg, -int32(offFuncRefDescPtr))
	f.cmpImm(ref, 0, true) // was TestSelf — CMP ref,#0 (SUBS XZR,ref,#0)
	f.trapIf(condE, trapIndirectOOB)
	f.leaDisp(ref, ref, int32((idx+1)*runtime.FuncRefDescBytes), true)
	f.pushReg(ref, mtI64)
	return nil
}

func (f *fn) refIsNull() {
	ref := f.materialize(f.popValue())
	f.cmpImm(ref, 0, true) // was TestSelf
	f.a.Cset32(ref, condE) // ref = (ref == 0) ? 1 : 0 (was SetccReg)
	f.pushReg(ref, mtI32)
}

func (f *fn) refEq() {
	right := f.materialize(f.popValue())
	f.pinned = f.pinned.add(right)
	left := f.materialize(f.popValue())
	f.cmpRR(left, right, true)
	f.pinned = f.pinned.remove(right)
	f.release(right)
	f.a.Cset32(left, condE) // left = (left == right) ? 1 : 0 (was SetccReg)
	f.pushReg(left, mtI32)
}

func (f *fn) snapshotFuncrefDescriptor(ref Reg, slot int) {
	f.cmpImm(ref, 0, true)   // was TestSelf
	null := f.a.Bcond(condE) // imm19
	tmp := f.allocReg(maskOf(ref))
	f.ld64(tmp, ref, runtime.TableEntryCodePtrOffset)
	f.cmpImm(tmp, 0, true)
	f.trapIf(condE, trapIndirectOOB)
	f.st64(SP, f.spillOff(slot), tmp)
	for i, off := 1, int32(8); off < runtime.TableEntryBytes; i, off = i+1, off+8 {
		f.ld64(tmp, ref, off)
		f.st64(SP, f.spillOff(slot+i), tmp)
	}
	f.release(tmp)
	ready := f.a.Branch() // imm26
	f.a.PatchBranch19(null, f.a.Len())
	f.a.MovImm64(ref, 0) // zero the descriptor register (was XorSelf32)
	for i := 0; i < runtime.TableEntryBytes/8; i++ {
		f.st64(SP, f.spillOff(slot+i), ref)
	}
	f.a.PatchBranch26(ready, f.a.Len())
}

func (f *fn) fillTableEntries(dst, count Reg, slot int) {
	f.cmpImm(count, 0, true) // was TestSelf
	done := f.a.Bcond(condE) // imm19
	loop := f.a.Len()
	tmp := f.allocReg(maskOf(dst).add(count))
	for i, off := 0, int32(0); off < runtime.TableEntryBytes; i, off = i+1, off+8 {
		f.ld64(tmp, SP, f.spillOff(slot+i))
		f.st64(dst, off, tmp)
	}
	f.release(tmp)
	f.leaDisp(dst, dst, runtime.TableEntryBytes, true)
	f.a.SubsImm64(count, count, 1) // count-- and set flags (was AluRI(5,count,1,true))
	f.a.PatchBranch19(f.a.Bcond(condNE), loop)
	f.a.PatchBranch19(done, f.a.Len())
}

func (f *fn) fillExternrefEntries(dst, count, ref Reg) {
	f.cmpImm(count, 0, true)
	done := f.a.Bcond(condE)
	loop := f.a.Len()
	f.st64(dst, 0, ref)
	f.leaDisp(dst, dst, 8, true)
	f.a.SubsImm64(count, count, 1)
	f.a.PatchBranch19(f.a.Bcond(condNE), loop)
	f.a.PatchBranch19(done, f.a.Len())
}

func (f *fn) copyFuncrefToEntry(ref, entry Reg) {
	valSlot := f.allocSpillSlots(runtime.TableEntryBytes / 8)
	f.snapshotFuncrefDescriptor(ref, valSlot)
	tmp := f.allocReg(maskOf(ref).add(entry))
	for i, off := 0, int32(0); off < runtime.TableEntryBytes; i, off = i+1, off+8 {
		f.ld64(tmp, SP, f.spillOff(valSlot+i))
		f.st64(entry, off, tmp)
	}
	f.release(tmp)
}

func (f *fn) checkedTableEntryAddr(idxReg Reg, tableIdx uint32) (entry Reg, table Reg) {
	f.pinned = f.pinned.add(idxReg)
	tbl := f.allocReg(0)
	f.loadTableDescriptor(tbl, tableIdx)
	f.pinned = f.pinned.add(tbl)
	ln := f.allocReg(0)
	f.ld32(ln, tbl, 0)
	f.cmpRR(idxReg, ln, false) // was AluRR(0x39,…) — CMP idx,len (32-bit)
	f.release(ln)
	f.trapIf(condAE, trapIndirectOOB)
	f.typedTableEntryAddr(idxReg, tbl, tableIdx)
	f.pinned = f.pinned.remove(tbl)
	f.pinned = f.pinned.remove(idxReg)
	return idxReg, tbl
}

func skipRefHeapTypeImmediate(r *wasm.Reader) error {
	b, err := r.Byte()
	if err != nil {
		return err
	}
	for b&0x80 != 0 {
		b, err = r.Byte()
		if err != nil {
			return err
		}
	}
	return nil
}
