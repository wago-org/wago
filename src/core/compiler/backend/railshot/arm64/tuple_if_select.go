// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var tupleIfSelectEnabled = true

type tupleIfSelectPlan struct {
	locals  [2][2]int
	sources [2][2]Reg
	types   [2]machineType
	end     int
	stores  [2]int
	direct  bool
}

type tupleStoreHint struct {
	locals [2]int
	end    int
}

// Source assignments are high result first, then low result. Restrict hints to
// original caller locals; nested splices and regional leases retain temporaries.
func (f *fn) inlineTupleStoreLocals(r *wasm.Reader) tupleStoreHint {
	invalid := tupleStoreHint{locals: [2]int{-1, -1}}
	if f.localBase != 0 || f.inlineDepth != 0 || f.inlineForwardN != 0 || len(f.intervalLast) != 0 || f.intervalControl || f.selectGroupEnd != 0 || f.streamingReduction || f.policy.CompactNative || profileEnabled || f.stats != nil && f.stats.RecordSources {
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

func (f *fn) planTupleSelectStores(plan *tupleIfSelectPlan, locals [2]int) {
	var registers regMask
	for i, local := range locals {
		if local < 0 || local >= len(f.localType) || f.localType[local] != plan.types[i] {
			return
		}
		reg, floating, pinned := f.pinReg(local)
		if !pinned || floating {
			return
		}
		if registers.has(reg) {
			return
		}
		registers = registers.add(reg)
		for _, arm := range plan.sources {
			for _, source := range arm {
				if source == reg {
					return
				}
			}
		}
	}
	plan.stores, plan.direct = locals, true
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
	if plan.direct {
		// Snapshot old caller values before either destination is overwritten.
		// This may allocate or materialize, so it must precede the condition flags.
		for _, local := range plan.stores {
			f.realizeLocalRefs(local, nil)
		}
	}
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
		if plan.direct {
			destinations[i], _, _ = f.pinReg(plan.stores[i])
		} else {
			destinations[i] = f.allocReg(0)
		}
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
		var value *elem
		if plan.direct {
			local := plan.stores[i]
			f.invalidateBoundsCertFor(1, uint32(local))
			if local < 64 {
				f.localWritten |= 1 << local
			}
			f.markLocalDirty(local)
			f.setFactsForLocal(local, 0)
			if plan.types[i] == mtI32 {
				f.setFactsForLocal(local, factUpper32Zero)
			}
			continue
		} else {
			value = f.pushReg(dest, plan.types[i])
		}
		if plan.types[i] == mtI32 {
			value.st.setValueFacts(factUpper32Zero)
		}
	}
	if plan.direct {
		f.prepareStoreForward(0x21)
		f.stats.peep("tuple-if-select-local-stores")
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

func (f *fn) inlineTupleSelectionBody(t *inlineTarget, stores tupleStoreHint, caller *wasm.Reader) (bool, bool, error) {
	plan, conditionEnd, ok := f.planInlineTupleSelection(t, f.localBase)
	if !ok {
		return false, false, nil
	}
	f.planTupleSelectStores(&plan, stores.locals)

	reader := wasm.NewReader(t.body)
	for reader.Offset() < conditionEnd {
		f.wasmPC = f.tracePCBase + uint32(reader.Offset())
		op, err := reader.Byte()
		if err != nil {
			return true, false, err
		}
		if err := f.emitPlain(reader, op); err != nil {
			return true, false, err
		}
	}
	f.wasmPC = f.tracePCBase + uint32(reader.Offset())
	f.emitTupleIfSelect(plan)
	if plan.direct {
		if err := caller.JumpTo(stores.end); err != nil {
			return true, false, err
		}
	}
	f.stats.peep("inline-tuple-if-select")
	return true, plan.direct, nil
}
