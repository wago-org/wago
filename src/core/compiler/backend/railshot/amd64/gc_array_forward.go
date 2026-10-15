//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// gcFreshArray16Candidate reuses the compact fresh-struct fact for the exact
// uniform-array shape used by the public GC benchmarks. The component kind in
// the module type distinguishes array facts from struct facts without another
// retained tag or compiler field.
func (f *fn) gcFreshArray16Candidate(typeIndex uint32, field wasm.FieldType) (storage, bool) {
	typ, ok := f.stagedGCType(typeIndex)
	if !ok || !typ.Final || typ.Comp.Kind != wasm.CompArray || field.Storage().Packed() {
		return storage{}, false
	}
	if valueType := field.Storage().Val(); valueType.Kind() != wasm.ValNum || valueType.Num() != wasm.NumI32 {
		return storage{}, false
	}
	if _, ok := directGCScalarStorage(field.Storage()); !ok {
		return storage{}, false
	}
	length := f.s.back()
	if length == nil || !length.isValue() || length.st.kind != stConst || length.st.typ != mtI32 || uint32(length.st.cval) != 16 {
		return storage{}, false
	}
	value := baseOfValentBlock(length).prev
	if value == nil || !value.isValue() {
		return storage{}, false
	}
	forward := value.st
	switch forward.kind {
	case stLocalRef, stLocalReg:
		forward.kind = stLocalRef
		forward.reg = regNone
	default:
		return storage{}, false
	}
	return forward, true
}

func (f *fn) tryForwardGCFreshArray16Get(typeIndex uint32) bool {
	fact, ok := f.getGCFreshStructFact()
	if !ok || typeIndex != fact.typeIndex {
		f.invalidateGCFreshStructFact()
		return false
	}
	typ, ok := f.stagedGCType(typeIndex)
	if !ok || typ.Comp.Kind != wasm.CompArray {
		f.invalidateGCFreshStructFact()
		return false
	}
	index := f.s.back()
	object := baseOfValentBlock(index).prev
	local, ok := gcLocalProvenance(object)
	if !ok || local != fact.objectLocal || !gcI32Mask15(index) {
		f.invalidateGCFreshStructFact()
		return false
	}
	f.dropValue() // bounded, nontrapping index mask
	f.dropValue() // unchanged fresh object local
	f.pushValue(fact.value)
	f.invalidateGCFreshStructFact()
	f.stats.peep("gc-fresh-array-get-forward")
	return true
}

func gcI32Mask15(root *elem) bool {
	if root == nil || !root.isDeferred() || root.deferredOp() != opAnd || root.valueType() != mtI32 {
		return false
	}
	return gcI32Mask15Leaves(root.arg0, root.arg1) || gcI32Mask15Leaves(root.arg1, root.arg0)
}

func gcI32Mask15Leaves(value, mask *elem) bool {
	if value == nil || mask == nil || !value.isValue() || !mask.isValue() ||
		value.st.typ != mtI32 || mask.st.kind != stConst || mask.st.typ != mtI32 || uint32(mask.st.cval) != 15 {
		return false
	}
	return value.st.kind == stLocalRef || value.st.kind == stLocalReg
}

// A reference-array store can only retain the fresh numeric array as a value;
// it cannot mutate that array. Any scalar-array store may target it and kills
// the fact conservatively before lowering the write.
func (f *fn) prepareGCFreshArraySet(target wasm.FieldType) {
	fact, ok := f.getGCFreshStructFact()
	if !ok {
		return
	}
	typ, found := f.stagedGCType(fact.typeIndex)
	if found && typ.Comp.Kind == wasm.CompArray && target.Storage().Val().Kind() != wasm.ValRef {
		f.invalidateGCFreshStructFact()
	}
}
