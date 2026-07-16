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
	gcStructGetS                = 4
	gcStructGetU                = 5
)

func (f *fn) emitFB(r *wasm.Reader) error {
	sub, err := r.U32()
	if err != nil {
		return err
	}
	if sub >= 6 && sub <= 20 {
		return f.emitGCArray(sub, r)
	}
	if sub == 22 || sub == 23 {
		return f.emitGCI31Cast(sub, r)
	}
	if sub >= 28 && sub <= 30 {
		return f.emitGCI31(sub)
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
	case 2, 3, 4: // struct.get / struct.get_s / struct.get_u typeidx fieldidx
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
		helper := uint32(gcStructGet)
		resultType := field.Storage.Val
		if sub == 3 || sub == 4 {
			if !field.Storage.Packed {
				return fmt.Errorf("amd64: struct.get_s/u type %d field %d is not packed", typeIndex, fieldIndex)
			}
			resultType = wasm.I32
			if sub == 3 {
				helper = gcStructGetS
			} else {
				helper = gcStructGetU
			}
		} else if field.Storage.Packed {
			return fmt.Errorf("amd64: plain struct.get cannot access packed type %d field %d", typeIndex, fieldIndex)
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(helper, []wasm.ValType{object, wasm.I32, wasm.I32}, []wasm.ValType{resultType})
	case 5: // struct.set typeidx fieldidx
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
			return fmt.Errorf("amd64: struct.set type %d field %d is unavailable", typeIndex, fieldIndex)
		}
		if field.Mut != wasm.Var {
			return fmt.Errorf("amd64: struct.set type %d field %d is immutable", typeIndex, fieldIndex)
		}
		if field.Storage.Val.Kind == wasm.ValRef {
			return fmt.Errorf("amd64: reference struct.set remains outside the staged helper slice")
		}
		valueType := field.Storage.Val
		if field.Storage.Packed {
			valueType = wasm.I32
		}
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
		f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(fieldIndex)})
		object := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: typeIndex}), false))
		return f.callGCStructHelper(gcStructSet, []wasm.ValType{object, valueType, wasm.I32, wasm.I32}, nil)
	default:
		return fmt.Errorf("amd64: unsupported staged 0xfb opcode %d", sub)
	}
}

func (f *fn) emitGCI31Cast(sub uint32, r *wasm.Reader) error {
	heap, err := r.S33()
	if err != nil {
		return err
	}
	if heap != -20 { // i31
		return fmt.Errorf("amd64: staged ref.cast heap %d is not i31", heap)
	}
	value := f.materialize(f.popValue())
	var nullableDone int
	if sub == 23 {
		f.a.TestSelf(value, true)
		nullableDone = f.a.JccPlaceholder(condE)
	} else {
		f.a.TestSelf(value, true)
		f.trapIf(condE, trapNullReference)
	}
	tag := f.allocReg(maskOf(value))
	f.a.MovRegReg32(tag, value)
	f.a.AluRI(4, tag, 1, false)
	f.a.TestSelf(tag, false)
	f.trapIf(condE, trapIndirectSig)
	f.release(tag)
	if sub == 23 {
		f.a.PatchRel32(nullableDone, f.a.Len())
	}
	f.pushReg(value, mtI64)
	return nil
}

func (f *fn) emitGCI31(sub uint32) error {
	value := f.materialize(f.popValue())
	switch sub {
	case 28: // ref.i31
		f.a.ShiftImm(4, value, 1, false) // low 31 bits << 1; 32-bit write clears the upper half
		f.a.AluRI(1, value, 1, false)    // tag immediate with low bit 1
		f.pushReg(value, mtI64)
	case 29: // i31.get_s
		f.a.TestSelf(value, true)
		f.trapIf(condE, trapNullReference)
		f.a.ShiftImm(7, value, 1, false) // arithmetic shift sign-extends bit 30
		f.pushReg(value, mtI32)
	case 30: // i31.get_u
		f.a.TestSelf(value, true)
		f.trapIf(condE, trapNullReference)
		f.a.ShiftImm(5, value, 1, false)
		f.pushReg(value, mtI32)
	default:
		return fmt.Errorf("amd64: unsupported staged i31 opcode %d", sub)
	}
	return nil
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
