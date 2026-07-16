//go:build amd64

package amd64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	gcArrayAllocDefault uint32 = 16
	gcArrayGet          uint32 = 17
	gcArrayGetS         uint32 = 18
	gcArrayGetU         uint32 = 19
	gcArraySet          uint32 = 20
	gcArrayLen          uint32 = 21
	gcArrayAllocFixed   uint32 = 22
)

func (f *fn) emitGCArray(sub uint32, r *wasm.Reader) error {
	if !f.gcArrayHelpers {
		return fmt.Errorf("amd64: unsupported staged array opcode %d without GC array helpers", sub)
	}
	switch sub {
	case 8: // array.new_fixed typeidx length
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		count, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := stagedArrayType(f.m, typeIndex)
		if !ok {
			return fmt.Errorf("amd64: array.new_fixed type %d is unavailable", typeIndex)
		}
		if count > 4 {
			return fmt.Errorf("amd64: array.new_fixed count %d exceeds staged bound 4", count)
		}
		valueType := field.Storage.Val
		if field.Storage.Packed {
			valueType = wasm.I32
		}
		if valueType.Kind == wasm.ValRef {
			return fmt.Errorf("amd64: reference array.new_fixed remains outside the staged helper slice")
		}
		params := make([]wasm.ValType, 0, int(count)+2)
		for i := uint32(0); i < count; i++ {
			params = append(params, valueType)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(count)})
		params = append(params, wasm.I32, wasm.I32)
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArrayAllocFixed, params, []wasm.ValType{result})
	case 7: // array.new_default typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		if _, ok := stagedArrayType(f.m, typeIndex); !ok {
			return fmt.Errorf("amd64: array.new_default type %d is unavailable", typeIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArrayAllocDefault, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{result})
	case 11, 12, 13: // array.get / array.get_s / array.get_u
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := stagedArrayType(f.m, typeIndex)
		if !ok {
			return fmt.Errorf("amd64: array.get type %d is unavailable", typeIndex)
		}
		helper := uint32(gcArrayGet)
		resultType := field.Storage.Val
		if sub == 12 || sub == 13 {
			if !field.Storage.Packed {
				return fmt.Errorf("amd64: array.get_s/u type %d is not packed", typeIndex)
			}
			resultType = wasm.I32
			if sub == 12 {
				helper = gcArrayGetS
			} else {
				helper = gcArrayGetU
			}
		} else if field.Storage.Packed {
			return fmt.Errorf("amd64: plain array.get cannot access packed type %d", typeIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(helper, []wasm.ValType{object, wasm.I32, wasm.I32}, []wasm.ValType{resultType})
	case 14: // array.set typeidx
		typeIndex, err := r.U32()
		if err != nil {
			return err
		}
		field, ok := stagedArrayType(f.m, typeIndex)
		if !ok {
			return fmt.Errorf("amd64: array.set type %d is unavailable", typeIndex)
		}
		if field.Mut != wasm.Var {
			return fmt.Errorf("amd64: array.set type %d is immutable", typeIndex)
		}
		if field.Storage.Val.Kind == wasm.ValRef {
			return fmt.Errorf("amd64: reference array.set remains outside the staged helper slice")
		}
		valueType := field.Storage.Val
		if field.Storage.Packed {
			valueType = wasm.I32
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcArraySet, []wasm.ValType{object, wasm.I32, valueType, wasm.I32}, nil)
	case 15: // array.len
		object := wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapArray), false))
		return f.callGCStructHelper(gcArrayLen, []wasm.ValType{object}, []wasm.ValType{wasm.I32})
	default:
		return fmt.Errorf("amd64: unsupported staged array opcode %d", sub)
	}
}

func stagedArrayType(m *wasm.Module, typeIndex uint32) (wasm.FieldType, bool) {
	if m == nil {
		return wasm.FieldType{}, false
	}
	index := typeIndex
	for _, group := range m.Types {
		if index < uint32(len(group.SubTypes)) {
			sub := group.SubTypes[index]
			return sub.Comp.Array, sub.Comp.Kind == wasm.CompArray
		}
		index -= uint32(len(group.SubTypes))
	}
	return wasm.FieldType{}, false
}
