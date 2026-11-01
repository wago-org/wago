//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Recognize a two-value rotating recurrence:
//
//	y == 0 ? x : return_call self(y, integer_binary(x,y))
//
// Two steps restore the physical parameter order. Alternating the register
// roles eliminates the parallel-assignment copies on both steps, while keeping
// a zero test before every operation and the ordinary arithmetic/trap lowering.
func (f *fn) tryInlineRotatingTail(t *inlineTarget) (bool, error) {
	types := f.inlineTargets.localTypes(t)
	frame := &f.ctrl[f.inlineRetFrame]
	if t.paramCount() != 2 || len(types) != 2 || types[0] != types[1] || t.res0 != types[0] || !frame.has(ctrlRegMerge1) || f.makesCalls || !f.opt(optBorrowedDivRem) || !f.opt(optValueFacts) {
		return false, nil
	}
	a, afp, aok := f.pinReg(f.localBase)
	b, bfp, bok := f.pinReg(f.localBase + 1)
	if !aok || !bok || afp || bfp || a == b {
		return false, nil
	}
	wide := types[0] == mtI64
	eqz, block := byte(0x45), byte(0x7f)
	if wide {
		eqz, block = 0x50, 0x7e
	}
	r := wasm.NewReader(t.body)
	byteIs := func(want byte) bool { got, err := r.Byte(); return err == nil && got == want }
	get := func(want uint32) bool {
		if !byteIs(0x20) {
			return false
		}
		got, err := r.U32()
		return err == nil && got == want
	}
	if !get(1) || !byteIs(eqz) || !byteIs(0x04) || !byteIs(block) || !get(0) || !byteIs(0x05) || !get(1) || !get(0) || !get(1) {
		return false, nil
	}
	operationPC := r.Offset()
	op, err := r.Byte()
	if err != nil {
		return false, nil
	}
	base := byte(0x6a)
	if wide {
		base = 0x7c
	}
	if op < base || op > base+14 || !byteIs(0x12) {
		return false, nil
	}
	index, err := r.U32()
	if err != nil || index != uint32(t.globalIdx) || !byteIs(0x0b) || !byteIs(0x0b) || r.HasNext() {
		return false, nil
	}
	oldTail := f.inlineTailLoopFrame
	f.inlineTailLoopFrame = len(f.ctrl)
	defer func() { f.inlineTailLoopFrame = oldTail }()
	f.hasLoop = true
	if f.sc != nil {
		f.sc.directPreparedBounded = false
	}
	zero := func(reg Reg) int {
		if wide {
			return f.a.Cbz64(reg)
		}
		return f.a.Cbz32(reg)
	}
	step := func(left, right Reg, li, ri int) error {
		f.wasmPC = f.tracePCBase + uint32(operationPC)
		f.pushValue(storage{kind: stLocalReg, typ: types[0], reg: left, idx: uint32(li)})
		divisor := f.pushValue(storage{kind: stLocalReg, typ: types[0], reg: right, idx: uint32(ri)})
		divisor.st.setValueFacts(factNonZero) // the preceding CBZ excludes zero
		if err := f.emitPlain(wasm.NewReader(nil), op); err != nil {
			return err
		}
		result := f.s.back()
		f.condenseInto(result, left)
		f.erase(result)
		f.release(left)
		f.invalidateLocalWriteFacts(li)
		f.setFactsForLocal(li, 0)
		f.markLocalDirty(li)
		return nil
	}
	initialZero := zero(b)
	header := f.a.Len()
	if err := step(a, b, f.localBase, f.localBase+1); err != nil {
		return true, err
	}
	firstZero := zero(a)
	if err := step(b, a, f.localBase+1, f.localBase); err != nil {
		return true, err
	}
	var back int
	if wide {
		back = f.a.Cbnz64(b)
	} else {
		back = f.a.Cbnz32(b)
	}
	f.patchBranch19(back, header)
	f.patchBranch19(initialZero, f.a.Len())
	f.a.MovReg64(mergeReg, a)
	f.convergeBranchLocals(frame)
	f.branchJump(frame)
	f.patchBranch19(firstZero, f.a.Len())
	f.a.MovReg64(mergeReg, b)
	f.convergeBranchLocals(frame)
	f.branchJump(frame)
	f.unreachable = true
	f.stats.peep("inline-rotating-tail")
	f.stats.peep("inline-tail-loop")
	return true, f.opEnd(wasm.NewReader(nil))
}
