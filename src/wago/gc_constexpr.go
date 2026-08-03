package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

type gcConstStackValue struct {
	bits     uint64
	bitsHi   uint64
	ref      gc.Ref
	isRef    bool
	isExtern bool
}

func gcConstStorageValue(kind gc.StorageKind, value gcConstStackValue) (gc.Value, error) {
	if kind == gc.StorageRef || kind == gc.StorageRefNull {
		if !value.isRef {
			return gc.Value{}, fmt.Errorf("numeric constant used for reference storage")
		}
		return gc.RefValue(value.ref), nil
	}
	if value.isRef {
		return gc.Value{}, fmt.Errorf("reference constant used for numeric storage %d", kind)
	}
	valueKind := kind
	if kind == gc.StorageI8 || kind == gc.StorageI16 {
		valueKind = gc.StorageI32
	}
	return gc.Value{Kind: valueKind, Bits: value.bits, BitsHi: value.bitsHi}, nil
}

func evalCompiledGCConstExpr(expr []byte, collector *gc.Collector, mapping *gcTypeMapping, conversion *gcExternConversionState, c *Compiled, globalCells []*Global, current int, funcRefDescs []byte) (uint64, error) {
	if c == nil {
		return 0, fmt.Errorf("collector-backed constant expression has no compiled module")
	}
	requireCollector := func() error {
		if collector == nil {
			return fmt.Errorf("collector-backed constant expression has no collector")
		}
		return nil
	}
	r := wasm.NewReader(expr)
	stack := make([]gcConstStackValue, 0, 16)
	pop := func() (gcConstStackValue, error) {
		if len(stack) == 0 {
			return gcConstStackValue{}, fmt.Errorf("constant expression stack underflow")
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v, nil
	}
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return 0, err
		}
		switch op {
		case 0x0b:
			if r.BytesLeft() != 0 || len(stack) != 1 || (!stack[0].isRef && !stack[0].isExtern) {
				return 0, fmt.Errorf("GC constant expression result stack has %d value(s)", len(stack))
			}
			if stack[0].isExtern {
				return stack[0].bits, nil
			}
			return uint64(stack[0].ref), nil
		case 0x23: // global.get
			idx, err := r.U32()
			if err != nil || int(idx) >= current || int(idx) >= len(globalCells) || globalCells[idx] == nil {
				return 0, fmt.Errorf("global.get %d is unavailable", idx)
			}
			typ := c.Globals[idx].Type
			if typ == ValV128 {
				vec := readGlobalObjectV128(globalCells[idx])
				stack = append(stack, gcConstStackValue{
					bits:   binary.LittleEndian.Uint64(vec[:8]),
					bitsHi: binary.LittleEndian.Uint64(vec[8:]),
				})
				break
			}
			bits := readGlobalObject(globalCells[idx], typ)
			stack = append(stack, gcConstStackValue{bits: bits, ref: gc.Ref(uint32(bits)), isRef: isGCRefValType(typ), isExtern: typ == ValExternRef})
		case 0x41:
			v, err := r.I32()
			if err != nil {
				return 0, err
			}
			stack = append(stack, gcConstStackValue{bits: uint64(uint32(v))})
		case 0x42:
			v, err := r.I64()
			if err != nil {
				return 0, err
			}
			stack = append(stack, gcConstStackValue{bits: uint64(v)})
		case 0x43:
			b, err := r.Bytes(4)
			if err != nil {
				return 0, err
			}
			stack = append(stack, gcConstStackValue{bits: uint64(binary.LittleEndian.Uint32(b))})
		case 0x44:
			b, err := r.Bytes(8)
			if err != nil {
				return 0, err
			}
			stack = append(stack, gcConstStackValue{bits: binary.LittleEndian.Uint64(b)})
		case 0xfd: // SIMD prefix
			sub, err := r.U32()
			if err != nil {
				return 0, err
			}
			if sub != 12 { // v128.const
				return 0, fmt.Errorf("unsupported SIMD constant expression opcode 0xfd %d", sub)
			}
			b, err := r.Bytes(16)
			if err != nil {
				return 0, err
			}
			stack = append(stack, gcConstStackValue{bits: binary.LittleEndian.Uint64(b[:8]), bitsHi: binary.LittleEndian.Uint64(b[8:])})
		case 0xd0: // ref.null
			heap, err := r.S33()
			if err != nil {
				return 0, err
			}
			if heap == -17 || heap == -14 { // extern / noextern
				stack = append(stack, gcConstStackValue{isExtern: true})
			} else {
				stack = append(stack, gcConstStackValue{ref: gc.Null(), isRef: true})
			}
		case 0xd2: // ref.func
			idx, err := r.U32()
			if err != nil {
				return 0, err
			}
			off := (int(idx) + 1) * 32
			if off < 32 || off+32 > len(funcRefDescs) {
				return 0, fmt.Errorf("ref.func %d descriptor is unavailable", idx)
			}
			return 0, fmt.Errorf("ref.func %d in collector-backed constant expression is not yet supported", idx)
		case 0xfb:
			sub, err := r.U32()
			if err != nil {
				return 0, err
			}
			switch sub {
			case 0: // struct.new
				if err := requireCollector(); err != nil {
					return 0, err
				}
				typeID, err := r.U32()
				if err != nil {
					return 0, err
				}
				if int(typeID) >= len(c.GCTypeDescs) || c.GCTypeDescs[typeID].Kind != gc.KindStruct {
					return 0, fmt.Errorf("struct.new type %d is unavailable", typeID)
				}
				fields := c.GCTypeDescs[typeID].Fields
				if len(stack) < len(fields) {
					return 0, fmt.Errorf("struct.new type %d stack underflow", typeID)
				}
				values := make([]gc.Value, len(fields))
				base := len(stack) - len(fields)
				for i, field := range fields {
					values[i], err = gcConstStorageValue(field.Kind, stack[base+i])
					if err != nil {
						return 0, fmt.Errorf("struct.new type %d field %d: %w", typeID, i, err)
					}
				}
				stack = stack[:base]
				domainType, err := mappedGCType(mapping, typeID)
				if err != nil {
					return 0, err
				}
				ref, err := collector.NewStructWithRoots(domainType, values, gc.EmptyRoots{})
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, isRef: true})
			case 1: // struct.new_default
				if err := requireCollector(); err != nil {
					return 0, err
				}
				typeID, err := r.U32()
				if err != nil {
					return 0, err
				}
				domainType, err := mappedGCType(mapping, typeID)
				if err != nil {
					return 0, err
				}
				ref, err := collector.NewStructDefaultWithRoots(domainType, gc.EmptyRoots{})
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, isRef: true})
			case 6: // array.new
				if err := requireCollector(); err != nil {
					return 0, err
				}
				typeID, err := r.U32()
				if err != nil {
					return 0, err
				}
				lengthValue, err := pop()
				if err != nil {
					return 0, err
				}
				initValue, err := pop()
				if err != nil {
					return 0, err
				}
				if int(typeID) >= len(c.GCTypeDescs) {
					return 0, fmt.Errorf("array.new type %d is unavailable", typeID)
				}
				init, err := gcConstStorageValue(c.GCTypeDescs[typeID].Elem, initValue)
				if err != nil {
					return 0, err
				}
				domainType, err := mappedGCType(mapping, typeID)
				if err != nil {
					return 0, err
				}
				ref, err := collector.NewArrayWithRoots(domainType, uint32(lengthValue.bits), init, gc.EmptyRoots{})
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, isRef: true})
			case 7: // array.new_default
				if err := requireCollector(); err != nil {
					return 0, err
				}
				typeID, err := r.U32()
				if err != nil {
					return 0, err
				}
				lengthValue, err := pop()
				if err != nil {
					return 0, err
				}
				domainType, err := mappedGCType(mapping, typeID)
				if err != nil {
					return 0, err
				}
				ref, err := collector.NewArrayDefaultWithRoots(domainType, uint32(lengthValue.bits), gc.EmptyRoots{})
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, isRef: true})
			case 8: // array.new_fixed
				if err := requireCollector(); err != nil {
					return 0, err
				}
				typeID, err := r.U32()
				if err != nil {
					return 0, err
				}
				count, err := r.U32()
				if err != nil {
					return 0, err
				}
				if int(typeID) >= len(c.GCTypeDescs) || len(stack) < int(count) {
					return 0, fmt.Errorf("array.new_fixed type/count %d/%d is unavailable", typeID, count)
				}
				values := make([]gc.Value, count)
				base := len(stack) - int(count)
				for i := range values {
					values[i], err = gcConstStorageValue(c.GCTypeDescs[typeID].Elem, stack[base+i])
					if err != nil {
						return 0, err
					}
				}
				stack = stack[:base]
				domainType, err := mappedGCType(mapping, typeID)
				if err != nil {
					return 0, err
				}
				ref, err := collector.NewArrayFixedWithRoots(domainType, values, gc.EmptyRoots{})
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, isRef: true})
			case 26: // any.convert_extern
				v, err := pop()
				if err != nil {
					return 0, err
				}
				if !v.isExtern || conversion == nil {
					return 0, fmt.Errorf("any.convert_extern constant has no extern conversion owner")
				}
				bits, err := conversion.anyFromExtern(v.bits)
				if err != nil {
					return 0, err
				}
				if bits>>32 != 0 {
					return 0, fmt.Errorf("any.convert_extern constant produced non-compact anyref %#x", bits)
				}
				stack = append(stack, gcConstStackValue{ref: gc.Ref(uint32(bits)), isRef: true})
			case 27: // extern.convert_any
				v, err := pop()
				if err != nil {
					return 0, err
				}
				if !v.isRef || conversion == nil {
					return 0, fmt.Errorf("extern.convert_any constant has no GC conversion owner")
				}
				bits, err := conversion.externFromAny(uint64(v.ref))
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{bits: bits, isExtern: true})
			case 28: // ref.i31
				v, err := pop()
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: gc.I31New(int32(v.bits)), isRef: true})
			default:
				return 0, fmt.Errorf("unsupported GC constant expression opcode 0xfb %d", sub)
			}
		default:
			return 0, fmt.Errorf("unsupported GC constant expression opcode 0x%02x", op)
		}
	}
	return 0, fmt.Errorf("GC constant expression missing end")
}

func isGCRefValType(t ValType) bool {
	return t == ValAnyRef || t == ValI31Ref
}
