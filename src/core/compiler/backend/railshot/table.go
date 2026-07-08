package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

const (
	offFuncRefDescPtr   = abi.FuncRefDescPtrOffset
	offFuncRefDescCount = abi.FuncRefDescCountOffset
	offPassiveElemPtr   = abi.PassiveElemPtrOffset
)

func (f *fn) tableSize(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	if idx != 0 {
		return fmt.Errorf("table.size: multi-table unsupported: table %d", idx)
	}
	tbl := f.allocReg(0)
	f.a.Load64(tbl, RBX, -int32(offTablePtr))
	f.a.Load32(tbl, tbl, 0)
	f.pushReg(tbl, mtI32)
	return nil
}

func (f *fn) tableInit(r *wasm.Reader) error {
	elemIdx, err := r.U32()
	if err != nil {
		return err
	}
	tableIdx, err := r.U32()
	if err != nil {
		return err
	}
	if tableIdx != 0 {
		return fmt.Errorf("table.init: multi-table unsupported: table %d", tableIdx)
	}
	f.materializePendingLoads()
	f.flush()
	d := f.depth()
	f.a.Load64(RDI, RSP, f.spillOff(d-3)) // dst table offset
	f.a.Load64(RSI, RSP, f.spillOff(d-2)) // src element offset
	f.a.Load64(RCX, RSP, f.spillOff(d-1)) // n entries

	f.a.Load64(R8, RBX, -int32(offTablePtr))
	f.a.Load32(RAX, R8, 0)
	f.a.LeaScaled(RDX, RDI, RCX, 0, 0)
	f.trapUnlessLE(RDX, RAX)
	f.a.ShiftImm(4, RDI, 5, true)
	f.a.Add64(RDI, R8)
	f.a.LeaDisp(RDI, RDI, 8)

	disp := int32(elemIdx) * runtime.PassiveElemDescBytes
	f.a.Load64(R8, RBX, -int32(offPassiveElemPtr))
	f.a.Load32(RAX, R8, disp+8)
	f.a.LeaScaled(RDX, RSI, RCX, 0, 0)
	f.trapUnlessLE(RDX, RAX)
	f.a.Load64(R8, R8, disp)
	f.a.ShiftImm(4, RSI, 5, true)
	f.a.Add64(RSI, R8)
	f.a.ShiftImm(4, RCX, 5, true)
	f.a.RepMovsb()
	f.setDepth(d - 3)
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
	f.a.Load64(R8, RBX, -int32(offPassiveElemPtr))
	f.a.StoreImm32Mem(R8, disp, 0)
	return nil
}

func (f *fn) tableCopy(r *wasm.Reader) error {
	dstIdx, err := r.U32()
	if err != nil {
		return err
	}
	srcIdx, err := r.U32()
	if err != nil {
		return err
	}
	if dstIdx != 0 || srcIdx != 0 {
		return fmt.Errorf("table.copy: multi-table unsupported: tables %d,%d", dstIdx, srcIdx)
	}
	f.materializePendingLoads()
	f.flush()
	d := f.depth()
	f.a.Load64(RDI, RSP, f.spillOff(d-3))
	f.a.Load64(RSI, RSP, f.spillOff(d-2))
	f.a.Load64(RCX, RSP, f.spillOff(d-1))
	f.a.Load64(R8, RBX, -int32(offTablePtr))
	f.a.Load32(RAX, R8, 0)
	f.a.LeaScaled(RDX, RDI, RCX, 0, 0)
	f.trapUnlessLE(RDX, RAX)
	f.a.LeaScaled(RDX, RSI, RCX, 0, 0)
	f.trapUnlessLE(RDX, RAX)
	f.a.ShiftImm(4, RDI, 5, true)
	f.a.Add64(RDI, R8)
	f.a.LeaDisp(RDI, RDI, 8)
	f.a.ShiftImm(4, RSI, 5, true)
	f.a.Add64(RSI, R8)
	f.a.LeaDisp(RSI, RSI, 8)
	f.a.ShiftImm(4, RCX, 5, true)
	f.a.Cmp64(RDI, RSI)
	fwd := f.a.JccPlaceholder(condBE)
	f.a.LeaScaled(RDX, RSI, RCX, 0, 0)
	f.a.Cmp64(RDI, RDX)
	fwdDisjoint := f.a.JccPlaceholder(condAE)
	f.a.LeaScaled(RDI, RDI, RCX, 0, -1)
	f.a.LeaScaled(RSI, RSI, RCX, 0, -1)
	f.a.Std()
	f.a.RepMovsb()
	f.a.Cld()
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(fwd, f.a.Len())
	f.a.PatchRel32(fwdDisjoint, f.a.Len())
	f.a.RepMovsb()
	f.a.PatchRel32(done, f.a.Len())
	f.setDepth(d - 3)
	return nil
}

func (f *fn) tableFill(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	if idx != 0 {
		return fmt.Errorf("table.fill: multi-table unsupported: table %d", idx)
	}
	f.materializePendingLoads()
	f.flush()
	d := f.depth()
	valSlot := f.allocSpillSlots(runtime.TableEntryBytes / 8)
	f.a.Load64(RDI, RSP, f.spillOff(d-3))
	f.a.Load64(RAX, RSP, f.spillOff(d-2))
	f.a.Load64(RCX, RSP, f.spillOff(d-1))
	f.a.Load64(R8, RBX, -int32(offTablePtr))
	f.a.Load32(RDX, R8, 0)
	f.a.LeaScaled(RDI, RDI, RCX, 0, 0)
	f.trapUnlessLE(RDI, RDX)
	f.a.Load64(RDI, RSP, f.spillOff(d-3))
	f.a.ShiftImm(4, RDI, 5, true)
	f.a.Add64(RDI, R8)
	f.a.LeaDisp(RDI, RDI, 8)
	// snapshotFuncrefDescriptor uses the register allocator internally. Keep the
	// fixed destination/count registers live across it so descriptor snapshotting
	// cannot clobber the table.fill loop operands.
	f.pinned = f.pinned.add(RDI).add(RCX)
	f.snapshotFuncrefDescriptor(RAX, valSlot)
	f.fillTableEntries(RDI, RCX, valSlot)
	f.pinned = f.pinned.remove(RCX).remove(RDI)
	f.setDepth(d - 3)
	return nil
}

func (f *fn) tableGrow(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	if idx != 0 {
		return fmt.Errorf("table.grow: multi-table unsupported: table %d", idx)
	}
	f.materializePendingLoads()
	f.flush()
	delta := f.materialize(f.popValue())
	f.pinned = f.pinned.add(delta)
	ref := f.materialize(f.popValue())
	f.pinned = f.pinned.add(ref)
	valSlot := f.allocSpillSlots(runtime.TableEntryBytes / 8)
	tbl := f.allocReg(maskOf(delta).add(ref))
	f.a.Load64(tbl, RBX, -int32(offTablePtr))
	old := f.allocReg(maskOf(delta).add(ref).add(tbl))
	f.a.Load32(old, tbl, 0)
	nw := f.allocReg(maskOf(delta).add(ref).add(tbl).add(old))
	f.a.MovRegReg32(nw, old)
	f.a.Add32(nw, delta)
	failOverflow := f.a.JccPlaceholder(condB)
	max := f.allocReg(maskOf(delta).add(ref).add(tbl).add(old).add(nw))
	f.a.Load32(max, tbl, 4)
	f.a.Cmp32(nw, max)
	failMax := f.a.JccPlaceholder(condA)
	f.release(max)
	// table.grow keeps the descriptor pointer, old length, and new length live
	// across descriptor snapshotting and the fill loop. Those helpers allocate
	// scratch registers internally, so protect these fixed live temporaries just
	// like table.fill protects its destination/count registers.
	f.pinned = f.pinned.add(tbl).add(old).add(nw)
	f.snapshotFuncrefDescriptor(ref, valSlot)
	dst := f.allocReg(maskOf(delta).add(ref).add(tbl).add(old).add(nw))
	f.a.MovRegReg32(dst, old)
	f.a.ShiftImm(4, dst, 5, true)
	f.a.Add64(dst, tbl)
	f.a.LeaDisp(dst, dst, 8)
	f.fillTableEntries(dst, delta, valSlot)
	f.a.Store32(tbl, 0, nw)
	f.pinned = f.pinned.remove(nw).remove(old).remove(tbl)
	done := f.a.JmpPlaceholder()
	f.a.PatchRel32(failOverflow, f.a.Len())
	f.a.PatchRel32(failMax, f.a.Len())
	f.a.MovImm32(old, -1)
	f.a.PatchRel32(done, f.a.Len())
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

func (f *fn) tableGet(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	if idx != 0 {
		return fmt.Errorf("table.get: multi-table unsupported: table %d", idx)
	}
	entry, tbl := f.checkedTableEntryAddr(f.materialize(f.popValue()))
	f.pinned = f.pinned.add(entry)
	slot := f.allocReg(0)
	f.a.Load64(slot, entry, 24)
	f.pinned = f.pinned.remove(entry)
	f.release(entry)
	f.release(tbl)
	f.pushReg(slot, mtI64)
	return nil
}

func (f *fn) tableSet(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	if idx != 0 {
		return fmt.Errorf("table.set: multi-table unsupported: table %d", idx)
	}
	ref := f.materialize(f.popValue())
	f.pinned = f.pinned.add(ref)
	entry, tbl := f.checkedTableEntryAddr(f.materialize(f.popValue()))
	f.pinned = f.pinned.add(entry)
	f.copyFuncrefToEntry(ref, entry)
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

// TODO(reference-types): Funcref values are currently compact instance-local
// payloads (0 = null, function index + 1 = non-null). That is sufficient for
// same-instance table mutation and call_indirect descriptor snapshots, but not
// for full cross-instance first-class reference identity through shared tables
// (table.get/table.set/ref.eq). Replace this with a globally meaningful funcref
// handle when completing reference-types support.
func (f *fn) refFunc(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	f.pushValue(storage{kind: stConst, typ: mtI64, cval: int64(idx) + 1})
	return nil
}

func (f *fn) refIsNull() {
	ref := f.materialize(f.popValue())
	f.a.TestSelf(ref, true)
	f.a.SetccReg(condE, ref)
	f.pushReg(ref, mtI32)
}

func (f *fn) refEq() {
	right := f.materialize(f.popValue())
	f.pinned = f.pinned.add(right)
	left := f.materialize(f.popValue())
	f.cmpRR(left, right, true)
	f.pinned = f.pinned.remove(right)
	f.release(right)
	f.a.SetccReg(condE, left)
	f.pushReg(left, mtI32)
}

func (f *fn) snapshotFuncrefDescriptor(ref Reg, slot int) {
	f.a.TestSelf(ref, true)
	null := f.a.JccPlaceholder(condE)
	base := f.allocReg(maskOf(ref))
	f.a.Load32(base, RBX, -int32(offFuncRefDescCount))
	f.a.Cmp64(ref, base)
	f.trapIf(condAE, trapIndirectOOB)
	f.a.Load64(base, RBX, -int32(offFuncRefDescPtr))
	f.a.TestSelf(base, true)
	f.trapIf(condE, trapIndirectOOB)
	f.a.ShiftImm(4, ref, 5, true)
	f.a.Add64(ref, base)
	tmp := f.allocReg(maskOf(ref))
	f.a.Load64(tmp, ref, 0)
	f.a.TestSelf(tmp, true)
	f.trapIf(condE, trapIndirectOOB)
	f.a.Store64(RSP, f.spillOff(slot), tmp)
	for i, off := 1, int32(8); off < runtime.TableEntryBytes; i, off = i+1, off+8 {
		f.a.Load64(tmp, ref, off)
		f.a.Store64(RSP, f.spillOff(slot+i), tmp)
	}
	f.release(tmp)
	f.release(base)
	ready := f.a.JmpPlaceholder()
	f.a.PatchRel32(null, f.a.Len())
	f.a.XorSelf32(ref)
	for i := 0; i < runtime.TableEntryBytes/8; i++ {
		f.a.Store64(RSP, f.spillOff(slot+i), ref)
	}
	f.a.PatchRel32(ready, f.a.Len())
}

func (f *fn) fillTableEntries(dst, count Reg, slot int) {
	f.a.TestSelf(count, true)
	done := f.a.JccPlaceholder(condE)
	loop := f.a.Len()
	tmp := f.allocReg(maskOf(dst).add(count))
	for i, off := 0, int32(0); off < runtime.TableEntryBytes; i, off = i+1, off+8 {
		f.a.Load64(tmp, RSP, f.spillOff(slot+i))
		f.a.Store64(dst, off, tmp)
	}
	f.release(tmp)
	f.a.LeaDisp(dst, dst, runtime.TableEntryBytes)
	f.a.AluRI(5, count, 1, true)
	f.a.PatchRel32(f.a.JccPlaceholder(condNE), loop)
	f.a.PatchRel32(done, f.a.Len())
}

func (f *fn) copyFuncrefToEntry(ref, entry Reg) {
	valSlot := f.allocSpillSlots(runtime.TableEntryBytes / 8)
	f.snapshotFuncrefDescriptor(ref, valSlot)
	tmp := f.allocReg(maskOf(ref).add(entry))
	for i, off := 0, int32(0); off < runtime.TableEntryBytes; i, off = i+1, off+8 {
		f.a.Load64(tmp, RSP, f.spillOff(valSlot+i))
		f.a.Store64(entry, off, tmp)
	}
	f.release(tmp)
}

func (f *fn) checkedTableEntryAddr(idxReg Reg) (entry Reg, table Reg) {
	f.pinned = f.pinned.add(idxReg)
	tbl := f.allocReg(0)
	f.a.Load64(tbl, RBX, -int32(offTablePtr))
	f.pinned = f.pinned.add(tbl)
	ln := f.allocReg(0)
	f.a.Load32(ln, tbl, 0)
	f.a.AluRR(0x39, idxReg, ln, false)
	f.release(ln)
	f.trapIf(condAE, trapIndirectOOB)
	f.a.ShiftImm(4, idxReg, 5, true)
	f.a.AluRR(0x01, idxReg, tbl, true)
	f.a.LeaDisp(idxReg, idxReg, 8)
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
