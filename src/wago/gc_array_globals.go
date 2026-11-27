// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type gcArrayGlobalInitMode uint8

const (
	gcArrayGlobalInitDefault gcArrayGlobalInitMode = iota + 1
	gcArrayGlobalInitUniform
	gcArrayGlobalInitFixed
	gcArrayGlobalInitFuncUniform
)

// gcArrayGlobalInit is compile-only metadata for exact staged immutable array
// globals. Codec reload deliberately drops it, including the constructor mode,
// numeric values, and collector-root admission.
type gcArrayGlobalInit struct {
	GlobalIndex uint32
	TypeID      uint32
	Length      uint32
	Mode        gcArrayGlobalInitMode
	Bits        []uint64
}

func stagedGCArrayGlobalInitializers(m *wasm.Module, product stagedGCArrayProduct) ([]gcArrayGlobalInit, error) {
	if m == nil {
		return nil, fmt.Errorf("nil module")
	}
	imports := m.ImportedGlobalCount()
	out := make([]gcArrayGlobalInit, 0, len(m.Globals))
	for i, g := range m.Globals {
		if g.Type.Type.Kind() != wasm.ValRef || g.Type.Type.Ref().Heap().Kind() != wasm.HeapTypeIndex {
			continue
		}
		sub, ok := stagedGCStructSubtype(m, g.Type.Type.Ref().Heap().Type().Index)
		if !ok || sub.Comp.Kind != wasm.CompArray {
			continue
		}
		if g.Type.Mutable && product != stagedGCArrayProductBulkFill && product != stagedGCArrayProductBulkCopy && product != stagedGCArrayProductInitData {
			return nil, fmt.Errorf("global %d GC array initializer is mutable", imports+i)
		}
		init, err := decodeStagedGCArrayGlobalInit(m, product, uint32(imports+i), g)
		if err != nil {
			return nil, fmt.Errorf("global %d GC array initializer: %w", imports+i, err)
		}
		out = append(out, init)
	}
	return out, nil
}

func decodeStagedGCArrayGlobalInit(m *wasm.Module, product stagedGCArrayProduct, globalIndex uint32, g wasm.Global) (gcArrayGlobalInit, error) {
	body := g.Init.BodyBytes
	if len(body) == 0 {
		encoded, err := wasm.EncodeExpr(g.Init)
		if err != nil {
			return gcArrayGlobalInit{}, err
		}
		body = encoded
	}
	r := wasm.NewReader(body)
	values := make([]gcStructConstValue, 0)
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
		case 0xd2: // ref.func
			if product != stagedGCArrayProductInitElem {
				return gcArrayGlobalInit{}, fmt.Errorf("reference array initializer remains unsupported")
			}
			fidx, err := r.U32()
			if err != nil {
				return gcArrayGlobalInit{}, err
			}
			typeIdx, ok := m.FuncTypeIndex(fidx)
			if !ok {
				return gcArrayGlobalInit{}, fmt.Errorf("ref.func index %d is unavailable", fidx)
			}
			values = append(values, gcStructConstValue{typ: wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(typeIdx), false)), bits: uint64(fidx)})
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
			if g.Type.Type.Ref().Heap().Type().Index != typeID || g.Type.Type.Ref().Nullable() {
				return gcArrayGlobalInit{}, fmt.Errorf("result type does not match non-null array type %d", typeID)
			}
			sub, ok := stagedGCStructSubtype(m, typeID)
			if !ok || sub.Comp.Kind != wasm.CompArray {
				return gcArrayGlobalInit{}, fmt.Errorf("type %d is not an array", typeID)
			}
			want := sub.Comp.Array.Storage().Val()
			if sub.Comp.Array.Storage().Packed() {
				want = wasm.I32
			}
			if want.Kind() == wasm.ValRef && product != stagedGCArrayProductInitElem {
				return gcArrayGlobalInit{}, fmt.Errorf("reference array initializer remains unsupported")
			}
			init := gcArrayGlobalInit{GlobalIndex: globalIndex, TypeID: typeID}
			switch subopcode {
			case 6: // array.new: value, length
				if len(values) != 2 || !wasm.EqualValType(values[0].typ, want) || !wasm.EqualValType(values[1].typ, wasm.I32) {
					return gcArrayGlobalInit{}, fmt.Errorf("array.new operands do not match %s, i32", want.String())
				}
				init.Mode = gcArrayGlobalInitUniform
				if want.Kind() == wasm.ValRef {
					init.Mode = gcArrayGlobalInitFuncUniform
				}
				init.Length = uint32(values[1].bits)
				init.Bits = []uint64{values[0].bits}
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
				if uint64(count) > uint64(maxInt()) || int(count) != len(values) {
					return gcArrayGlobalInit{}, fmt.Errorf("array.new_fixed count %d has %d operands", count, len(values))
				}
				init.Mode = gcArrayGlobalInitFixed
				init.Length = count
				init.Bits = make([]uint64, count)
				for i := range values {
					if !wasm.EqualValType(values[i].typ, want) {
						return gcArrayGlobalInit{}, fmt.Errorf("element %d operand type %s, want %s", i, values[i].typ.String(), want.String())
					}
					init.Bits[i] = values[i].bits
				}
			}
			end, err := r.Byte()
			if err != nil || end != 0x0b || r.BytesLeft() != 0 {
				return gcArrayGlobalInit{}, fmt.Errorf("GC array constant expression has malformed end")
			}
			return init, nil
		default:
			return gcArrayGlobalInit{}, fmt.Errorf("unsupported GC array constant operand opcode 0x%02x", op)
		}
	}
	return gcArrayGlobalInit{}, fmt.Errorf("GC array constant expression is missing an array constructor")
}

func (c *Compiled) gcArrayGlobalInit(globalIndex int) (gcArrayGlobalInit, bool) {
	if c == nil || c.memoryDir == nil {
		return gcArrayGlobalInit{}, false
	}
	for _, init := range c.memoryDir.gcArrayGlobals() {
		if int(init.GlobalIndex) == globalIndex {
			return init, true
		}
	}
	return gcArrayGlobalInit{}, false
}

func instantiateGCArrayGlobal(collector *gc.Collector, mapping *gcTypeMapping, descs []gc.TypeDesc, init gcArrayGlobalInit, funcRefDescs []byte) (gc.Ref, uint32, error) {
	if collector == nil || int(init.TypeID) >= len(descs) || descs[init.TypeID].Kind != gc.KindArray {
		return gc.Null(), 0, fmt.Errorf("GC array global type %d is unavailable", init.TypeID)
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
	domainType, err := mappedGCType(mapping, init.TypeID)
	if err != nil {
		return gc.Null(), 0, err
	}
	var ref gc.Ref
	switch init.Mode {
	case gcArrayGlobalInitDefault:
		ref, err = collector.NewArrayDefaultWithRoots(domainType, init.Length, gc.EmptyRoots{})
	case gcArrayGlobalInitUniform:
		ref, err = collector.NewArrayWithRoots(domainType, init.Length, gc.Value{Kind: valueKind, Bits: init.Bits[0]}, gc.EmptyRoots{})
	case gcArrayGlobalInitFixed:
		values := make([]gc.Value, init.Length)
		for i := uint32(0); i < init.Length; i++ {
			values[i] = gc.Value{Kind: valueKind, Bits: init.Bits[i]}
		}
		ref, err = collector.NewArrayFixedWithRoots(domainType, values, gc.EmptyRoots{})
	case gcArrayGlobalInitFuncUniform:
		fidx := int(init.Bits[0])
		off := (fidx + 1) * coreruntime.FuncRefDescBytes
		if (valueKind != gc.StorageFuncRef && valueKind != gc.StorageFuncRefNull) || fidx < 0 || off < coreruntime.FuncRefDescBytes || off+coreruntime.FuncRefDescBytes > len(funcRefDescs) {
			return gc.Null(), 0, fmt.Errorf("GC array global function initializer %d is unavailable", fidx)
		}
		identity := binary.LittleEndian.Uint64(funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:])
		if identity == 0 {
			return gc.Null(), 0, fmt.Errorf("GC array global function initializer %d has no identity", fidx)
		}
		ref, err = collector.NewArrayWithRoots(domainType, init.Length, gc.Value{Kind: valueKind, Bits: identity}, gc.EmptyRoots{})
	default:
		return gc.Null(), 0, fmt.Errorf("GC array global initializer mode %d is unavailable", init.Mode)
	}
	if err != nil {
		return gc.Null(), 0, err
	}
	// Install the checked collector root before any later global initializer may
	// allocate or collect. The native global cell receives the same stable handle.
	slot, err := collector.NewCheckedGlobalSlot(ref)
	if err != nil {
		return gc.Null(), 0, err
	}
	return ref, slot, nil
}
