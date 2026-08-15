//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type directGCScalar struct {
	size int
	typ  machineType
}

func directGCScalarStorage(st wasm.StorageType) (directGCScalar, bool) {
	if st.Packed() {
		switch st.Pack() {
		case wasm.PackI8:
			return directGCScalar{size: 1, typ: mtI32}, true
		case wasm.PackI16:
			return directGCScalar{size: 2, typ: mtI32}, true
		default:
			return directGCScalar{}, false
		}
	}
	if st.Val().Kind() != wasm.ValNum {
		return directGCScalar{}, false
	}
	switch st.Val().Num() {
	case wasm.NumI32:
		return directGCScalar{size: 4, typ: mtI32}, true
	case wasm.NumI64:
		return directGCScalar{size: 8, typ: mtI64}, true
	case wasm.NumF32:
		return directGCScalar{size: 4, typ: mtF32}, true
	case wasm.NumF64:
		return directGCScalar{size: 8, typ: mtF64}, true
	default:
		return directGCScalar{}, false
	}
}

func (f *fn) directGCStructLayout(typeIndex, fieldIndex uint32) (codegen.GCFieldLayout, directGCScalar, bool) {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct)
	if !ok || !layout.Type.Final || int(fieldIndex) >= len(layout.FieldLayout) {
		return codegen.GCFieldLayout{}, directGCScalar{}, false
	}
	scalar, ok := directGCScalarStorage(layout.Type.Comp.Fields[fieldIndex].Storage())
	if !ok || layout.FieldLayout[fieldIndex].Size != uint32(scalar.size) {
		return codegen.GCFieldLayout{}, directGCScalar{}, false
	}
	return layout.FieldLayout[fieldIndex], scalar, true
}

func (f *fn) directGCStructRefLayout(typeIndex, fieldIndex uint32) (codegen.GCFieldLayout, bool) {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct)
	if !ok || !layout.Type.Final || int(fieldIndex) >= len(layout.FieldLayout) {
		return codegen.GCFieldLayout{}, false
	}
	field := layout.FieldLayout[fieldIndex]
	return field, field.CollectorRef && field.Size == 4
}

func (f *fn) directGCArrayLayout(typeIndex uint32) (directGCScalar, bool) {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray)
	if !ok || !layout.Type.Final {
		return directGCScalar{}, false
	}
	scalar, ok := directGCScalarStorage(layout.Type.Comp.Array.Storage())
	return scalar, ok && layout.ElemLayout.Size == uint32(scalar.size)
}

func (f *fn) directGCArrayRefLayout(typeIndex uint32) bool {
	layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray)
	return ok && layout.Type.Final && layout.ElemLayout.CollectorRef && layout.ElemLayout.Size == 4
}

func (f *fn) gcTypeLayout(typeIndex uint32, kind wasm.CompTypeKind) (codegen.GCTypeLayout, bool) {
	if int(typeIndex) >= len(f.gcTypeLayouts) {
		return codegen.GCTypeLayout{}, false
	}
	layout := f.gcTypeLayouts[typeIndex]
	return layout, layout.Type != nil && layout.Type.Comp.Kind == kind
}

func (f *fn) stagedStructType(typeIndex uint32) (wasm.SubType, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); ok {
		return *layout.Type, true
	}
	return stagedStructType(f.m, typeIndex)
}

func (f *fn) stagedStructField(typeIndex, fieldIndex uint32) (wasm.FieldType, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); ok {
		if int(fieldIndex) >= len(layout.Type.Comp.Fields) {
			return wasm.FieldType{}, false
		}
		return layout.Type.Comp.Fields[fieldIndex], true
	}
	return stagedStructField(f.m, typeIndex, fieldIndex)
}

func (f *fn) stagedArraySubtype(typeIndex uint32) (wasm.SubType, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray); ok {
		return *layout.Type, true
	}
	return stagedArraySubtype(f.m, typeIndex)
}

func (f *fn) stagedArrayType(typeIndex uint32) (wasm.FieldType, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray); ok {
		return layout.Type.Comp.Array, true
	}
	return stagedArrayType(f.m, typeIndex)
}
