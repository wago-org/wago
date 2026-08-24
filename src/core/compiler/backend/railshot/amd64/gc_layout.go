//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *fn) gcTypeLayout(typeIndex uint32, kind wasm.CompTypeKind) (codegen.GCTypeLayout, bool) {
	if int(typeIndex) >= len(f.gcTypeLayouts) {
		return codegen.GCTypeLayout{}, false
	}
	layout := f.gcTypeLayouts[typeIndex]
	return layout, layout.Type != nil && layout.Type.Comp.Kind == kind
}

func (f *fn) stagedGCType(typeIndex uint32) (wasm.SubType, bool) {
	if int(typeIndex) < len(f.gcTypeLayouts) && f.gcTypeLayouts[typeIndex].Type != nil {
		return *f.gcTypeLayouts[typeIndex].Type, true
	}
	return nativeGCFlatType(f.m, typeIndex)
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

func (f *fn) gcStructFieldLayout(typeIndex, fieldIndex uint32) (codegen.GCFieldLayout, bool, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); ok {
		if int(fieldIndex) >= len(layout.FieldLayout) {
			return codegen.GCFieldLayout{}, false, false
		}
		return layout.FieldLayout[fieldIndex], layout.Type.Final, true
	}
	off, size, final, ok := nativeGCStructFieldLayout(f.m, typeIndex, fieldIndex)
	field, found := stagedStructField(f.m, typeIndex, fieldIndex)
	return codegen.GCFieldLayout{Offset: off, Size: size, CollectorRef: found && nativeGCCollectorRefStorage(f.m, typeIndex, field.Storage())}, final, ok
}

type nativeGCStructAllocPlan struct {
	fields      []codegen.GCFieldLayout
	objectSize  uint32
	objectAlign uint32
	pointerFree bool
}

func (f *fn) nativeGCStructAllocLayout(typeIndex uint32) (nativeGCStructAllocPlan, bool) {
	if !f.opt(optGCNativeAlloc) {
		return nativeGCStructAllocPlan{}, false
	}
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); ok {
		if !layout.NativeAlloc {
			return nativeGCStructAllocPlan{}, false
		}
		align := layout.Align
		if align < 8 {
			align = 8
		}
		return nativeGCStructAllocPlan{fields: layout.FieldLayout, objectSize: layout.ObjectSize, objectAlign: align, pointerFree: layout.PointerFree}, true
	}
	fields, size, align, pointerFree, ok := nativeGCStructAllocLayout(f.m, typeIndex)
	if !ok {
		return nativeGCStructAllocPlan{}, false
	}
	layoutFields := make([]codegen.GCFieldLayout, len(fields))
	for i, field := range fields {
		layoutFields[i] = codegen.GCFieldLayout{Offset: field.offset, Size: field.size, Slot: field.slot, Nullable: field.nullable, CollectorRef: field.ref}
	}
	return nativeGCStructAllocPlan{fields: layoutFields, objectSize: size, objectAlign: align, pointerFree: pointerFree}, true
}

func (f *fn) directGCStructLayout(typeIndex, fieldIndex uint32) (uint32, directGCScalar, bool, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompStruct); ok {
		if int(fieldIndex) >= len(layout.FieldLayout) {
			return 0, directGCScalar{}, false, false
		}
		scalar, ok := directGCScalarStorage(layout.Type.Comp.Fields[fieldIndex].Storage())
		if !ok {
			return 0, directGCScalar{}, false, false
		}
		return layout.FieldLayout[fieldIndex].Offset, scalar, layout.Type.Final, true
	}
	return directGCStructLayout(f.m, typeIndex, fieldIndex)
}

func (f *fn) directGCArrayLayout(typeIndex uint32) (directGCScalar, bool, bool) {
	if layout, ok := f.gcTypeLayout(typeIndex, wasm.CompArray); ok {
		scalar, ok := directGCScalarStorage(layout.Type.Comp.Array.Storage())
		return scalar, layout.Type.Final, ok
	}
	return directGCArrayLayout(f.m, typeIndex)
}
