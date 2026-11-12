//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// The declared type of a parameter remains valid after local.set. Restrict the
// proof to absolute type indexes; recursive-local indexes need their own owner.
func staticGCParameterCast(m *wasm.Module, ft *wasm.CompType, local int, sub uint32, heap int64, exact bool) bool {
	if m == nil || ft == nil || local < 0 || local >= len(ft.Params) || heap < 0 || heap > int64(^uint32(0)) {
		return false
	}
	source := ft.Params[local]
	if source.Kind() != wasm.ValRef {
		return false
	}
	ref := source.Ref()
	typ := ref.Heap()
	if typ.Kind() != wasm.HeapTypeIndex || typ.Type().Rec || typ.Type().Index != uint32(heap) ||
		(sub != 22 && sub != 23) || sub == 22 && ref.Nullable() {
		return false
	}
	target, ok := m.TypeSubType(uint32(heap))
	return ok && (target.Comp.Kind == wasm.CompStruct || target.Comp.Kind == wasm.CompArray) &&
		(!exact || target.Final || ref.Exact())
}

// Match a short forwarding body without allocating a plan or changing its
// source reader. It must pass each parameter once, in order, with only proved
// no-op casts before one same-module call. The destination signature is equal.
func (f *fn) gcForwardCallTarget(global uint32, ft *wasm.CompType) (uint32, *wasm.CompType, bool) {
	if !f.opt(optInline) || f.policy.CompactNative || f.moduleEH || profileEnabled || f.inlineDepth != 0 || ft == nil ||
		len(ft.Params) < 1 || len(ft.Params) > 2 || len(ft.Results) != 1 ||
		ft.Params[0].Kind() != wasm.ValRef || len(ft.Params) == 2 && ft.Params[1] != wasm.I32 ||
		ft.Results[0].Kind() != wasm.ValRef && !isIntValType(ft.Results[0]) {
		return 0, nil, false
	}
	local := int(global) - f.importedFunctionCount()
	if local < 0 || local >= len(f.m.Code) {
		return 0, nil, false
	}
	if _, custom := f.customInstructions[global]; custom {
		return 0, nil, false
	}
	body := &f.m.Code[local]
	if len(body.Locals.Runs) != 0 || len(body.BodyBytes) == 0 || len(body.BodyBytes) > 64 {
		return 0, nil, false
	}
	r := wasm.ReaderFrom(body.BodyBytes)
	casts := 0
	for i := range ft.Params {
		op, err := r.Byte()
		if err != nil || op != 0x20 {
			return 0, nil, false
		}
		index, err := r.U32()
		if err != nil || index != uint32(i) {
			return 0, nil, false
		}
		if op, ok := r.Peek(); ok && op == 0xfb {
			_, _ = r.Byte()
			sub, err := r.U32()
			if err != nil || sub != 22 && sub != 23 {
				return 0, nil, false
			}
			heap, exact, err := readRefHeapTypeImmediate(&r)
			if err != nil || !staticGCParameterCast(f.m, ft, i, sub, heap, exact) {
				return 0, nil, false
			}
			casts++
		}
	}
	op, err := r.Byte()
	if err != nil || op != 0x10 || casts == 0 {
		return 0, nil, false
	}
	target, err := r.U32()
	if err != nil || target == global || int(target) < f.importedFunctionCount() {
		return 0, nil, false
	}
	if _, custom := f.customInstructions[target]; custom {
		return 0, nil, false
	}
	next, ok := f.functionSignature(target)
	if !ok || len(next.Params) != len(ft.Params) || len(next.Results) != len(ft.Results) {
		return 0, nil, false
	}
	if !equalGCForwardTypes(ft.Params, next.Params) || !equalGCForwardTypes(ft.Results, next.Results) {
		return 0, nil, false
	}
	op, err = r.Byte()
	return target, next, err == nil && op == 0x0b && r.BytesLeft() == 0
}

func equalGCForwardTypes(source, destination []wasm.ValType) bool {
	for i, typ := range source {
		if !wasm.EqualValType(typ, destination[i]) ||
			typ.Kind() == wasm.ValRef && typ.Ref().Heap().Kind() == wasm.HeapTypeIndex && typ.Ref().Heap().Type().Rec {
			return false
		}
	}
	return true
}
