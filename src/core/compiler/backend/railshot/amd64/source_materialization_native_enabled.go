//go:build amd64 && wago_regalloccheck && !tinygo && !wago_profile

package amd64

import (
	"github.com/wago-org/wago/internal/regalloccheck"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func nativeSourceMaterializationNode(st *nativeSourcePlanState, e *elem) (shared.SourceNodeRef, bool) {
	if e == nil {
		shared.FailSourceMaterialization(st.materialization, regalloccheck.UnsupportedOperation)
		return shared.SourceNodeRef{}, false
	}
	a, ok := st.associations[e]
	if !ok {
		shared.FailSourceMaterialization(st.materialization, regalloccheck.UnsupportedOperation)
		return shared.SourceNodeRef{}, false
	}
	n, ok := shared.SourceSlotNode(a.ref)
	if !ok {
		shared.FailSourceMaterialization(st.materialization, regalloccheck.InvalidGraph)
		return shared.SourceNodeRef{}, false
	}
	v, ok := shared.SourceNodeContract(st.owner.Token, n)
	if !ok {
		return shared.SourceNodeRef{}, false
	}
	if e.valueType() != nativeSourceType(v.Type) {
		shared.FailSourceMaterialization(st.materialization, regalloccheck.UnsupportedOperation)
		return shared.SourceNodeRef{}, false
	}
	return n, true
}

// This hook surrounds the actual defining applyALU call after its children
// materialize. It does not include recursive emission or destination copies.
// The first recipe accepts only RR forms; all other selected forms need their
// own final-byte recipe. Missing receipts never authorize machine Definitions.
func checkNativeSourceALUBefore(f *fn, node, left, right *elem) {
	st := nativeSourceActive(f)
	if st == nil || st.materialization == nil {
		return
	}
	if shared.SourceMaterializationStatus(st.materialization).Reason != regalloccheck.NoFailure {
		return
	}
	if st.materializationToken != (shared.SourceMaterializationToken{}) {
		shared.FailSourceMaterialization(st.materialization, regalloccheck.InvalidGraph)
		return
	}
	if node == nil || !node.isDeferred() || right == nil || !right.isValue() || (right.st.kind != stReg && right.st.kind != stLocalReg) {
		shared.FailSourceMaterialization(st.materialization, regalloccheck.UnsupportedOperation)
		return
	}
	n, ok := nativeSourceMaterializationNode(st, node)
	if !ok {
		return
	}
	l, ok := nativeSourceMaterializationNode(st, left)
	if !ok {
		return
	}
	r, ok := nativeSourceMaterializationNode(st, right)
	if !ok {
		return
	}
	// Original recipe matching below supplies the admitted operator. Validate the
	// actual selected node's operator too, before making its emission claim.
	v, ok := shared.SourceNodeContract(st.owner.Token, n)
	if !ok {
		return
	}
	if nativeSourceType(v.Type) == mtNone || (node.deferredOp() != opAdd && node.deferredOp() != opAnd && node.deferredOp() != opOr && node.deferredOp() != opXor) {
		shared.FailSourceMaterialization(st.materialization, regalloccheck.UnsupportedOperation)
		return
	}
	if st.associations[node].producer == (shared.SourceMaterializationProducer{}) {
		shared.FailSourceMaterialization(st.materialization, regalloccheck.UnsupportedOperation)
		return
	}
	token, ok := shared.BeginSourceMaterialization(st.materialization, st.associations[node].producer, n, []shared.SourceNodeRef{l, r}, nativeSourceMaterializationKind(node), f.a.Len())
	if ok {
		st.materializationToken = token
	}
}
func checkNativeSourceALUAfter(f *fn) {
	st := f.sourcePlan
	if st == nil || st.materialization == nil {
		return
	}
	if st.materializationToken == (shared.SourceMaterializationToken{}) {
		if shared.SourceMaterializationStatus(st.materialization).Reason == regalloccheck.NoFailure {
			shared.FailSourceMaterialization(st.materialization, regalloccheck.UnsupportedOperation)
		}
		return
	}
	token := st.materializationToken
	st.materializationToken = shared.SourceMaterializationToken{}
	shared.CommitSourceMaterialization(st.materialization, token, f.a.Len())
}

func nativeSourceMaterializationKind(node *elem) wasm.InstrKind {
	wide := node.valueType() == mtI64
	switch node.deferredOp() {
	case opAdd:
		if wide {
			return wasm.InstrI64Add
		}
		return wasm.InstrI32Add
	case opAnd:
		if wide {
			return wasm.InstrI64And
		}
		return wasm.InstrI32And
	case opOr:
		if wide {
			return wasm.InstrI64Or
		}
		return wasm.InstrI32Or
	case opXor:
		if wide {
			return wasm.InstrI64Xor
		}
		return wasm.InstrI32Xor
	}
	return wasm.InstrInvalid
}

// Capture before recursive materialization/tree covering can erase children.
func checkNativeSourceProducerBefore(f *fn, node *elem) {
	st := nativeSourceActive(f)
	if st == nil || st.materialization == nil || shared.SourceMaterializationStatus(st.materialization).Reason != regalloccheck.NoFailure {
		return
	}
	if node == nil || !node.isDeferred() {
		shared.FailSourceMaterialization(st.materialization, regalloccheck.UnsupportedOperation)
		return
	}
	n, ok := nativeSourceMaterializationNode(st, node)
	if !ok {
		return
	}
	left, ok := nativeSourceMaterializationNode(st, node.arg0)
	if !ok {
		return
	}
	right, ok := nativeSourceMaterializationNode(st, node.arg1)
	if !ok {
		return
	}
	producer, ok := shared.ExpectSourceMaterializationProducer(st.materialization, n, []shared.SourceNodeRef{left, right}, nativeSourceMaterializationKind(node))
	if !ok {
		return
	}
	a := st.associations[node]
	a.producer = producer
	st.associations[node] = a
}
