package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

const (
	maxGCArrayFixedElements    = 4
	maxGCArrayBulkGlobalLength = 12
)

type gcArrayGlobalInitMode uint8

const (
	gcArrayGlobalInitDefault gcArrayGlobalInitMode = iota + 1
	gcArrayGlobalInitUniform
	gcArrayGlobalInitFixed
)

// gcArrayGlobalInit is compile-only metadata for exact staged immutable array
// globals. Codec reload deliberately drops it, including the constructor mode,
// numeric values, and collector-root admission.
type gcArrayGlobalInit struct {
	GlobalIndex uint32
	TypeID      uint32
	Length      uint32
	Mode        gcArrayGlobalInitMode
	Bits        [maxGCArrayFixedElements]uint64
}

func stagedGCArrayGlobalInitializers(m *wasm.Module, product stagedGCArrayProduct) ([]gcArrayGlobalInit, error) {
	if m == nil {
		return nil, fmt.Errorf("nil module")
	}
	imports := m.ImportedGlobalCount()
	out := make([]gcArrayGlobalInit, 0, 2)
	for i, g := range m.Globals {
		if g.Type.Type.Kind != wasm.ValRef || g.Type.Type.Ref.Heap.Kind != wasm.HeapTypeIndex {
			continue
		}
		sub, ok := stagedGCStructSubtype(m, g.Type.Type.Ref.Heap.Type.Index)
		if !ok || sub.Comp.Kind != wasm.CompArray {
			continue
		}
		if g.Type.Mutable && product != stagedGCArrayProductBulkFill && product != stagedGCArrayProductBulkCopy {
			return nil, fmt.Errorf("global %d GC array initializer is mutable", imports+i)
		}
		init, err := decodeStagedGCArrayGlobalInit(m, uint32(imports+i), g)
		if err != nil {
			return nil, fmt.Errorf("global %d GC array initializer: %w", imports+i, err)
		}
		out = append(out, init)
	}
	if len(out) > 2 {
		return nil, fmt.Errorf("GC array global count %d exceeds staged bound 2", len(out))
	}
	return out, nil
}

func decodeStagedGCArrayGlobalInit(m *wasm.Module, globalIndex uint32, g wasm.Global) (gcArrayGlobalInit, error) {
	body := g.Init.BodyBytes
	if len(body) == 0 {
		encoded, err := wasm.EncodeExpr(g.Init)
		if err != nil {
			return gcArrayGlobalInit{}, err
		}
		body = encoded
	}
	r := wasm.NewReader(body)
	values := make([]gcStructConstValue, 0, maxGCArrayFixedElements)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return gcArrayGlobalInit{}, err
		}
		switch op {
		case 0x41:
			v, err := r.I32()
			if err != nil {
				return gcArrayGlobalInit{}, err
			}
			values = append(values, gcStructConstValue{typ: wasm.I32, bits: uint64(uint32(v))})
		case 0x42:
			v, err := r.I64()
			if err != nil {
				return gcArrayGlobalInit{}, err
			}
			values = append(values, gcStructConstValue{typ: wasm.I64, bits: uint64(v)})
		case 0x43:
			b, err := r.Bytes(4)
			if err != nil {
				return gcArrayGlobalInit{}, err
			}
			values = append(values, gcStructConstValue{typ: wasm.F32, bits: uint64(binary.LittleEndian.Uint32(b))})
		case 0x44:
			b, err := r.Bytes(8)
			if err != nil {
				return gcArrayGlobalInit{}, err
			}
			values = append(values, gcStructConstValue{typ: wasm.F64, bits: binary.LittleEndian.Uint64(b)})
		case 0xfb:
			subopcode, err := r.U32()
			if err != nil {
				return gcArrayGlobalInit{}, err
			}
			if subopcode != 6 && subopcode != 7 && subopcode != 8 {
				return gcArrayGlobalInit{}, fmt.Errorf("unsupported GC array constant opcode 0xfb %d", subopcode)
			}
			typeID, err := r.U32()
			if err != nil {
				return gcArrayGlobalInit{}, err
			}
			if g.Type.Type.Ref.Heap.Type.Index != typeID || g.Type.Type.Ref.Nullable {
				return gcArrayGlobalInit{}, fmt.Errorf("result type does not match non-null array type %d", typeID)
			}
			sub, ok := stagedGCStructSubtype(m, typeID)
			if !ok || sub.Comp.Kind != wasm.CompArray {
				return gcArrayGlobalInit{}, fmt.Errorf("type %d is not an array", typeID)
			}
			want := sub.Comp.Array.Storage.Val
			if sub.Comp.Array.Storage.Packed {
				want = wasm.I32
			}
			if want.Kind == wasm.ValRef {
				return gcArrayGlobalInit{}, fmt.Errorf("reference array initializer remains unsupported")
			}
			init := gcArrayGlobalInit{GlobalIndex: globalIndex, TypeID: typeID}
			switch subopcode {
			case 6: // array.new: value, length
				if len(values) != 2 || !wasm.EqualValType(values[0].typ, want) || !wasm.EqualValType(values[1].typ, wasm.I32) {
					return gcArrayGlobalInit{}, fmt.Errorf("array.new operands do not match %s, i32", want)
				}
				init.Mode = gcArrayGlobalInitUniform
				init.Length = uint32(values[1].bits)
				init.Bits[0] = values[0].bits
			case 7: // array.new_default: length
				if len(values) != 1 || !wasm.EqualValType(values[0].typ, wasm.I32) {
					return gcArrayGlobalInit{}, fmt.Errorf("array.new_default requires one i32 length")
				}
				init.Mode = gcArrayGlobalInitDefault
				init.Length = uint32(values[0].bits)
			case 8: // array.new_fixed: immediate count, then values
				count, err := r.U32()
				if err != nil {
					return gcArrayGlobalInit{}, err
				}
				if count > maxGCArrayFixedElements || int(count) != len(values) {
					return gcArrayGlobalInit{}, fmt.Errorf("array.new_fixed count %d has %d operands", count, len(values))
				}
				init.Mode = gcArrayGlobalInitFixed
				init.Length = count
				for i := range values {
					if !wasm.EqualValType(values[i].typ, want) {
						return gcArrayGlobalInit{}, fmt.Errorf("element %d operand type %s, want %s", i, values[i].typ, want)
					}
					init.Bits[i] = values[i].bits
				}
			}
			limit := uint32(maxGCArrayFixedElements)
			if init.Mode == gcArrayGlobalInitDefault || init.Mode == gcArrayGlobalInitUniform {
				limit = maxGCArrayBulkGlobalLength
			}
			if init.Length > limit {
				return gcArrayGlobalInit{}, fmt.Errorf("array global length %d exceeds staged bound %d", init.Length, limit)
			}
			end, err := r.Byte()
			if err != nil || end != 0x0b || r.BytesLeft() != 0 {
				return gcArrayGlobalInit{}, fmt.Errorf("GC array constant expression has malformed end")
			}
			return init, nil
		default:
			return gcArrayGlobalInit{}, fmt.Errorf("unsupported GC array constant operand opcode 0x%02x", op)
		}
		if len(values) > maxGCArrayFixedElements {
			return gcArrayGlobalInit{}, fmt.Errorf("GC array constant operand count exceeds %d", maxGCArrayFixedElements)
		}
	}
	return gcArrayGlobalInit{}, fmt.Errorf("GC array constant expression is missing an array constructor")
}

func (c *Compiled) gcArrayGlobalInit(globalIndex int) (gcArrayGlobalInit, bool) {
	if c == nil || c.memoryDir == nil {
		return gcArrayGlobalInit{}, false
	}
	for _, init := range c.memoryDir.gcArrayGlobals {
		if int(init.GlobalIndex) == globalIndex {
			return init, true
		}
	}
	return gcArrayGlobalInit{}, false
}

func instantiateGCArrayGlobal(collector *gc.Collector, descs []gc.TypeDesc, init gcArrayGlobalInit) (gc.Ref, uint32, error) {
	if collector == nil || int(init.TypeID) >= len(descs) || descs[init.TypeID].Kind != gc.KindArray {
		return gc.Null(), 0, fmt.Errorf("GC array global type %d is unavailable", init.TypeID)
	}
	limit := uint32(maxGCArrayFixedElements)
	if init.Mode == gcArrayGlobalInitDefault || init.Mode == gcArrayGlobalInitUniform {
		limit = maxGCArrayBulkGlobalLength
	}
	if init.Length > limit {
		return gc.Null(), 0, fmt.Errorf("GC array global length %d exceeds staged bound %d", init.Length, limit)
	}
	desc := descs[init.TypeID]
	kind := desc.Elem
	valueKind := kind
	if kind == gc.StorageI8 || kind == gc.StorageI16 {
		valueKind = gc.StorageI32
	}
	if kind == gc.StorageRef || kind == gc.StorageRefNull {
		return gc.Null(), 0, fmt.Errorf("GC array global reference elements remain unsupported")
	}
	ref, err := collector.NewArrayDefaultWithRoots(gc.TypeID(init.TypeID), init.Length, gc.EmptyRoots{})
	if err != nil {
		return gc.Null(), 0, err
	}
	switch init.Mode {
	case gcArrayGlobalInitDefault:
	case gcArrayGlobalInitUniform:
		value := gc.Value{Kind: valueKind, Bits: init.Bits[0]}
		for i := uint32(0); i < init.Length; i++ {
			if err := collector.ArraySet(ref, i, value); err != nil {
				return gc.Null(), 0, err
			}
		}
	case gcArrayGlobalInitFixed:
		for i := uint32(0); i < init.Length; i++ {
			if err := collector.ArraySet(ref, i, gc.Value{Kind: valueKind, Bits: init.Bits[i]}); err != nil {
				return gc.Null(), 0, err
			}
		}
	default:
		return gc.Null(), 0, fmt.Errorf("GC array global initializer mode %d is unavailable", init.Mode)
	}
	// Install the checked collector root before any later global initializer may
	// allocate or collect. The native global cell receives the same stable handle.
	slot, err := collector.NewCheckedGlobalSlot(ref)
	if err != nil {
		return gc.Null(), 0, err
	}
	return ref, slot, nil
}
