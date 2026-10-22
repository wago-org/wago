//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

func externAggregateStorage(kind gc.StorageKind) bool {
	return kind == gc.StorageExternRef || kind == gc.StorageExternRefNull
}
func opaqueAggregateStorage(kind gc.StorageKind) bool {
	return functionStorage(kind) || externAggregateStorage(kind)
}
func (c *Compiled) usesManagedOpaqueStorage() bool {
	if c == nil {
		return false
	}
	if c.usesManagedFunctionStorage() {
		return true
	}
	if !c.compactExternFrameRoots() {
		return false
	}
	for _, desc := range c.GCTypeDescs {
		if desc.Kind == gc.KindArray && externAggregateStorage(desc.Elem) {
			return true
		}
		if desc.Kind == gc.KindStruct {
			for _, field := range desc.Fields {
				if externAggregateStorage(field.Kind) {
					return true
				}
			}
		}
	}
	return false
}
func (in *Instance) managedOpaqueTypedValue(kind gc.StorageKind, want ValueTypeDescriptor, bits uint64) gc.Value {
	if !externAggregateStorage(kind) {
		return in.managedFunctionTypedValue(kind, want, bits)
	}
	if !in.c.compactExternFrameRoots() || want.Kind != ValueTypeReference || want.Ref.Exact || want.Ref.Heap.Defined || want.Ref.Heap.TypeIndex != 0 || (want.Ref.Heap.Abstract != AbstractHeapExtern && want.Ref.Heap.Abstract != AbstractHeapNoExtern) {
		panic(gcHelperFailuref("invalid extern aggregate descriptor"))
	}
	if bits == 0 {
		if kind == gc.StorageExternRef || !want.Ref.Nullable {
			panic(gcHelperFailuref("extern aggregate rejects null"))
		}
		return gc.Value{Kind: kind}
	}
	if want.Ref.Heap.Abstract == AbstractHeapNoExtern {
		panic(gcHelperFailuref("bottom extern aggregate rejects non-null"))
	}
	state, err := in.compactExternCollector(in.gcInvocationDomain())
	if err != nil {
		panic(gcHelperFailure(err))
	}
	word, _, pin, err := state.persistentWord(bits)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	if pin != 0 && !in.refStore.unpinExternref(pin) {
		panic("wago: extern aggregate validation pin changed")
	}
	return gc.Value{Kind: kind, Bits: word}
}
func (a *managedFunctionAggregateScratch) changeOpaque(s *managedFunctionCollector, in *Instance, kind gc.StorageKind, bits uint64, delta int64, allocate bool) {
	if functionStorage(kind) {
		a.change(s, in, bits, delta, allocate)
		return
	}
	if !externAggregateStorage(kind) {
		panic(gcHelperFailuref("invalid opaque aggregate edge kind"))
	}
	if bits == 0 || delta == 0 {
		return
	}
	state, err := in.compactExternCollector(in.gcInvocationDomain())
	if err != nil {
		panic(gcHelperFailure(err))
	}
	collector, ref := compactExternWordRoot(in.refStore, bits)
	if collector != nil {
		if collector != in.gc {
			panic(gcHelperFailuref("extern aggregate edge has a foreign collector"))
		}
	} else if allocate {
		ref, err = state.anyFromForeign(bits, a)
		if err != nil {
			panic(gcHelperFailure(err))
		}
	} else {
		var ok bool
		ref, ok = state.byToken[bits]
		if !ok {
			panic(gcHelperFailuref("extern aggregate word has no traced anchor"))
		}
	}
	a.changeRef(ref, delta)
}
