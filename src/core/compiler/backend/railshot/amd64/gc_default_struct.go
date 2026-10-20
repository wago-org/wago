//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// The existing scalar allocator also handles numeric defaults when its fields
// receive typed positive-zero initializers. Its checked helper remains the
// fallback for an empty reservation, stress collection, and Tiny mode.
func (f *fn) emitNativeDefaultStruct(typeIndex uint32, st wasm.SubType) (bool, error) {
	if !st.Final || len(st.Comp.Fields) > 4 {
		return false, nil
	}
	var params [5]wasm.ValType
	for i, field := range st.Comp.Fields {
		typ := field.Storage().Val()
		if field.Storage().Packed() || typ.Kind() != wasm.ValNum {
			return false, nil
		}
		params[i] = typ
	}
	layout, ok := f.nativeGCStructAllocLayout(typeIndex)
	if !ok || !layout.pointerFree || layout.objectSize > 64 {
		return false, nil
	}
	for _, typ := range params[:len(st.Comp.Fields)] {
		f.pushValue(storage{kind: stConst, typ: mtOf(typ)})
	}
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	params[len(st.Comp.Fields)] = wasm.I32
	f.nativeStructAllocType = typeIndex + 1
	result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
	if err := f.callGCStructHelper(gcStructAllocOne, params[:len(st.Comp.Fields)+1], []wasm.ValType{result}); err != nil {
		return true, err
	}
	f.markTopGCReference()
	f.stats.peep("gc-native-default-struct")
	return true, nil
}
