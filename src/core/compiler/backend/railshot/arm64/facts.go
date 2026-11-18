// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

// valueFacts is bounded semantic provenance carried directly by a Valent node.
// It shares one metadata byte with GC/EH root state, so facts neither allocate
// nor enlarge the operand-stack arena. Facts describe the value, not its current
// register or slot, and therefore survive materialization and spills.
type valueFacts uint8

const (
	factUpper32Zero valueFacts = 1 << iota
	factBoolean
	storageGCRoot
	storageEHRoot
	factNonZero
	storageCapturedBounds // stReg contains an unchanged i32 address with a proof in cval
	valueFactMask         = factUpper32Zero | factBoolean | factNonZero
	localFactEpochStep    = valueFacts(1 << 5)
	localFactEpochMask    = valueFacts(7 << 5)
)

func (facts valueFacts) has(want valueFacts) bool { return facts&want == want }

func (st storage) valueFacts() valueFacts { return valueFacts(st.meta) & valueFactMask }

func (st *storage) setValueFacts(facts valueFacts) {
	st.meta = st.meta&^uint8(valueFactMask) | uint8(facts&valueFactMask)
}

func (st storage) hasGCRoot() bool { return valueFacts(st.meta).has(storageGCRoot) }

func (st *storage) setGCRoot(root bool) {
	if root {
		st.meta |= uint8(storageGCRoot)
	} else {
		st.meta &^= uint8(storageGCRoot)
	}
}

func (st storage) hasEHRoot() bool { return valueFacts(st.meta).has(storageEHRoot) }

func (st *storage) setEHRoot(root bool) {
	if root {
		st.meta |= uint8(storageEHRoot)
	} else {
		st.meta &^= uint8(storageEHRoot)
	}
}

func (f *fn) factsForLocal(x int) valueFacts {
	// A serialized i32 parameter can arrive in a 64-bit carrier with high bits
	// set. Declared i32 locals have a stronger representation invariant: their
	// zero initializer is canonical, and setLocal canonicalizes any assignment
	// whose producer does not already write a W register. That invariant survives
	// control joins without assignment-version sidecars.
	if uint(x) >= uint(len(f.locals)) {
		return 0
	}
	facts := valueFacts(0)
	if f.canonicalI32Local(x) {
		facts |= factUpper32Zero
	}
	if f.localFactsEnabled {
		localFacts := f.locals[x].facts
		if localFacts&localFactEpochMask == f.localFactEpoch {
			facts |= localFacts & valueFactMask
		}
	}
	if f.inlineLocalNonzero(x) {
		facts |= factNonZero
	}
	return facts
}

func (f *fn) declaredI32Local(x int) bool {
	return f.opt(optValueFacts) && x >= f.nParams && uint(x) < uint(len(f.localType)) && f.localType[x] == mtI32
}

func (f *fn) canonicalI32Local(x int) bool {
	return f.declaredI32Local(x) || uint(x) < 64 && f.canonicalI32Params&(uint64(1)<<uint(x)) != 0
}

func (f *fn) canonicalizeI32LocalAssignment(x int, reg Reg, facts valueFacts) {
	if f.canonicalI32Local(x) && !facts.has(factUpper32Zero) {
		f.a.MovReg32(reg, reg)
		f.stats.peep("local-i32-canonicalize")
	}
}

func canonicalI32ParamMask(hints *funcHintView, localTypes []machineType, nParams int, enabled bool) uint64 {
	if !enabled || !hints.flags.has(hintTouchesMemory) {
		return 0
	}
	var mask uint64
	for i := 0; i < min(nParams, min(len(localTypes), min(len(hints.localScore), 64))); i++ {
		// One entry canonicalization replaces at least two direct scalar-load
		// address canonicalizations. Single-use parameters retain per-use lowering.
		if localTypes[i] == mtI32 && hints.localScore[i]&localScoreParamAddressReuse != 0 {
			mask |= uint64(1) << uint(i)
		}
	}
	return mask
}

func (f *fn) setFactsForLocal(x int, facts valueFacts) {
	if f.localFactsEnabled && uint(x) < uint(len(f.locals)) {
		f.locals[x].facts = facts&valueFactMask&^factNonZero | f.localFactEpoch
	}
}

// clearLocalValueFacts ends the current straight-line assignment versions.
// A structured join can be reached by an edge compiled before the current
// fallthrough, so facts from only the fallthrough cannot survive that join.
// Roots are separate storage bits and remain untouched.
func (f *fn) clearLocalValueFacts() {
	if !f.localFactsEnabled {
		return
	}
	f.localFactEpoch = (f.localFactEpoch + localFactEpochStep) & localFactEpochMask
	if f.localFactEpoch != 0 {
		return
	}
	// Prevent an old generation from becoming current again after the bounded
	// tag wraps. This full clear occurs only once per eight structured joins.
	for i := range f.locals {
		f.locals[i].facts = 0
	}
}

func (f *fn) applyFactsForLocal(e *elem, x int) {
	facts := f.factsForLocal(x)
	e.st.setValueFacts(facts)
	if f.localFactsEnabled && uint(x) < uint(len(f.locals)) {
		localFacts := f.locals[x].facts
		if localFacts&localFactEpochMask == f.localFactEpoch && localFacts&valueFactMask != 0 {
			f.stats.peep("local-fact")
		}
	}
}

const deferredFactI32Only valueFacts = 1 << 7

// Unknown operations retain no facts. The extra table bit restricts ordinary
// integer results to W-register lowering and never enters operand metadata.
var deferredOpFacts = [256]valueFacts{
	opAdd:    factUpper32Zero | deferredFactI32Only,
	opSub:    factUpper32Zero | deferredFactI32Only,
	opAnd:    factUpper32Zero | deferredFactI32Only,
	opOr:     factUpper32Zero | deferredFactI32Only,
	opXor:    factUpper32Zero | deferredFactI32Only,
	opShl:    factUpper32Zero | deferredFactI32Only,
	opShrU:   factUpper32Zero | deferredFactI32Only,
	opShrS:   factUpper32Zero | deferredFactI32Only,
	opRotl:   factUpper32Zero | deferredFactI32Only,
	opRotr:   factUpper32Zero | deferredFactI32Only,
	opMul:    factUpper32Zero | deferredFactI32Only,
	opDivU:   factUpper32Zero | deferredFactI32Only,
	opDivS:   factUpper32Zero | deferredFactI32Only,
	opRemU:   factUpper32Zero | deferredFactI32Only,
	opRemS:   factUpper32Zero | deferredFactI32Only,
	opClz:    factUpper32Zero | deferredFactI32Only,
	opCtz:    factUpper32Zero | deferredFactI32Only,
	opPopcnt: factUpper32Zero | deferredFactI32Only,
	opWrap:   factUpper32Zero,
	opZExt32: factUpper32Zero,
	opEq:     factUpper32Zero | factBoolean,
	opNe:     factUpper32Zero | factBoolean,
	opLtS:    factUpper32Zero | factBoolean,
	opLtU:    factUpper32Zero | factBoolean,
	opGtS:    factUpper32Zero | factBoolean,
	opGtU:    factUpper32Zero | factBoolean,
	opLeS:    factUpper32Zero | factBoolean,
	opLeU:    factUpper32Zero | factBoolean,
	opGeS:    factUpper32Zero | factBoolean,
	opGeU:    factUpper32Zero | factBoolean,
	opEqz:    factUpper32Zero | factBoolean,
	opFEq:    factUpper32Zero | factBoolean,
	opFNe:    factUpper32Zero | factBoolean,
	opFLt:    factUpper32Zero | factBoolean,
	opFGt:    factUpper32Zero | factBoolean,
	opFLe:    factUpper32Zero | factBoolean,
	opFGe:    factUpper32Zero | factBoolean,
}

// deferredResultFacts returns only properties guaranteed by the Wasm operation
// and ARM64's W-register write semantics. Reads from locals, globals, memory, or
// unknown calls begin with no facts and cannot enter through this function.
func deferredResultFacts(op wOp, typ machineType) valueFacts {
	facts := deferredOpFacts[op]
	if typ != mtI32 && facts&deferredFactI32Only != 0 {
		return 0
	}
	return facts &^ deferredFactI32Only
}
