// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"

	"github.com/wago-org/wago/internal/buildfeatures"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func gcConstNumericType(kind ValueTypeKind) ValueTypeDescriptor {
	return ValueTypeDescriptor{Kind: kind}
}

func gcConstHeapType(c *Compiled, heap int64, nullable, exact bool) (ValueTypeDescriptor, error) {
	ref := ReferenceTypeDescriptor{Nullable: nullable, Exact: exact}
	if heap >= 0 {
		if c == nil || int(heap) >= len(c.Types) {
			return ValueTypeDescriptor{}, fmt.Errorf("heap type %d is out of range", heap)
		}
		ref.Heap = HeapTypeDescriptor{Defined: true, TypeIndex: uint32(heap)}
		return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ref}, nil
	}
	switch wasm.AbsHeapType(byte(heap & 0x7f)) {
	case wasm.HeapString:
		ref.Heap.Abstract = AbstractHeapString
	case wasm.HeapExn:
		ref.Heap.Abstract = AbstractHeapExn
	case wasm.HeapArray:
		ref.Heap.Abstract = AbstractHeapArray
	case wasm.HeapStruct:
		ref.Heap.Abstract = AbstractHeapStruct
	case wasm.HeapI31:
		ref.Heap.Abstract = AbstractHeapI31
	case wasm.HeapEq:
		ref.Heap.Abstract = AbstractHeapEq
	case wasm.HeapAny:
		ref.Heap.Abstract = AbstractHeapAny
	case wasm.HeapExtern:
		ref.Heap.Abstract = AbstractHeapExtern
	case wasm.HeapFunc:
		ref.Heap.Abstract = AbstractHeapFunc
	case wasm.HeapNone:
		ref.Heap.Abstract = AbstractHeapNone
	case wasm.HeapNoExtern:
		ref.Heap.Abstract = AbstractHeapNoExtern
	case wasm.HeapNoFunc:
		ref.Heap.Abstract = AbstractHeapNoFunc
	case wasm.HeapNoExn:
		ref.Heap.Abstract = AbstractHeapNoExn
	default:
		return ValueTypeDescriptor{}, fmt.Errorf("unsupported heap type %d", heap)
	}
	return ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ref}, nil
}

func (c *Compiled) validateGCConstExpr(expr []byte, want ValueTypeDescriptor, globalLimit int) error {
	if c == nil {
		return fmt.Errorf("nil compiled module")
	}
	r := wasm.NewReader(expr)
	stack := make([]ValueTypeDescriptor, 0, 16)
	pop := func() (ValueTypeDescriptor, error) {
		if len(stack) == 0 {
			return ValueTypeDescriptor{}, fmt.Errorf("stack underflow")
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v, nil
	}
	requireSubtype := func(actual, required ValueTypeDescriptor, context string) error {
		if !valueTypeSubtype(actual, c.Types, required, c.Types) {
			return fmt.Errorf("%s type mismatch", context)
		}
		return nil
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return err
		}
		switch {
		case op == 0x0b:
			if r.BytesLeft() != 0 {
				return fmt.Errorf("instructions follow end")
			}
			if len(stack) != 1 {
				return fmt.Errorf("result stack has %d values", len(stack))
			}
			return requireSubtype(stack[0], want, "result")
		case constExprLowOpcodesForImage&(1<<0x23) != 0 && op == 0x23: // global.get
			idx, err := r.U32()
			if err != nil {
				return err
			}
			if int(idx) >= globalLimit || int(idx) >= len(c.Globals) || c.Globals[idx].Mutable {
				return fmt.Errorf("global.get %d is unavailable or mutable", idx)
			}
			if buildfeatures.SIMD == 0 && c.Globals[idx].Type == ValV128 {
				return checkCompiledFeatures(CoreFeatureSIMD)
			}
			t, err := c.globalExactType(int(idx))
			if err != nil {
				return err
			}
			stack = append(stack, t)
		case constExprNumericOpcodesForImage&(1<<(0x41-0x40)) != 0 && op == 0x41:
			if _, err := r.I32(); err != nil {
				return err
			}
			stack = append(stack, gcConstNumericType(ValueTypeI32))
		case constExprNumericOpcodesForImage&(1<<(0x42-0x40)) != 0 && op == 0x42:
			if _, err := r.I64(); err != nil {
				return err
			}
			stack = append(stack, gcConstNumericType(ValueTypeI64))
		case constExprNumericOpcodesForImage&(1<<(0x43-0x40)) != 0 && op == 0x43:
			if _, err := r.Bytes(4); err != nil {
				return err
			}
			stack = append(stack, gcConstNumericType(ValueTypeF32))
		case constExprNumericOpcodesForImage&(1<<(0x44-0x40)) != 0 && op == 0x44:
			if _, err := r.Bytes(8); err != nil {
				return err
			}
			stack = append(stack, gcConstNumericType(ValueTypeF64))
		case constExprArithmeticOpcodesForImage != 0 && constExprArithmeticOpcodesForImage&(uint64(1)<<(op-0x40)) != 0: // integer add/sub/mul
			if !buildfeatures.ExtendedConstArithmetic {
				return checkCompiledFeatures(CoreFeatureExtendedConst)
			}
			required := gcConstNumericType(ValueTypeI32)
			if op >= 0x7c {
				required = gcConstNumericType(ValueTypeI64)
			}
			right, err := pop()
			if err != nil {
				return err
			}
			left, err := pop()
			if err != nil {
				return err
			}
			if err := requireSubtype(left, required, "integer left operand"); err != nil {
				return err
			}
			if err := requireSubtype(right, required, "integer right operand"); err != nil {
				return err
			}
			stack = append(stack, required)
		case constExprReferenceOpcodesForImage&(1<<(0xfd-0xc0)) != 0 && op == 0xfd:
			if buildfeatures.SIMD == 0 {
				return checkCompiledFeatures(CoreFeatureSIMD)
			}
			if _, err := readV128ConstExpr(r); err != nil {
				return err
			}
			stack = append(stack, gcConstNumericType(ValueTypeV128))
		case constExprReferenceOpcodesForImage&(1<<(0xd0-0xc0)) != 0 && op == 0xd0: // ref.null
			exact, heap, err := r.RefHeapType()
			if err != nil {
				return err
			}
			t, err := gcConstHeapType(c, heap, true, exact)
			if err != nil {
				return err
			}
			stack = append(stack, t)
		case constExprReferenceOpcodesForImage&(1<<(0xd2-0xc0)) != 0 && op == 0xd2: // ref.func
			idx, err := r.U32()
			if err != nil {
				return err
			}
			t, err := c.functionRefExactType(idx)
			if err != nil {
				return err
			}
			t.Ref.Nullable = false
			stack = append(stack, t)
		case constExprReferenceOpcodesForImage&(1<<(0xfb-0xc0)) != 0 && op == 0xfb:
			sub, err := r.U32()
			if err != nil {
				return err
			}
			switch {
			case gcConstOpcodesForImage&(1<<0) != 0 && sub == 0: // struct.new
				typeID, err := r.U32()
				if err != nil {
					return err
				}
				if int(typeID) >= len(c.Types) || c.Types[typeID].Kind != CompositeTypeStruct || int(typeID) >= len(c.GCTypeDescs) || c.GCTypeDescs[typeID].Kind != gc.KindStruct {
					return fmt.Errorf("struct.new type %d is unavailable", typeID)
				}
				fields := c.Types[typeID].Fields
				if len(stack) < len(fields) {
					return fmt.Errorf("struct.new type %d stack underflow", typeID)
				}
				base := len(stack) - len(fields)
				for i := range fields {
					required := fields[i].Storage.Value
					if fields[i].Storage.Packed {
						required = gcConstNumericType(ValueTypeI32)
					}
					if err := requireSubtype(stack[base+i], required, fmt.Sprintf("struct.new type %d field %d", typeID, i)); err != nil {
						return err
					}
				}
				stack = stack[:base]
				stack = append(stack, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: typeID}}})
			case gcConstOpcodesForImage&(1<<1) != 0 && sub == 1: // struct.new_default
				typeID, err := r.U32()
				if err != nil {
					return err
				}
				if int(typeID) >= len(c.Types) || c.Types[typeID].Kind != CompositeTypeStruct || int(typeID) >= len(c.GCTypeDescs) || c.GCTypeDescs[typeID].Kind != gc.KindStruct {
					return fmt.Errorf("struct.new_default type %d is unavailable", typeID)
				}
				for i, field := range c.GCTypeDescs[typeID].Fields {
					if field.Kind == gc.StorageRef || field.Kind == gc.StorageFuncRef || field.Kind == gc.StorageExternRef {
						return fmt.Errorf("struct.new_default type %d field %d is not defaultable", typeID, i)
					}
				}
				stack = append(stack, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: typeID}}})
			case gcConstOpcodesForImage&(1<<6) != 0 && sub == 6 ||
				gcConstOpcodesForImage&(1<<7) != 0 && sub == 7: // array.new / array.new_default
				typeID, err := r.U32()
				if err != nil {
					return err
				}
				if int(typeID) >= len(c.Types) || c.Types[typeID].Kind != CompositeTypeArray || int(typeID) >= len(c.GCTypeDescs) || c.GCTypeDescs[typeID].Kind != gc.KindArray {
					return fmt.Errorf("array constructor type %d is unavailable", typeID)
				}
				length, err := pop()
				if err != nil {
					return err
				}
				if err := requireSubtype(length, gcConstNumericType(ValueTypeI32), "array length"); err != nil {
					return err
				}
				if gcConstOpcodesForImage&(1<<6) != 0 && sub == 6 {
					init, err := pop()
					if err != nil {
						return err
					}
					required := c.Types[typeID].Array.Storage.Value
					if c.Types[typeID].Array.Storage.Packed {
						required = gcConstNumericType(ValueTypeI32)
					}
					if err := requireSubtype(init, required, "array initializer"); err != nil {
						return err
					}
				} else if elem := c.GCTypeDescs[typeID].Elem; elem == gc.StorageRef || elem == gc.StorageFuncRef || elem == gc.StorageExternRef {
					return fmt.Errorf("array.new_default type %d is not defaultable", typeID)
				}
				stack = append(stack, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: typeID}}})
			case gcConstOpcodesForImage&(1<<8) != 0 && sub == 8: // array.new_fixed
				typeID, err := r.U32()
				if err != nil {
					return err
				}
				count, err := r.U32()
				if err != nil {
					return err
				}
				if int(typeID) >= len(c.Types) || c.Types[typeID].Kind != CompositeTypeArray || int(typeID) >= len(c.GCTypeDescs) || c.GCTypeDescs[typeID].Kind != gc.KindArray || len(stack) < int(count) {
					return fmt.Errorf("array.new_fixed type/count %d/%d is unavailable", typeID, count)
				}
				required := c.Types[typeID].Array.Storage.Value
				if c.Types[typeID].Array.Storage.Packed {
					required = gcConstNumericType(ValueTypeI32)
				}
				base := len(stack) - int(count)
				for i := base; i < len(stack); i++ {
					if err := requireSubtype(stack[i], required, "array.new_fixed initializer"); err != nil {
						return err
					}
				}
				stack = stack[:base]
				stack = append(stack, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: typeID}}})
			case gcConstOpcodesForImage&(1<<26) != 0 && sub == 26: // any.convert_extern
				v, err := pop()
				if err != nil {
					return err
				}
				required := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}}
				if err := requireSubtype(v, required, "any.convert_extern operand"); err != nil {
					return err
				}
				stack = append(stack, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: v.Ref.Nullable, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}})
			case gcConstOpcodesForImage&(1<<27) != 0 && sub == 27: // extern.convert_any
				v, err := pop()
				if err != nil {
					return err
				}
				required := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapAny}}}
				if err := requireSubtype(v, required, "extern.convert_any operand"); err != nil {
					return err
				}
				stack = append(stack, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: v.Ref.Nullable, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}})
			case gcConstOpcodesForImage&(1<<28) != 0 && sub == 28: // ref.i31
				v, err := pop()
				if err != nil {
					return err
				}
				if err := requireSubtype(v, gcConstNumericType(ValueTypeI32), "ref.i31 operand"); err != nil {
					return err
				}
				stack = append(stack, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Abstract: AbstractHeapI31}}})
			default:
				return fmt.Errorf("unsupported GC opcode 0xfb %d", sub)
			}
		default:
			return fmt.Errorf("unsupported opcode 0x%02x", op)
		}
	}
	return fmt.Errorf("missing end")
}
