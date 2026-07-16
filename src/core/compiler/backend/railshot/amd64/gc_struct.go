//go:build amd64

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// GC helpers reuse the synchronous parked-Go dispatcher. The high dispatch bit
// separates internal helpers from real Wasm function imports and public host
// funcref dispatch. These values are mirrored at the src/wago dispatcher
// boundary; they are compile-only ABI constants, not serialized product data.
const (
	gcStructDispatchBit  uint32 = 1 << 30
	gcStructAllocDefault        = 1
	gcStructGet                 = 2
	gcStructSet                 = 3
)

func (f *fn) emitFB(r *wasm.Reader) error {
	sub, err := r.U32()
	if err != nil {
		return err
	}
	if !f.gcStructHelpers {
		return fmt.Errorf("amd64: unsupported 0xfb opcode %d without staged GC struct helpers", sub)
	}
	switch sub {
	case 1: // struct.new_default typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := stagedStructType(f.m, typeIndex); !ok {
			return fmt.Errorf("amd64: struct.new_default type %d is unavailable", typeIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcStructAllocDefault, []wasm.ValType{wasm.I32}, []wasm.ValType{result})
	case 2: // struct.get typeidx fieldidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		fieldIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := stagedStructField(f.m, typeIndex, fieldIndex)
		if !ok {
			return fmt.Errorf("amd64: struct.get type %d field %d is unavailable", typeIndex, fieldIndex)
		}
		if field.Storage.Packed {
			return fmt.Errorf("amd64: packed struct.get remains outside the staged helper slice")
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcStructGet, []wasm.ValType{object, wasm.I32, wasm.I32}, []wasm.ValType{field.Storage.Val})
	case 5: // struct.set is wired in the next bounded mutation slice.
		return fmt.Errorf("amd64: struct.set remains outside the staged GC helper slice")
	default:
		return fmt.Errorf("amd64: unsupported staged 0xfb opcode %d", sub)
	}
}

func (f *fn) callGCStructHelper(helper uint32, params, results []wasm.ValType) error {
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: params, Results: results}
	return f.callHostSync(int(gcStructDispatchBit|helper), ft)
}

func stagedStructType(m *wasm.Module, typeIndex uint32) (wasm.SubType, bool) {
	if m == nil {
		return wasm.SubType{}, false
	}
	index := typeIndex
	for _, group := range m.Types {
		if index < uint32(len(group.SubTypes)) {
			sub := group.SubTypes[index]
			return sub, sub.Comp.Kind == wasm.CompStruct
		}
		index -= uint32(len(group.SubTypes))
	}
	return wasm.SubType{}, false
}

func stagedStructField(m *wasm.Module, typeIndex, fieldIndex uint32) (wasm.FieldType, bool) {
	sub, ok := stagedStructType(m, typeIndex)
	if !ok || fieldIndex >= uint32(len(sub.Comp.Fields)) {
		return wasm.FieldType{}, false
	}
	return sub.Comp.Fields[fieldIndex], true
}
