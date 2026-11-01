//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var tupleIfSelectEnabled = true

type tupleIfSelectPlan struct {
	locals  [2][2]int
	sources [2][2]Reg
	types   [2]machineType
	end     int
}

// scanTupleIfSelect proves both arms before touching compiler state. Dedicated
// local homes stay valid throughout the plain, read-only integer condition.
func (f *fn) scanTupleIfSelect(r wasm.Reader, signature wasm.Reader, types []machineType, localBase int) (tupleIfSelectPlan, bool) {
	var plan tupleIfSelectPlan
	if !f.opt(optTupleIfSelect) || profileEnabled || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || len(types) != 2 {
		return plan, false
	}
	// machineType also represents references with mtI64. Inspect the actual Wasm
	// signature so a reference pair cannot be mistaken for an integer pair.
	index, err := signature.I64()
	if err != nil || index < 0 {
		return plan, false
	}
	ft, ok := f.m.TypeFunc(uint32(index))
	if !ok || len(ft.Params) != 0 || len(ft.Results) != 2 {
		return plan, false
	}
	for _, typ := range ft.Results {
		if typ != wasm.I32 && typ != wasm.I64 {
			return plan, false
		}
	}
	rr := r
	for arm := 0; arm < 2; arm++ {
		for i := 0; i < 2; i++ {
			op, err := rr.Byte()
			if err != nil || op != 0x20 {
				return plan, false
			}
			x, err := rr.U32()
			if err != nil {
				return plan, false
			}
			absolute := uint64(x) + uint64(localBase)
			if absolute >= uint64(len(f.localType)) {
				return plan, false
			}
			local := int(absolute)
			if f.localType[local] != types[i] {
				return plan, false
			}
			reg, isFloat, pinned := f.pinReg(local)
			if !pinned || isFloat {
				return plan, false
			}
			plan.locals[arm][i], plan.sources[arm][i] = local, reg
		}
		delimiter := byte(0x05)
		if arm == 1 {
			delimiter = 0x0b
		}
		op, err := rr.Byte()
		if err != nil || op != delimiter {
			return plan, false
		}
	}
	copy(plan.types[:], types)
	plan.end = rr.Offset()
	return plan, true
}

func (f *fn) emitTupleIfSelect(plan tupleIfSelectPlan) {
	f.materializePendingTraps()
	for _, arm := range plan.locals {
		for _, local := range arm {
			f.recoverLocal(local)
		}
	}
	saved := f.pinned
	for _, arm := range plan.sources {
		for _, reg := range arm {
			f.pinned = f.pinned.add(reg)
		}
	}
	var destinations [2]Reg
	for i := range destinations {
		destinations[i] = f.allocReg(0)
		f.pinned = f.pinned.add(destinations[i])
	}
	cond := f.s.back()
	var cc Cond
	if isFusableCondition(cond) {
		cc = f.condenseToFlags(cond)
	} else {
		reg, owned := f.materializeRead(f.popValue())
		f.a.CmpImm32(reg, 0)
		cc = condNE
		if owned {
			f.release(reg)
		}
	}
	// Nothing that allocates or changes flags may intervene between the compare
	// and both selections. Neither destination aliases any arm's source.
	for i, dest := range destinations {
		f.a.Csel(dest, plan.sources[0][i], plan.sources[1][i], cc, plan.types[i] == mtI64)
	}
	f.pinned = saved
	for i, dest := range destinations {
		value := f.pushReg(dest, plan.types[i])
		if plan.types[i] == mtI32 {
			value.st.setValueFacts(factUpper32Zero)
		}
	}
	f.stats.peep("tuple-if-select")
}

// inlineTupleSelectionBody omits the synthetic merge frame only for a complete
// leaf consisting of a plain integer condition and one local-only pair diamond.
// Prove the entire body before emitting anything; a rejection uses the ordinary
// control-flow splice, including all its branch/return reconciliation.
func (f *fn) planInlineTupleSelection(t *inlineTarget, localBase int) (tupleIfSelectPlan, int, bool) {
	var empty tupleIfSelectPlan
	if !f.opt(optTupleIfSelect) || profileEnabled || !t.tupleControl || f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 {
		return empty, 0, false
	}
	rr := wasm.NewReader(t.body)
	conditionEnd := -1
	for rr.Offset() < len(t.body) {
		at := rr.Offset()
		op, err := rr.Byte()
		if err != nil {
			return empty, 0, false
		}
		switch {
		case op == 0x04:
			conditionEnd = at
		case op == 0x20:
			if _, err := rr.U32(); err != nil {
				return empty, 0, false
			}
		case op == 0x41:
			if _, err := rr.I32(); err != nil {
				return empty, 0, false
			}
		case op == 0x42:
			if _, err := rr.I64(); err != nil {
				return empty, 0, false
			}
		case op >= 0x45 && op <= 0x5a || op >= 0x67 && op <= 0x8a || op == 0xa7 || op == 0xac || op == 0xad:
		default:
			return empty, 0, false
		}
		if conditionEnd >= 0 {
			break
		}
	}
	if conditionEnd < 0 {
		return empty, 0, false
	}
	signature := *rr
	_, types, _, _, err := f.blockType(rr)
	if err != nil || len(types) != 2 {
		return empty, 0, false
	}
	plan, ok := f.scanTupleIfSelect(*rr, signature, types, localBase)
	if !ok {
		return empty, 0, false
	}
	if err := rr.JumpTo(plan.end); err != nil {
		return empty, 0, false
	}
	op, err := rr.Byte()
	if err != nil || op != 0x0b || rr.Offset() != len(t.body) {
		return empty, 0, false
	}

	return plan, conditionEnd, true
}

func (f *fn) inlineTupleSelectionBody(t *inlineTarget) (bool, error) {
	plan, conditionEnd, ok := f.planInlineTupleSelection(t, f.localBase)
	if !ok {
		return false, nil
	}

	reader := wasm.NewReader(t.body)
	for reader.Offset() < conditionEnd {
		f.wasmPC = f.tracePCBase + uint32(reader.Offset())
		op, err := reader.Byte()
		if err != nil {
			return true, err
		}
		if err := f.emitPlain(reader, op); err != nil {
			return true, err
		}
	}
	f.wasmPC = f.tracePCBase + uint32(reader.Offset())
	f.emitTupleIfSelect(plan)
	f.stats.peep("inline-tuple-if-select")
	return true, nil
}
