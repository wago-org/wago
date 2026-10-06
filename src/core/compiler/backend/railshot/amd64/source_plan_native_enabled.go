//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func checkNativeSourceBegin(f *fn, function int, admit bool) {
	owner := shared.BeginIntegerSourcePlan(&f.sc.scalar, f.m, function, admit)
	if owner == nil {
		return
	}
	keep := false
	defer func() {
		if !keep {
			owner.Close()
		}
	}()
	// opEnd rebuilds canonical stack roots for multi-result returns. That
	// rewrite needs its own original-lineage rule, not new expected tags.
	if ft, ok := f.m.LocalFuncType(function); !ok || len(ft.Results) > 1 {
		shared.FailSourcePlanAdapter(owner.Token, regalloccheck.UnsupportedOperation)
		return
	}
	if !shared.ChargeSourcePlanAdapter(owner.Token, 1+owner.Locals, 258+owner.Locals) {
		return
	}
	st := &nativeSourcePlanState{owner: owner, locals: make([]shared.SourceNodeRef, owner.Locals), associations: make(map[*elem]nativeSourceAssociation)}
	if owner.Locals != f.nLocals || len(f.localType) != owner.Locals || f.localBase != 0 {
		shared.FailSourcePlanAdapter(owner.Token, regalloccheck.UnsupportedOperation)
		return
	}
	for i := range st.locals {
		n, ok := shared.SourceEntryNode(owner.Token, i)
		if !ok {
			return
		}
		v, ok := shared.SourceNodeContract(owner.Token, n)
		if !ok {
			return
		}
		if nativeSourceType(v.Type) != f.localType[i] {
			shared.FailSourcePlanAdapter(owner.Token, regalloccheck.InvalidGraph)
			return
		}
		st.locals[i] = n
	}
	st.materialization = shared.BeginSourceMaterializationJournal(owner.Token, 1024)
	if st.materialization == nil {
		return
	}
	f.sourcePlan = st
	st.physical = shared.BeginSourceIntegerPhysical(st.materialization, f.skipFence && f.singleRegResult)
	if st.physical.ObservationReady() {
		f.sourceRestore = f.ObserveScalarGraph(st.physical.ObserveEffect, st.physical.ObserveGPWrites)
	}
	keep = true
}
func nativeSourceType(t wasm.ValType) machineType {
	switch t {
	case wasm.I32:
		return mtI32
	case wasm.I64:
		return mtI64
	}
	return mtNone
}
func (st *nativeSourcePlanState) fail(reason regalloccheck.FailureReason) bool {
	shared.FailSourcePlanAdapter(st.owner.Token, reason)
	return false
}
func nativeSourceActive(f *fn) *nativeSourcePlanState {
	st := f.sourcePlan
	if st == nil || st.owner == nil || st.sealed || shared.SourcePlanStatus(st.owner.Token).Reason != regalloccheck.NoFailure {
		return nil
	}
	return st
}
func (st *nativeSourcePlanState) node(e *elem) (shared.SourceNodeRef, bool) {
	a, ok := st.associations[e]
	if !ok {
		return shared.SourceNodeRef{}, st.fail(regalloccheck.InvalidGraph)
	}
	n, ok := shared.SourceSlotNode(a.ref)
	if !ok {
		return shared.SourceNodeRef{}, st.fail(regalloccheck.InvalidGraph)
	}
	v, ok := shared.SourceNodeContract(st.owner.Token, n)
	if !ok {
		return shared.SourceNodeRef{}, false
	}
	if e == nil || nativeSourceType(v.Type) != e.valueType() {
		return shared.SourceNodeRef{}, st.fail(regalloccheck.InvalidGraph)
	}
	return n, true
}

// Every linked node is charged, even nonlogical children. Fixed limits bound
// malformed links/cycles and output storage without relying on a left-spine walk.
func (st *nativeSourcePlanState) walk(f *fn, roots *[128]*elem, nodes *[128]shared.SourceNodeRef, tagged bool) (int, bool) {
	s := f.s
	if s == nil || s.head == nil || s.head.prev == nil || s.head.next == nil {
		return 0, st.fail(regalloccheck.InvalidGraph)
	}
	count, visited := 0, 0
	previous := s.head
	for e := s.head.next; e != s.head; e = e.next {
		if visited >= 8192 {
			return 0, st.fail(regalloccheck.ResourceLimit)
		}
		if !shared.ChargeSourcePlanAdapter(st.owner.Token, 1, 0) {
			return 0, false
		}
		visited++
		if e == nil || e.prev != previous || e.next == nil {
			return 0, st.fail(regalloccheck.InvalidGraph)
		}
		if e.st.hasLogicalRoot() {
			if count >= len(roots) {
				return 0, st.fail(regalloccheck.ResourceLimit)
			}
			roots[count] = e
			if tagged {
				n, ok := st.node(e)
				if !ok {
					return 0, false
				}
				nodes[count] = n
			}
			count++
		}
		previous = e
	}
	if previous != s.head.prev || count != int(s.logicalDepth) {
		return 0, st.fail(regalloccheck.InvalidGraph)
	}
	return count, true
}
func nativeSourceRule(kind wasm.InstrKind, functionEnd bool) shared.SourceRecipeRule {
	if functionEnd {
		return shared.SourceRuleExit
	}
	switch kind {
	case wasm.InstrLocalGet, wasm.InstrLocalSet, wasm.InstrLocalTee:
		return shared.SourceRuleAlias
	case wasm.InstrI32Const, wasm.InstrI64Const:
		return shared.SourceRuleLiteral
	case wasm.InstrDrop:
		return shared.SourceRuleDrop
	case wasm.InstrNop:
		return shared.SourceRuleNop
	case wasm.InstrI32Add, wasm.InstrI32And, wasm.InstrI32Or, wasm.InstrI32Xor, wasm.InstrI64Add, wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Xor:
		return shared.SourceRuleIntegerBinary
	}
	return 0
}
func nativeSourceOpcode(kind wasm.InstrKind, functionEnd bool) byte {
	if functionEnd {
		return 0x0b
	}
	switch kind {
	case wasm.InstrNop:
		return 0x01
	case wasm.InstrDrop:
		return 0x1a
	case wasm.InstrLocalGet:
		return 0x20
	case wasm.InstrLocalSet:
		return 0x21
	case wasm.InstrLocalTee:
		return 0x22
	case wasm.InstrI32Const:
		return 0x41
	case wasm.InstrI64Const:
		return 0x42
	case wasm.InstrI32Add:
		return 0x6a
	case wasm.InstrI32And:
		return 0x71
	case wasm.InstrI32Or:
		return 0x72
	case wasm.InstrI32Xor:
		return 0x73
	case wasm.InstrI64Add:
		return 0x7c
	case wasm.InstrI64And:
		return 0x83
	case wasm.InstrI64Or:
		return 0x84
	case wasm.InstrI64Xor:
		return 0x85
	}
	return 0
}
func checkNativeSourceBefore(f *fn, pc int, op byte) {
	st := nativeSourceActive(f)
	if st == nil {
		return
	}
	if st.pending {
		st.fail(regalloccheck.InvalidGraph)
		return
	}
	event, ok := shared.SourcePlanEvent(st.owner.Token, st.nextEvent)
	if !ok {
		st.fail(regalloccheck.UnsupportedOperation)
		return
	}
	c, ok := shared.SourcePlanEventContract(st.owner.Token, event)
	if !ok {
		return
	}
	if c.Event.PC != pc || nativeSourceOpcode(c.Event.Kind, c.Event.FunctionEnd) != op || f.localBase != 0 {
		st.fail(regalloccheck.UnsupportedOperation)
		return
	}
	count, ok := st.walk(f, &st.roots, &st.nodes, true)
	if !ok {
		return
	}
	if count < c.Event.InputCount {
		st.fail(regalloccheck.InvalidGraph)
		return
	}
	rule := nativeSourceRule(c.Event.Kind, c.Event.FunctionEnd)
	recipe, ok := shared.BeginSourceRecipe(st.owner.Token, rule, event, st.nodes[count-c.Event.InputCount:count])
	if !ok {
		return
	}
	st.contract, st.rule, st.recipe = c, rule, recipe
	st.count, st.start, st.pending, st.selected = count, f.a.Len(), true, false
}
func checkNativeSourceGet(f *fn, x uint32) {
	st := nativeSourceActive(f)
	if st == nil {
		return
	}
	if !st.pending || st.contract.Event.Kind != wasm.InstrLocalGet || st.selected {
		st.fail(regalloccheck.InvalidGraph)
		return
	}
	if f.localBase != 0 || x != st.contract.Event.Index || int64(x) >= int64(len(st.locals)) || int64(x) >= int64(len(f.localType)) {
		st.fail(regalloccheck.InvalidGraph)
		return
	}
	expected, ok := shared.SourceRecipeOutput(st.recipe, 0)
	if !ok {
		return
	}
	if st.locals[x] != expected || f.localType[x] != nativeSourceType(st.contract.Outputs[0].Type) {
		st.fail(regalloccheck.InvalidGraph)
		return
	}
	st.selected = true
}
func checkNativeSourceSet(f *fn, x int, tee bool, e *elem) {
	st := nativeSourceActive(f)
	if st == nil {
		return
	}
	if !st.pending || st.selected || (st.contract.Event.Kind != wasm.InstrLocalSet && st.contract.Event.Kind != wasm.InstrLocalTee) {
		st.fail(regalloccheck.InvalidGraph)
		return
	}
	// Correct production set/get fusion is unsupported, not a bad machine verdict.
	if tee != (st.contract.Event.Kind == wasm.InstrLocalTee) {
		st.fail(regalloccheck.UnsupportedOperation)
		return
	}
	if f.localBase != 0 || x < 0 || x >= len(st.locals) || x >= len(f.localType) || uint32(x) != st.contract.Event.Index || st.count == 0 || e != st.roots[st.count-1] {
		st.fail(regalloccheck.InvalidGraph)
		return
	}
	n, ok := st.node(e)
	if !ok {
		return
	}
	if n != st.nodes[st.count-1] || f.localType[x] != e.valueType() {
		st.fail(regalloccheck.InvalidGraph)
		return
	}
	st.locals[x], st.selected = n, true
}
func nativeSourceBinary(kind wasm.InstrKind) wOp {
	switch kind {
	case wasm.InstrI32Add, wasm.InstrI64Add:
		return opAdd
	case wasm.InstrI32And, wasm.InstrI64And:
		return opAnd
	case wasm.InstrI32Or, wasm.InstrI64Or:
		return opOr
	case wasm.InstrI32Xor, wasm.InstrI64Xor:
		return opXor
	}
	return 0
}
func (st *nativeSourcePlanState) bind(e *elem, n shared.SourceNodeRef) bool {
	a, exists := st.associations[e]
	if !exists {
		if st.nextSlot >= 4096 || !shared.ChargeSourcePlanAdapter(st.owner.Token, 1, 1) {
			return st.fail(regalloccheck.ResourceLimit)
		}
		a.slot = st.nextSlot
		st.nextSlot++
	}
	ref, ok := shared.BindSourceSlot(st.owner.Token, a.slot, n)
	if !ok {
		return false
	}
	a.ref = ref
	st.associations[e] = a
	return true
}
func checkNativeSourceAfter(f *fn, endPC int) {
	st := nativeSourceActive(f)
	if st == nil {
		return
	}
	if !st.pending {
		st.fail(regalloccheck.UnsupportedOperation)
		return
	}
	c := st.contract
	if c.Event.EndPC != endPC {
		st.fail(regalloccheck.UnsupportedOperation)
		return
	}
	var roots [128]*elem
	var nodes [128]shared.SourceNodeRef
	count, ok := st.walk(f, &roots, &nodes, false)
	if !ok {
		return
	}
	prefix := st.count - c.Event.InputCount
	if count != prefix+c.Event.OutputCount {
		st.fail(regalloccheck.InvalidGraph)
		return
	}
	for i := 0; i < prefix; i++ {
		if roots[i] != st.roots[i] {
			st.fail(regalloccheck.InvalidGraph)
			return
		}
		n, ok := st.node(roots[i])
		if !ok {
			return
		}
		if n != st.nodes[i] {
			st.fail(regalloccheck.InvalidGraph)
			return
		}
	}
	if (c.Event.Kind == wasm.InstrLocalGet || c.Event.Kind == wasm.InstrLocalSet || c.Event.Kind == wasm.InstrLocalTee) && !st.selected {
		st.fail(regalloccheck.UnsupportedOperation)
		return
	}
	if c.Event.OutputCount == 1 {
		e := roots[prefix]
		if e.valueType() != nativeSourceType(c.Outputs[0].Type) {
			st.fail(regalloccheck.InvalidGraph)
			return
		}
		switch st.rule {
		case shared.SourceRuleIntegerBinary:
			if !e.isDeferred() {
				st.fail(regalloccheck.UnsupportedOperation)
				return
			}
			if _, exists := st.associations[e]; exists {
				st.fail(regalloccheck.UnsupportedOperation)
				return
			}
			if e.deferredOp() != nativeSourceBinary(c.Event.Kind) || e.arg0 != st.roots[st.count-2] || e.arg1 != st.roots[st.count-1] {
				st.fail(regalloccheck.InvalidGraph)
				return
			}
			for i, child := range []*elem{e.arg0, e.arg1} {
				n, ok := st.node(child)
				if !ok {
					return
				}
				if n != st.nodes[st.count-2+i] {
					st.fail(regalloccheck.InvalidGraph)
					return
				}
			}
		case shared.SourceRuleLiteral:
			if !e.isValue() || e.st.kind != stConst {
				st.fail(regalloccheck.UnsupportedOperation)
				return
			}
			bits := uint64(e.st.cval)
			if e.valueType() == mtI32 {
				bits = uint64(uint32(bits))
			}
			if bits != c.Outputs[0].Bits || c.Outputs[0].BitsHigh != 0 {
				st.fail(regalloccheck.InvalidGraph)
				return
			}
		case shared.SourceRuleAlias:
			if c.Event.Kind == wasm.InstrLocalTee && e != st.roots[st.count-1] {
				st.fail(regalloccheck.InvalidGraph)
				return
			}
			if c.Event.Kind == wasm.InstrLocalGet {
				if _, exists := st.associations[e]; exists {
					st.fail(regalloccheck.UnsupportedOperation)
					return
				}
				if !e.isValue() {
					st.fail(regalloccheck.UnsupportedOperation)
					return
				}
				switch e.st.kind {
				case stLocalRef, stLocalReg:
					if e.st.idx != c.Event.Index {
						st.fail(regalloccheck.InvalidGraph)
						return
					}
				case stConst:
					v := c.Outputs[0]
					bits := uint64(e.st.cval)
					if e.valueType() == mtI32 {
						bits = uint64(uint32(bits))
					}
					if v.Kind != wasm.SourceConstant || bits != v.Bits || v.BitsHigh != 0 {
						st.fail(regalloccheck.InvalidGraph)
						return
					}
				default:
					// Interval-register forwarding needs a separately audited local
					// selection/materialization bridge; a register is not an ID.
					st.fail(regalloccheck.UnsupportedOperation)
					return
				}
			}
		}
		n, ok := shared.SourceRecipeOutput(st.recipe, 0)
		if !ok {
			return
		}
		if !st.bind(e, n) {
			return
		}
	}
	if f.a.Len() < st.start {
		st.fail(regalloccheck.UnsupportedOperation)
		return
	}
	if !shared.CommitSourceRecipe(st.recipe, st.start, f.a.Len()) {
		return
	}
	st.pending = false
	st.nextEvent++
	if c.Event.FunctionEnd {
		if st.nextEvent != st.owner.Events {
			st.fail(regalloccheck.UnsupportedOperation)
			return
		}
		st.owner.Seal()
		st.sealed = true
	}
}
