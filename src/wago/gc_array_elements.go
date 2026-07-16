package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

const maxGCArrayElementValues = 2

// gcArrayElementInit is compile-only metadata for the exact passive GC element
// segment in the pinned reference-array product. It is deliberately separate
// from ordinary table element metadata and immutable-global roots.
type gcArrayElementInit struct {
	SegmentIndex uint32
	TypeID       uint32
	Count        uint8
	Values       [maxGCArrayElementValues]gcArrayElementValueInit
}

type gcArrayElementValueInit struct {
	Mode   gcArrayGlobalInitMode
	Length uint32
	Bits   [maxGCArrayFixedElements]uint64
}

// gcArrayElementState is the bounded per-instance lifecycle for one passive GC
// element segment. Refs are rooted through collector table slots until elem.drop
// withdraws them; Descriptor is the arena-owned {ptr,len,pad} record seen by JIT
// code and contains no Go pointer.
type gcArrayElementState struct {
	Descriptor []byte
	Refs       [maxGCArrayElementValues]gc.Ref
	Slots      [maxGCArrayElementValues]uint32
	Count      uint8
}

func stagedGCArrayElementInitializer(m *wasm.Module) (*gcArrayElementInit, error) {
	if m == nil {
		return nil, fmt.Errorf("nil module")
	}
	if len(m.Elements) != 1 {
		return nil, fmt.Errorf("reference array product requires one element segment, got %d", len(m.Elements))
	}
	e := &m.Elements[0]
	if e.Mode.Kind != wasm.ElemPassive || e.Kind.Kind != wasm.ElemTypedExprs || len(e.Kind.Exprs) != maxGCArrayElementValues {
		return nil, fmt.Errorf("reference array product requires one passive typed segment with two expressions")
	}
	if e.Kind.Ref.Nullable || e.Kind.Ref.Exact || e.Kind.Ref.Heap.Kind != wasm.HeapTypeIndex || e.Kind.Ref.Heap.Type.Index != 0 {
		return nil, fmt.Errorf("reference array product segment type must be non-null type 0")
	}
	sub, ok := stagedGCStructSubtype(m, 0)
	if !ok || sub.Comp.Kind != wasm.CompArray || !sub.Comp.Array.Storage.Packed || sub.Comp.Array.Storage.Pack != wasm.PackI8 || sub.Comp.Array.Mut != wasm.Const {
		return nil, fmt.Errorf("reference array product type 0 must be immutable i8 array")
	}
	init := &gcArrayElementInit{SegmentIndex: 0, TypeID: 0, Count: maxGCArrayElementValues}
	for i := range e.Kind.Exprs {
		value, err := decodeStagedGCArrayElementValue(e.Kind.Exprs[i], uint32(i))
		if err != nil {
			return nil, fmt.Errorf("element expression %d: %w", i, err)
		}
		init.Values[i] = value
	}
	return init, nil
}

func decodeStagedGCArrayElementValue(expr wasm.Expr, index uint32) (gcArrayElementValueInit, error) {
	body := expr.BodyBytes
	if len(body) == 0 {
		encoded, err := wasm.EncodeExpr(expr)
		if err != nil {
			return gcArrayElementValueInit{}, err
		}
		body = encoded
	}
	r := wasm.NewReader(body)
	values := make([]uint64, 0, maxGCArrayFixedElements)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return gcArrayElementValueInit{}, err
		}
		if op == 0x41 {
			v, err := r.I32()
			if err != nil {
				return gcArrayElementValueInit{}, err
			}
			values = append(values, uint64(uint32(v)))
			if len(values) > maxGCArrayFixedElements {
				return gcArrayElementValueInit{}, fmt.Errorf("operand count exceeds %d", maxGCArrayFixedElements)
			}
			continue
		}
		if op != 0xfb {
			return gcArrayElementValueInit{}, fmt.Errorf("unsupported operand opcode 0x%02x", op)
		}
		subopcode, err := r.U32()
		if err != nil {
			return gcArrayElementValueInit{}, err
		}
		typeID, err := r.U32()
		if err != nil {
			return gcArrayElementValueInit{}, err
		}
		if typeID != 0 {
			return gcArrayElementValueInit{}, fmt.Errorf("constructor type = %d, want 0", typeID)
		}
		var out gcArrayElementValueInit
		switch subopcode {
		case 6: // array.new: i32 value, i32 length
			if index != 0 || len(values) != 2 {
				return gcArrayElementValueInit{}, fmt.Errorf("array.new requires the first expression and two i32 operands")
			}
			out.Mode = gcArrayGlobalInitUniform
			out.Bits[0], out.Length = values[0], uint32(values[1])
		case 8: // array.new_fixed: immediate count, then i32 values
			count, err := r.U32()
			if err != nil {
				return gcArrayElementValueInit{}, err
			}
			if index != 1 || count > maxGCArrayFixedElements || int(count) != len(values) {
				return gcArrayElementValueInit{}, fmt.Errorf("array.new_fixed count %d has %d operands", count, len(values))
			}
			out.Mode, out.Length = gcArrayGlobalInitFixed, count
			copy(out.Bits[:], values)
		default:
			return gcArrayElementValueInit{}, fmt.Errorf("unsupported constructor 0xfb %d", subopcode)
		}
		if out.Length > maxGCArrayFixedElements {
			return gcArrayElementValueInit{}, fmt.Errorf("array length %d exceeds staged bound %d", out.Length, maxGCArrayFixedElements)
		}
		end, err := r.Byte()
		if err != nil || end != 0x0b || r.BytesLeft() != 0 {
			return gcArrayElementValueInit{}, fmt.Errorf("malformed expression end")
		}
		return out, nil
	}
	return gcArrayElementValueInit{}, fmt.Errorf("missing array constructor")
}

func instantiateGCArrayElementSegment(collector *gc.Collector, descs []gc.TypeDesc, init *gcArrayElementInit, descriptor []byte) (*gcArrayElementState, error) {
	if collector == nil || init == nil || init.Count != maxGCArrayElementValues || int(init.TypeID) >= len(descs) {
		return nil, fmt.Errorf("GC array element initializer is unavailable")
	}
	if len(descriptor) != 16 {
		return nil, fmt.Errorf("GC array element descriptor has %d bytes, want 16", len(descriptor))
	}
	desc := descs[init.TypeID]
	if desc.Kind != gc.KindArray || desc.Elem != gc.StorageI8 {
		return nil, fmt.Errorf("GC array element type %d is not i8 array", init.TypeID)
	}
	state := &gcArrayElementState{Descriptor: descriptor, Count: init.Count}
	for i := uint8(0); i < init.Count; i++ {
		value := init.Values[i]
		ref, err := collector.NewArrayDefaultWithRoots(gc.TypeID(init.TypeID), value.Length, gc.EmptyRoots{})
		if err != nil {
			state.drop(collector)
			return nil, err
		}
		switch value.Mode {
		case gcArrayGlobalInitUniform:
			for j := uint32(0); j < value.Length; j++ {
				if err := collector.ArraySet(ref, j, gc.I32Value(int32(value.Bits[0]))); err != nil {
					state.drop(collector)
					return nil, err
				}
			}
		case gcArrayGlobalInitFixed:
			for j := uint32(0); j < value.Length; j++ {
				if err := collector.ArraySet(ref, j, gc.I32Value(int32(value.Bits[j]))); err != nil {
					state.drop(collector)
					return nil, err
				}
			}
		default:
			state.drop(collector)
			return nil, fmt.Errorf("GC array element initializer mode %d is unavailable", value.Mode)
		}
		slot, err := collector.NewCheckedTableSlot(ref)
		if err != nil {
			state.drop(collector)
			return nil, err
		}
		state.Refs[i], state.Slots[i] = ref, slot
	}
	binary.LittleEndian.PutUint32(descriptor[8:], uint32(init.Count))
	return state, nil
}

func (s *gcArrayElementState) drop(collector *gc.Collector) {
	if s == nil || collector == nil {
		return
	}
	if len(s.Descriptor) >= 12 {
		binary.LittleEndian.PutUint32(s.Descriptor[8:], 0)
	}
	for i := uint8(0); i < s.Count; i++ {
		_ = collector.SetTableSlot(s.Slots[i], gc.Null())
	}
}
