package frontend

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// BuildGCTypeDescs lowers decoded Wasm GC recursive type groups into runtime GC
// descriptors. The returned slice is indexed by flattened wasm.TypeIdx.Index.
func BuildGCTypeDescs(m *wasm.Module) ([]gc.TypeDesc, error) {
	metadata, err := BuildGCTypeMetadata(m)
	if err != nil {
		return nil, err
	}
	return metadata.Descs, nil
}

// GCTypeMetadata contains the runtime descriptors and compiler-only layout
// table derived together from one flattened traversal of the type section.
type GCTypeMetadata struct {
	Descs   []gc.TypeDesc
	Layouts []codegen.GCTypeLayout
}

// BuildGCTypeMetadata lowers one module's immutable GC type metadata once.
func BuildGCTypeMetadata(m *wasm.Module) (GCTypeMetadata, error) {
	if m == nil {
		return GCTypeMetadata{}, fmt.Errorf("frontend: nil wasm module")
	}
	return LowerGCTypeMetadata(m.Types)
}

// LowerGCTypeDescs flattens recursive groups in decoder/validator order and
// returns descriptors indexed by the same flattened wasm.TypeIdx.Index values.
// Function types get gc.KindFunc sentinels so later struct/array indexes are not
// shifted. Field mutability affects validation/codegen, not GC layout or scan
// reachability, so it is intentionally not represented in gc.TypeDesc.
func LowerGCTypeDescs(types []wasm.RecType) ([]gc.TypeDesc, error) {
	metadata, err := LowerGCTypeMetadata(types)
	if err != nil {
		return nil, err
	}
	return metadata.Descs, nil
}

// LowerGCTypeMetadata derives runtime and compiler layout metadata in one pass.
func LowerGCTypeMetadata(types []wasm.RecType) (GCTypeMetadata, error) {
	flat, hasLayouts := flattenGCTypes(types)
	descs := make([]gc.TypeDesc, len(flat))
	var layouts []codegen.GCTypeLayout
	if hasLayouts {
		layouts = make([]codegen.GCTypeLayout, len(flat))
	}
	for i, ft := range flat {
		st := ft.Source
		id := gc.TypeID(i)
		resolver := gcTypeResolver{total: len(flat), recBase: ft.RecBase, recLen: ft.RecLen, flat: flat}
		layout := codegen.GCTypeLayout{Type: st, RecBase: uint32(ft.RecBase), RecLen: uint32(ft.RecLen), PointerFree: true, DirectCast: st.Final && (st.Comp.Kind == wasm.CompStruct || st.Comp.Kind == wasm.CompArray), DirectLen: st.Final && st.Comp.Kind == wasm.CompArray}
		var d gc.TypeDesc
		var err error
		switch st.Comp.Kind {
		case wasm.CompFunc:
			d = gc.TypeDesc{ID: id, Kind: gc.KindFunc, Final: st.Final}
		case wasm.CompStruct:
			builder := gc.NewStructDescBuilder(id, len(st.Comp.Fields))
			for j, f := range st.Comp.Fields {
				var field gc.StorageKind
				field, err = lowerGCStorage(f.Storage(), resolver)
				if err != nil {
					return GCTypeMetadata{}, fmt.Errorf("frontend: type %d field %d: %w", i, j, err)
				}
				if err = builder.Add(field); err != nil {
					return GCTypeMetadata{}, fmt.Errorf("frontend: type %d field %d: %w", i, j, err)
				}
			}
			d, err = builder.Finish()
			if err != nil {
				return GCTypeMetadata{}, fmt.Errorf("frontend: type %d struct: %w", i, err)
			}
			d.Final = st.Final
			layout.FieldLayout = make([]codegen.GCFieldLayout, len(d.Fields))
			var slot uint32
			for j, field := range d.Fields {
				align, size := gcStorageLayout(field.Kind)
				collectorRef := field.Kind == gc.StorageRef || field.Kind == gc.StorageRefNull
				mutable := st.Comp.Fields[j].Mut() == wasm.Var
				storage := st.Comp.Fields[j].Storage()
				layout.FieldLayout[j] = codegen.GCFieldLayout{Offset: field.Offset, Size: size, Align: align, Slot: slot, Storage: field.Kind, Mutable: mutable, Nullable: isNullableStorage(storage), CollectorRef: collectorRef, DirectAccess: directStorage(storage), RefClass: gcRefClass(field.Kind), Barrier: gcBarrierClass(collectorRef && mutable)}
				if collectorRef {
					layout.ScanOffsets = append(layout.ScanOffsets, field.Offset)
				}
				slot++
				if size == 16 {
					slot++
				}
			}
			layout.ObjectSize, err = gc.StructSize(d)
			if err != nil {
				return GCTypeMetadata{}, fmt.Errorf("frontend: type %d struct size: %w", i, err)
			}
			layout.Align = d.Align
			layout.PointerFree = d.PointerFree()
			layout.NativeAlloc = nativeStructAllocEligible(st, layout.FieldLayout)
		case wasm.CompArray:
			elem, err := lowerGCStorage(st.Comp.Array.Storage(), resolver)
			if err != nil {
				return GCTypeMetadata{}, fmt.Errorf("frontend: type %d array: %w", i, err)
			}
			d, err = gc.NewArrayDesc(id, elem)
			if err != nil {
				return GCTypeMetadata{}, fmt.Errorf("frontend: type %d array: %w", i, err)
			}
			d.Final = st.Final
			collectorRef := d.ArrayElementsAreRefs()
			mutable := st.Comp.Array.Mut() == wasm.Var
			storage := st.Comp.Array.Storage()
			layout.ElemLayout = codegen.GCFieldLayout{Size: d.ElemSize, Align: d.Align, Storage: elem, Mutable: mutable, Nullable: isNullableStorage(storage), CollectorRef: collectorRef, DirectAccess: directStorage(storage), RefClass: gcRefClass(elem), Barrier: gcBarrierClass(collectorRef && mutable)}
			layout.Align = d.Align
			layout.PointerFree = d.PointerFree()
		default:
			return GCTypeMetadata{}, fmt.Errorf("frontend: type %d has unsupported component kind %d", i, st.Comp.Kind)
		}
		if len(st.Supers) > 0 {
			if len(st.Supers) > 1 {
				return GCTypeMetadata{}, fmt.Errorf("frontend: type %d has multiple supers; runtime descriptor stores one", i)
			}
			super, err := resolver.resolve(st.Supers[0])
			if err != nil {
				return GCTypeMetadata{}, fmt.Errorf("frontend: type %d has invalid super type index %d", i, st.Supers[0].Index)
			}
			d.Super = gc.TypeID(super)
			d.HasSuper = true
		}
		descs[i] = d
		if layouts != nil {
			layouts[i] = layout
		}
	}
	if err := gc.ValidateTypeDescs(descs); err != nil {
		return GCTypeMetadata{}, fmt.Errorf("frontend: lowered GC type descriptors invalid: %w", err)
	}
	return GCTypeMetadata{Descs: descs, Layouts: layouts}, nil
}

func isNullableStorage(st wasm.StorageType) bool {
	return !st.Packed() && st.Val().Kind() == wasm.ValRef && st.Val().Ref().Nullable()
}

func directStorage(st wasm.StorageType) bool {
	return st.Packed() || st.Val().Kind() == wasm.ValNum
}

func gcRefClass(kind gc.StorageKind) codegen.GCRefClass {
	switch kind {
	case gc.StorageRef, gc.StorageRefNull:
		return codegen.GCRefCollector
	case gc.StorageFuncRef, gc.StorageFuncRefNull:
		return codegen.GCRefFunction
	case gc.StorageExternRef, gc.StorageExternRefNull:
		return codegen.GCRefExtern
	default:
		return codegen.GCRefNone
	}
}

func gcBarrierClass(needsBarrier bool) codegen.GCBarrierClass {
	if needsBarrier {
		return codegen.GCBarrierCollector
	}
	return codegen.GCBarrierNone
}

func nativeStructAllocEligible(st *wasm.SubType, fields []codegen.GCFieldLayout) bool {
	if !st.Final || len(fields) != len(st.Comp.Fields) {
		return false
	}
	for i, field := range st.Comp.Fields {
		nextSlot := fields[i].Slot + 1
		if fields[i].Size == 16 {
			nextSlot++
		}
		// One final helper slot carries the type index.
		storage := field.Storage()
		if storage.Packed() || nextSlot+1 > 64 {
			return false
		}
		if storage.Val().Kind() == wasm.ValRef {
			heap := storage.Val().Ref().Heap()
			if heap.Kind() != wasm.HeapAbs || (heap.Abs() != wasm.HeapAny && heap.Abs() != wasm.HeapEq) || fields[i].Size != 4 {
				return false
			}
		} else if storage.Val().Kind() != wasm.ValNum && storage.Val().Kind() != wasm.ValVec {
			return false
		}
	}
	return true
}

func gcStorageLayout(kind gc.StorageKind) (align, size uint32) {
	switch kind {
	case gc.StorageI8:
		return 1, 1
	case gc.StorageI16:
		return 2, 2
	case gc.StorageI32, gc.StorageF32, gc.StorageRef, gc.StorageRefNull:
		return 4, 4
	case gc.StorageI64, gc.StorageF64, gc.StorageFuncRef, gc.StorageFuncRefNull, gc.StorageExternRef, gc.StorageExternRefNull:
		return 8, 8
	case gc.StorageV128:
		return 16, 16
	default:
		panic("frontend: validated GC storage kind has no layout")
	}
}

type flattenedGCType struct {
	Source  *wasm.SubType
	RecBase int
	RecLen  int
}

type gcTypeResolver struct {
	total   int
	recBase int
	recLen  int
	flat    []flattenedGCType
}

func (r gcTypeResolver) resolve(idx wasm.TypeIdx) (uint32, error) {
	if idx.Rec {
		if idx.Index >= uint32(r.recLen) {
			return 0, fmt.Errorf("invalid recursive type index %d", idx.Index)
		}
		return uint32(r.recBase) + idx.Index, nil
	}
	if idx.Index >= uint32(r.total) {
		return 0, fmt.Errorf("invalid type index %d", idx.Index)
	}
	return idx.Index, nil
}

func flattenGCTypes(types []wasm.RecType) (flat []flattenedGCType, hasLayouts bool) {
	total := 0
	for i := range types {
		total += len(types[i].SubTypes)
		for j := range types[i].SubTypes {
			kind := types[i].SubTypes[j].Comp.Kind
			hasLayouts = hasLayouts || kind == wasm.CompStruct || kind == wasm.CompArray
		}
	}
	if total == 0 {
		return nil, false
	}
	flat = make([]flattenedGCType, total)
	at := 0
	for ri := range types {
		rt := &types[ri]
		base := at
		for si := range rt.SubTypes {
			st := &rt.SubTypes[si]
			flat[at] = flattenedGCType{Source: st, RecBase: base, RecLen: len(rt.SubTypes)}
			at++
		}
	}
	return flat, hasLayouts
}

func lowerGCStorage(st wasm.StorageType, resolver gcTypeResolver) (gc.StorageKind, error) {
	if st.Packed() {
		switch st.Pack() {
		case wasm.PackI8:
			return gc.StorageI8, nil
		case wasm.PackI16:
			return gc.StorageI16, nil
		default:
			return 0, fmt.Errorf("unsupported packed storage %d", st.Pack())
		}
	}
	return lowerGCValType(st.Val(), resolver)
}

func lowerGCValType(v wasm.ValType, resolver gcTypeResolver) (gc.StorageKind, error) {
	switch v.Kind() {
	case wasm.ValNum:
		switch v.Num() {
		case wasm.NumI32:
			return gc.StorageI32, nil
		case wasm.NumI64:
			return gc.StorageI64, nil
		case wasm.NumF32:
			return gc.StorageF32, nil
		case wasm.NumF64:
			return gc.StorageF64, nil
		default:
			return 0, fmt.Errorf("unsupported numeric storage %d", v.Num())
		}
	case wasm.ValVec:
		if wasm.EqualValType(v, wasm.V128) {
			return gc.StorageV128, nil
		}
		return 0, fmt.Errorf("unsupported vector storage")
	case wasm.ValRef:
		opaque := gc.StorageKind(0)
		rt := v.Ref()
		heap := rt.Heap()
		if heap.Kind() == wasm.HeapTypeIndex {
			idx, err := resolver.resolve(heap.Type())
			if err != nil {
				return 0, fmt.Errorf("invalid referenced type index %d", heap.Type().Index)
			}
			if int(idx) < len(resolver.flat) && resolver.flat[idx].Source.Comp.Kind == wasm.CompFunc {
				opaque = gc.StorageFuncRef
			}
		} else {
			switch heap.Abs() {
			case wasm.HeapFunc, wasm.HeapNoFunc:
				opaque = gc.StorageFuncRef
			case wasm.HeapExtern, wasm.HeapNoExtern:
				opaque = gc.StorageExternRef
			}
		}
		if opaque != 0 {
			if rt.Nullable() {
				if opaque == gc.StorageFuncRef {
					return gc.StorageFuncRefNull, nil
				}
				return gc.StorageExternRefNull, nil
			}
			return opaque, nil
		}
		if rt.Nullable() {
			return gc.StorageRefNull, nil
		}
		// GC-category references use compact collector handles. i31 immediates and
		// null are ignored by scanning; struct/array/eq/any refs are traced.
		return gc.StorageRef, nil
	case wasm.ValBot:
		return 0, fmt.Errorf("unsupported bottom storage")
	default:
		return 0, fmt.Errorf("unsupported value kind %d", v.Kind())
	}
}
