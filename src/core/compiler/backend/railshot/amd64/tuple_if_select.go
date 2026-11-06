//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var tupleIfSelectEnabled = true

type tupleStoreHint struct {
	locals [2]int
	end    int
}

// Peek at immediate result assignments without consuming the caller's reader.
// Private or regional locals retain the ordinary temporary-result path.
func (f *fn) inlineTupleStoreLocals(r *wasm.Reader) tupleStoreHint {
	invalid := tupleStoreHint{locals: [2]int{-1, -1}}
	if f.localBase != 0 || f.inlineDepth != 0 || f.inlineForwardN != 0 || len(f.intervalLast) != 0 || f.intervalControl || f.pinRelinquished || f.policy.CompactNative || profileEnabled || f.stats != nil && f.stats.RecordSources {
		return invalid
	}
	rr := *r
	var locals [2]int
	for i := 1; i >= 0; i-- {
		op, err := rr.Byte()
		if err != nil || op != 0x21 {
			return invalid
		}
		x, err := rr.U32()
		if err != nil || uint64(x) >= uint64(f.nLocals) {
			return invalid
		}
		locals[i] = int(x)
	}
	if locals[0] == locals[1] {
		return invalid
	}
	return tupleStoreHint{locals: locals, end: rr.Offset()}
}

func (f *fn) tupleSelectStoreRegisters(locals [2]int, types []machineType, sources regMask) ([2]Reg, bool) {
	var registers [2]Reg
	used := sources.union(maskOf(RAX, RDX, RCX, R8))
	for i, local := range locals {
		if local < 0 || local >= f.nLocals || f.localType[local] != types[i] {
			return registers, false
		}
		reg, floating, pinned := f.pinReg(local)
		if !pinned || floating || used.has(reg) {
			return registers, false
		}
		registers[i], used = reg, used.add(reg)
	}
	return registers, true
}

func readTupleLocalArms(r wasm.Reader) (locals [2][2]uint32, end int, ok bool) {
	for arm := range locals {
		for i := range locals[arm] {
			op, err := r.Byte()
			if err != nil || op != 0x20 {
				return locals, 0, false
			}
			locals[arm][i], err = r.U32()
			if err != nil {
				return locals, 0, false
			}
		}
		want := byte(0x05)
		if arm == 1 {
			want = 0x0b
		}
		op, err := r.Byte()
		if err != nil || op != want {
			return locals, 0, false
		}
	}
	return locals, r.Offset(), true
}

// Only a complete read-only condition followed by one local-only pair if may
// omit its synthetic function merge. No return or branch can reach that boundary.
func integerTupleSelectTarget(m *wasm.Module, code wasm.Func, ft *wasm.CompType) bool {
	if len(code.BodyBytes) == 0 || len(code.BodyBytes) > min(inlineMaxBytes, 64) || len(ft.Results) != 2 || !sigIsIntOnly(ft) {
		return false
	}
	r := wasm.NewReader(code.BodyBytes)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		switch {
		case op == 0x01:
		case op == 0x20:
			x, err := r.U32()
			if err != nil || uint64(x) >= uint64(len(ft.Params)) {
				return false
			}
		case op == 0x41:
			if _, err := r.I32(); err != nil {
				return false
			}
		case op == 0x42:
			if _, err := r.I64(); err != nil {
				return false
			}
		case op >= 0x45 && op <= 0x5a || op >= 0x67 && op <= 0x8a || op == 0xa7 || op == 0xac || op == 0xad:
		case op == 0x04:
			index, err := r.I64()
			if err != nil || index < 0 || uint64(index) > uint64(^uint32(0)) {
				return false
			}
			block, ok := m.TypeFunc(uint32(index))
			if !ok || len(block.Params) != 0 || len(block.Results) != 2 || !sigIsIntOnly(block) {
				return false
			}
			arms, end, ok := readTupleLocalArms(*r)
			if !ok {
				return false
			}
			for _, arm := range arms {
				for i, x := range arm {
					if uint64(x) >= uint64(len(ft.Params)) || ft.Params[x] != block.Results[i] || ft.Results[i] != block.Results[i] {
						return false
					}
				}
			}
			if r.JumpTo(end) != nil {
				return false
			}
			op, err = r.Byte()
			return err == nil && op == 0x0b && !r.HasNext()
		default:
			return false
		}
	}
	return false
}

func (f *fn) tryTupleIfSelect(r *wasm.Reader, signature wasm.Reader, types []machineType) (bool, error) {
	return f.tryTupleIfSelectStores(r, signature, types, [2]int{-1, -1}, nil)
}

func (f *fn) tryTupleIfSelectStores(r *wasm.Reader, signature wasm.Reader, types []machineType, stores [2]int, consumed *bool) (bool, error) {
	if !f.opt(optTupleIfSelect) || profileEnabled || f.unreachable || f.makesCalls || f.usesCalls ||
		f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 ||
		f.intervalControl || f.policy.CompactNative || f.inlineForwardN != 0 || len(types) != 2 ||
		f.stats != nil && f.stats.RecordSources || !treeReorderSafe(f.s.back()) {
		return false, nil
	}
	index, err := signature.I64()
	if err != nil || index < 0 || uint64(index) > uint64(^uint32(0)) {
		return false, nil
	}
	ft, ok := f.m.TypeFunc(uint32(index))
	if !ok || len(ft.Params) != 0 || len(ft.Results) != 2 || !sigIsIntOnly(ft) {
		return false, nil
	}
	arms, end, ok := readTupleLocalArms(*r)
	if !ok {
		return false, nil
	}
	var locals [2][2]int
	var sources [2][2]Reg
	var sourceMask regMask
	for arm := range arms {
		for i, x := range arms[arm] {
			absolute := uint64(x) + uint64(f.localBase)
			if absolute >= uint64(len(f.localType)) || f.localType[absolute] != types[i] {
				return false, nil
			}
			reg, floating, pinned := f.pinReg(int(absolute))
			if !pinned || floating {
				return false, nil
			}
			locals[arm][i], sources[arm][i] = int(absolute), reg
			sourceMask = sourceMask.add(reg)
		}
	}
	destinations, direct := f.tupleSelectStoreRegisters(stores, types, sourceMask)
	f.materializePendingTraps()
	if direct {
		// Preserve get-time values in the caller prefix before either home changes.
		for _, local := range stores {
			f.realizeLocalRefs(local, nil)
		}
	}
	for _, arm := range locals {
		for _, local := range arm {
			f.recoverLocal(local)
		}
	}
	savedPins, savedReserved := f.pinned, f.reserved
	f.reserved = f.reserved.union(sourceMask)
	for i := range destinations {
		if direct {
			f.pinned = f.pinned.add(destinations[i])
			f.reserved = f.reserved.add(destinations[i])
			continue
		}
		// Fixed-role instructions in a deferred condition may reclaim these
		// registers despite ordinary pins. Keep both result homes elsewhere.
		reg := f.allocRegOrNone(maskOf(RAX, RDX, RCX, R8))
		if reg == regNone {
			f.pinned, f.reserved = savedPins, savedReserved
			return false, nil
		}
		destinations[i] = reg
		f.pinned = f.pinned.add(reg)
	}
	condition := f.s.back()
	var cc Cond
	if isFusableCompare(condition) {
		cc = f.condenseToFlags(condition)
	} else {
		reg, owned := f.materializeRead(f.popValue())
		f.a.TestSelf(reg, false)
		cc = condNE
		if owned {
			f.release(reg)
		}
	}
	if direct {
		// Both caller homes are disjoint from all sources. Copy the false
		// pair first, then overwrite it only on the true path. The conditional
		// edge changes no compiler state and both paths define both homes.
		for i, dest := range destinations {
			f.moveInt(dest, sources[1][i], types[i])
		}
		over := f.a.JccPlaceholder(invertCond(cc))
		for i, dest := range destinations {
			f.moveInt(dest, sources[0][i], types[i])
		}
		f.a.PatchRel32(over, f.a.Len())
		f.stats.peep("tuple-if-select-store-branch")
	} else {
		for i, dest := range destinations {
			f.moveInt(dest, sources[1][i], types[i])
			f.a.Cmovcc(cc, dest, sources[0][i], types[i] == mtI64)
		}
	}
	f.pinned, f.reserved = savedPins, savedReserved
	for i, dest := range destinations {
		value := f.pushReg(dest, types[i])
		if types[i] == mtI32 {
			value.st.setValueFacts(factUpper32Zero)
		}
		if direct {
			local := stores[i]
			if local < 64 {
				f.localWritten |= 1 << local
			}
			f.setLocal(nil, local, false)
		}
	}
	if direct {
		f.prepareStoreForward(0x21)
		*consumed = true
		f.stats.peep("tuple-if-select-local-stores")
	}
	if err := r.JumpTo(end); err != nil {
		return true, err
	}
	f.stats.peep("tuple-if-select")
	return true, nil
}

func (f *fn) inlineTupleSelectBody(t *inlineTarget, stores tupleStoreHint) (bool, error) {
	consumed := false
	minCtrl := len(f.ctrl)
	r := wasm.NewReader(t.body)
	for {
		f.wasmPC = f.tracePCBase + uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			return false, err
		}
		switch op {
		case 0x01:
			continue
		case 0x04:
			signature := *r
			params, types, _, _, parseErr := f.blockType(r)
			if parseErr != nil {
				return false, parseErr
			}
			done := false
			if len(params) == 0 && len(types) == 2 {
				done, err = f.tryTupleIfSelectStores(r, signature, types, stores.locals, &consumed)
			}
			if !done && err == nil {
				*r = signature
				err = f.opBlock(r, op)
			}
		case 0x05:
			err = f.opElse()
		case 0x0b:
			if len(f.ctrl) == minCtrl {
				return consumed, nil
			}
			err = f.opEnd()
		default:
			err = f.emitPlain(r, op)
		}
		if err != nil {
			return false, err
		}
	}
}
