package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

const maxGCStructGlobalFields = 4

// gcStructGlobalInit is compile-only metadata for the exact staged global
// products. Codec reload deliberately drops it, so live collector admission
// cannot be inherited from persisted metadata.
type gcStructGlobalInit struct {
	GlobalIndex uint32
	TypeID      uint32
	FieldCount  uint8
	Default     bool
	Bits        [maxGCStructGlobalFields]uint64
}

type gcGlobalRootMapping struct {
	GlobalIndex uint32
	SlotIndex   uint32
}

type gcStructConstValue struct {
	typ  wasm.ValType
	bits uint64
}

func stagedGCStructGlobalInitializers(m *wasm.Module) ([]gcStructGlobalInit, error) {
	if m == nil {
		return nil, fmt.Errorf("nil module")
	}
	imports := m.ImportedGlobalCount()
	out := make([]gcStructGlobalInit, 0, len(m.Globals))
	for i := range m.Globals {
		g := m.Globals[i]
		if g.Type.Type.Kind != wasm.ValRef || g.Type.Type.Ref.Heap.Kind != wasm.HeapTypeIndex {
			continue
		}
		sub, ok := stagedGCStructSubtype(m, g.Type.Type.Ref.Heap.Type.Index)
		if !ok || sub.Comp.Kind != wasm.CompStruct {
			continue
		}
		if g.Type.Mutable {
			return nil, fmt.Errorf("global %d GC struct initializer is mutable", imports+i)
		}
		init, err := decodeStagedGCStructGlobalInit(m, uint32(imports+i), g)
		if err != nil {
			return nil, fmt.Errorf("global %d GC struct initializer: %w", imports+i, err)
		}
		out = append(out, init)
	}
	if len(out) > 2 {
		return nil, fmt.Errorf("GC struct global count %d exceeds staged bound 2", len(out))
	}
	return out, nil
}

func decodeStagedGCStructGlobalInit(m *wasm.Module, globalIndex uint32, g wasm.Global) (gcStructGlobalInit, error) {
	body := g.Init.BodyBytes
	if len(body) == 0 {
		encoded, err := wasm.EncodeExpr(g.Init)
		if err != nil {
			return gcStructGlobalInit{}, err
		}
		body = encoded
	}
	r := wasm.NewReader(body)
	values := make([]gcStructConstValue, 0, maxGCStructGlobalFields)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return gcStructGlobalInit{}, err
		}
		switch op {
		case 0x41:
			v, err := r.I32()
			if err != nil {
				return gcStructGlobalInit{}, err
			}
			values = append(values, gcStructConstValue{typ: wasm.I32, bits: uint64(uint32(v))})
		case 0x42:
			v, err := r.I64()
			if err != nil {
				return gcStructGlobalInit{}, err
			}
			values = append(values, gcStructConstValue{typ: wasm.I64, bits: uint64(v)})
		case 0x43:
			b, err := r.Bytes(4)
			if err != nil {
				return gcStructGlobalInit{}, err
			}
			values = append(values, gcStructConstValue{typ: wasm.F32, bits: uint64(binary.LittleEndian.Uint32(b))})
		case 0x44:
			b, err := r.Bytes(8)
			if err != nil {
				return gcStructGlobalInit{}, err
			}
			values = append(values, gcStructConstValue{typ: wasm.F64, bits: binary.LittleEndian.Uint64(b)})
		case 0xfb:
			subopcode, err := r.U32()
			if err != nil {
				return gcStructGlobalInit{}, err
			}
			if subopcode != 0 && subopcode != 1 {
				return gcStructGlobalInit{}, fmt.Errorf("unsupported GC constant opcode 0xfb %d", subopcode)
			}
			typeID, err := r.U32()
			if err != nil {
				return gcStructGlobalInit{}, err
			}
			if g.Type.Type.Ref.Heap.Type.Index != typeID || g.Type.Type.Ref.Nullable {
				return gcStructGlobalInit{}, fmt.Errorf("result type does not match non-null struct type %d", typeID)
			}
			st, ok := stagedGCStructSubtype(m, typeID)
			if !ok || st.Comp.Kind != wasm.CompStruct {
				return gcStructGlobalInit{}, fmt.Errorf("type %d is not a struct", typeID)
			}
			if len(st.Comp.Fields) > maxGCStructGlobalFields {
				return gcStructGlobalInit{}, fmt.Errorf("type %d field count %d exceeds staged bound %d", typeID, len(st.Comp.Fields), maxGCStructGlobalFields)
			}
			init := gcStructGlobalInit{GlobalIndex: globalIndex, TypeID: typeID, FieldCount: uint8(len(st.Comp.Fields)), Default: subopcode == 1}
			if init.Default {
				if len(values) != 0 {
					return gcStructGlobalInit{}, fmt.Errorf("struct.new_default has %d operands", len(values))
				}
			} else {
				if len(values) != len(st.Comp.Fields) {
					return gcStructGlobalInit{}, fmt.Errorf("struct.new has %d operands, want %d", len(values), len(st.Comp.Fields))
				}
				for i, field := range st.Comp.Fields {
					want := field.Storage.Val
					if field.Storage.Packed {
						want = wasm.I32
					}
					if want.Kind == wasm.ValRef || !wasm.EqualValType(values[i].typ, want) {
						return gcStructGlobalInit{}, fmt.Errorf("field %d operand type %s, want numeric %s", i, values[i].typ, want)
					}
					init.Bits[i] = values[i].bits
				}
			}
			end, err := r.Byte()
			if err != nil || end != 0x0b || r.BytesLeft() != 0 {
				return gcStructGlobalInit{}, fmt.Errorf("GC struct constant expression has malformed end")
			}
			return init, nil
		default:
			return gcStructGlobalInit{}, fmt.Errorf("unsupported GC constant operand opcode 0x%02x", op)
		}
		if len(values) > maxGCStructGlobalFields {
			return gcStructGlobalInit{}, fmt.Errorf("GC struct constant operand count exceeds %d", maxGCStructGlobalFields)
		}
	}
	return gcStructGlobalInit{}, fmt.Errorf("GC struct constant expression is missing struct.new")
}

func stagedGCStructSubtype(m *wasm.Module, typeIndex uint32) (wasm.SubType, bool) {
	for _, group := range m.Types {
		if typeIndex < uint32(len(group.SubTypes)) {
			return group.SubTypes[typeIndex], true
		}
		typeIndex -= uint32(len(group.SubTypes))
	}
	return wasm.SubType{}, false
}

func (c *Compiled) gcStructGlobalInit(globalIndex int) (gcStructGlobalInit, bool) {
	if c == nil || c.memoryDir == nil {
		return gcStructGlobalInit{}, false
	}
	for _, init := range c.memoryDir.gcStructGlobals {
		if int(init.GlobalIndex) == globalIndex {
			return init, true
		}
	}
	return gcStructGlobalInit{}, false
}

func instantiateGCStructGlobal(collector *gc.Collector, descs []gc.TypeDesc, init gcStructGlobalInit) (gc.Ref, uint32, error) {
	if collector == nil {
		return gc.Null(), 0, fmt.Errorf("GC struct global has no live collector")
	}
	if int(init.TypeID) >= len(descs) || descs[init.TypeID].Kind != gc.KindStruct {
		return gc.Null(), 0, fmt.Errorf("GC struct global type %d is unavailable", init.TypeID)
	}
	ref, err := collector.NewStructDefaultWithRoots(gc.TypeID(init.TypeID), gc.EmptyRoots{})
	if err != nil {
		return gc.Null(), 0, err
	}
	if !init.Default {
		desc := descs[init.TypeID]
		if int(init.FieldCount) != len(desc.Fields) {
			return gc.Null(), 0, fmt.Errorf("GC struct global type %d field count %d != %d", init.TypeID, init.FieldCount, len(desc.Fields))
		}
		for i, field := range desc.Fields {
			kind := field.Kind
			valueKind := kind
			if kind == gc.StorageI8 || kind == gc.StorageI16 {
				valueKind = gc.StorageI32
			}
			if kind == gc.StorageRef || kind == gc.StorageRefNull {
				return gc.Null(), 0, fmt.Errorf("GC struct global reference field %d remains unsupported", i)
			}
			if err := collector.StructSet(ref, uint32(i), gc.Value{Kind: valueKind, Bits: init.Bits[i]}); err != nil {
				return gc.Null(), 0, err
			}
		}
	}
	// Install the collector-owned root immediately. Any later initializer
	// allocation sees this slot through the collector's intrinsic global roots.
	slot, err := collector.NewCheckedGlobalSlot(ref)
	if err != nil {
		return gc.Null(), 0, err
	}
	return ref, slot, nil
}
