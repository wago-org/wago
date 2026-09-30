package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

type gcConstValueKind uint8

const (
	gcConstNumeric gcConstValueKind = iota
	gcConstCollectorRef
	gcConstFuncRef
	gcConstExternRef
	gcConstExnRef
)

type gcConstStackValue struct {
	bits   uint64
	bitsHi uint64
	ref    gc.Ref
	kind   gcConstValueKind
}

type gcConstStackRootSet struct {
	stack *[]gcConstStackValue
	extra gc.RootSet
}

type gcConstStackRootSlot struct {
	stack *[]gcConstStackValue
	index int
}

func (s gcConstStackRootSlot) GetRef() gc.Ref { return (*s.stack)[s.index].ref }
func (s gcConstStackRootSlot) SetRef(ref gc.Ref) {
	(*s.stack)[s.index].ref = ref
}

func (s gcConstStackRootSet) RangeRoots(fn func(gc.RootSlot) bool) {
	if s.extra != nil {
		keepGoing := true
		s.extra.RangeRoots(func(slot gc.RootSlot) bool {
			keepGoing = fn(slot)
			return keepGoing
		})
		if !keepGoing {
			return
		}
	}
	if s.stack == nil {
		return
	}
	for i := range *s.stack {
		if (*s.stack)[i].kind == gcConstCollectorRef && !fn(gcConstStackRootSlot{stack: s.stack, index: i}) {
			return
		}
	}
}

func (s gcConstStackRootSet) RangeClassifiedRootRefs(sink gc.ClassifiedRootRefSink) bool {
	if sink == nil {
		return true
	}
	classified := classifiedRootSink{sink: sink, class: gc.RootSnapshotTemporary}
	if s.extra != nil {
		if direct, ok := s.extra.(gc.DirectClassifiedRootRefSet); ok {
			if !direct.RangeClassifiedRootRefs(sink) {
				return false
			}
		} else if direct, ok := s.extra.(gc.DirectRootRefSet); ok {
			if !direct.RangeRootRefs(classified) {
				return false
			}
		} else {
			keepGoing := true
			s.extra.RangeRoots(func(slot gc.RootSlot) bool {
				keepGoing = sink.VisitClassifiedRootRef(gc.RootSnapshotTemporary, slot.GetRef())
				return keepGoing
			})
			if !keepGoing {
				return false
			}
		}
	}
	if s.stack == nil {
		return true
	}
	for i := range *s.stack {
		if (*s.stack)[i].kind == gcConstCollectorRef && !sink.VisitClassifiedRootRef(gc.RootSnapshotTemporary, (*s.stack)[i].ref) {
			return false
		}
	}
	return true
}

// compactRefRootSlot roots an off-heap compact reference in place while an
// instance is still being assembled and its table descriptors are not yet part
// of the collector's permanent root view.
type compactRefRootSlot []byte

func (s compactRefRootSlot) GetRef() gc.Ref {
	return gc.Ref(binary.LittleEndian.Uint32(s))
}
func (s compactRefRootSlot) SetRef(ref gc.Ref) {
	binary.LittleEndian.PutUint32(s, uint32(ref))
}

func gcConstStorageValue(kind gc.StorageKind, value gcConstStackValue) (gc.Value, error) {
	switch kind {
	case gc.StorageRef, gc.StorageRefNull:
		if value.kind != gcConstCollectorRef {
			return gc.Value{}, fmt.Errorf("non-collector constant used for collector-reference storage")
		}
		return gc.RefValue(value.ref), nil
	case gc.StorageFuncRef, gc.StorageFuncRefNull:
		if value.kind != gcConstFuncRef {
			return gc.Value{}, fmt.Errorf("non-function constant used for function-reference storage")
		}
		valueKind := gc.StorageFuncRef
		if value.bits == 0 {
			valueKind = gc.StorageFuncRefNull
		}
		return gc.Value{Kind: valueKind, Bits: value.bits}, nil
	case gc.StorageExternRef, gc.StorageExternRefNull:
		if value.kind != gcConstExternRef {
			return gc.Value{}, fmt.Errorf("non-extern constant used for external-reference storage")
		}
		valueKind := gc.StorageExternRef
		if value.bits == 0 {
			valueKind = gc.StorageExternRefNull
		}
		return gc.Value{Kind: valueKind, Bits: value.bits}, nil
	}
	if value.kind != gcConstNumeric {
		return gc.Value{}, fmt.Errorf("reference constant used for numeric storage %d", kind)
	}
	valueKind := kind
	if kind == gc.StorageI8 || kind == gc.StorageI16 {
		valueKind = gc.StorageI32
	}
	return gc.Value{Kind: valueKind, Bits: value.bits, BitsHi: value.bitsHi}, nil
}

func gcConstReferenceKind(c *Compiled, t ValueTypeDescriptor) (gcConstValueKind, error) {
	if t.Kind != ValueTypeReference {
		return gcConstNumeric, nil
	}
	if t.Ref.Heap.Defined {
		if c == nil || int(t.Ref.Heap.TypeIndex) >= len(c.Types) {
			return 0, fmt.Errorf("reference type %d is unavailable", t.Ref.Heap.TypeIndex)
		}
		switch c.Types[t.Ref.Heap.TypeIndex].Kind {
		case CompositeTypeFunction:
			return gcConstFuncRef, nil
		case CompositeTypeStruct, CompositeTypeArray:
			return gcConstCollectorRef, nil
		default:
			return 0, fmt.Errorf("reference type %d has unsupported kind", t.Ref.Heap.TypeIndex)
		}
	}
	switch t.Ref.Heap.Abstract {
	case AbstractHeapFunc, AbstractHeapNoFunc:
		return gcConstFuncRef, nil
	case AbstractHeapExtern, AbstractHeapNoExtern:
		return gcConstExternRef, nil
	case AbstractHeapExn, AbstractHeapNoExn:
		return gcConstExnRef, nil
	case AbstractHeapAny, AbstractHeapEq, AbstractHeapI31, AbstractHeapStruct, AbstractHeapArray, AbstractHeapNone:
		return gcConstCollectorRef, nil
	default:
		return 0, fmt.Errorf("unsupported abstract reference heap %d", t.Ref.Heap.Abstract)
	}
}

func gcConstHeapKind(c *Compiled, heap int64) (gcConstValueKind, error) {
	if heap >= 0 {
		return gcConstReferenceKind(c, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: uint32(heap)}}})
	}
	switch wasm.AbsHeapType(byte(heap & 0x7f)) {
	case wasm.HeapFunc, wasm.HeapNoFunc:
		return gcConstFuncRef, nil
	case wasm.HeapExtern, wasm.HeapNoExtern:
		return gcConstExternRef, nil
	case wasm.HeapExn, wasm.HeapNoExn:
		return gcConstExnRef, nil
	case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone:
		return gcConstCollectorRef, nil
	default:
		return 0, fmt.Errorf("unsupported ref.null heap type %d", heap)
	}
}

func evalCompiledGCConstExpr(expr []byte, collector *gc.Collector, mapping *gcTypeMapping, conversion *gcExternConversionState, c *Compiled, globalCells []*Global, current int, funcRefDescs []byte, roots gc.RootSet) (uint64, error) {
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
	allocationRoots := gcConstStackRootSet{stack: &stack, extra: roots}
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
			if r.BytesLeft() != 0 || len(stack) != 1 || stack[0].kind == gcConstNumeric {
				return 0, fmt.Errorf("GC constant expression result stack has %d value(s)", len(stack))
			}
			if stack[0].kind == gcConstCollectorRef {
				return uint64(stack[0].ref), nil
			}
			return stack[0].bits, nil
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
			exact, err := c.globalExactType(int(idx))
			if err != nil {
				return 0, err
			}
			kind, err := gcConstReferenceKind(c, exact)
			if err != nil {
				return 0, err
			}
			value := gcConstStackValue{bits: bits, kind: kind}
			if kind == gcConstCollectorRef {
				value.ref = gc.Ref(uint32(bits))
			}
			stack = append(stack, value)
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
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e: // integer add/sub/mul
			right, err := pop()
			if err != nil {
				return 0, err
			}
			left, err := pop()
			if err != nil {
				return 0, err
			}
			if left.kind != gcConstNumeric || right.kind != gcConstNumeric {
				return 0, fmt.Errorf("integer constant expression operand is not numeric")
			}
			var bits uint64
			switch op {
			case 0x6a:
				bits = uint64(uint32(left.bits) + uint32(right.bits))
			case 0x6b:
				bits = uint64(uint32(left.bits) - uint32(right.bits))
			case 0x6c:
				bits = uint64(uint32(left.bits) * uint32(right.bits))
			case 0x7c:
				bits = left.bits + right.bits
			case 0x7d:
				bits = left.bits - right.bits
			case 0x7e:
				bits = left.bits * right.bits
			}
			stack = append(stack, gcConstStackValue{bits: bits})
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
			kind, err := gcConstHeapKind(c, heap)
			if err != nil {
				return 0, err
			}
			stack = append(stack, gcConstStackValue{ref: gc.Null(), kind: kind})
		case 0xd2: // ref.func
			idx, err := r.U32()
			if err != nil {
				return 0, err
			}
			off := (int(idx) + 1) * coreruntime.FuncRefDescBytes
			if off < coreruntime.FuncRefDescBytes || off+coreruntime.FuncRefDescBytes > len(funcRefDescs) {
				return 0, fmt.Errorf("ref.func %d descriptor is unavailable", idx)
			}
			stack = append(stack, gcConstStackValue{bits: uint64(uintptr(unsafe.Pointer(&funcRefDescs[off]))), kind: gcConstFuncRef})
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
				ref, err := collector.NewStructWithRoots(domainType, values, allocationRoots)
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, kind: gcConstCollectorRef})
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
				ref, err := collector.NewStructDefaultWithRoots(domainType, allocationRoots)
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, kind: gcConstCollectorRef})
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
				ref, err := collector.NewArrayWithRoots(domainType, uint32(lengthValue.bits), init, allocationRoots)
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, kind: gcConstCollectorRef})
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
				ref, err := collector.NewArrayDefaultWithRoots(domainType, uint32(lengthValue.bits), allocationRoots)
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, kind: gcConstCollectorRef})
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
				ref, err := collector.NewArrayFixedWithRoots(domainType, values, allocationRoots)
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: ref, kind: gcConstCollectorRef})
			case 26: // any.convert_extern
				v, err := pop()
				if err != nil {
					return 0, err
				}
				if v.kind != gcConstExternRef || conversion == nil {
					return 0, fmt.Errorf("any.convert_extern constant has no extern conversion owner")
				}
				bits, err := conversion.anyFromExtern(v.bits)
				if err != nil {
					return 0, err
				}
				if bits>>32 != 0 {
					return 0, fmt.Errorf("any.convert_extern constant produced non-compact anyref %#x", bits)
				}
				stack = append(stack, gcConstStackValue{ref: gc.Ref(uint32(bits)), kind: gcConstCollectorRef})
			case 27: // extern.convert_any
				v, err := pop()
				if err != nil {
					return 0, err
				}
				if v.kind != gcConstCollectorRef || conversion == nil {
					return 0, fmt.Errorf("extern.convert_any constant has no GC conversion owner")
				}
				bits, err := conversion.externFromAny(uint64(v.ref))
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{bits: bits, kind: gcConstExternRef})
			case 28: // ref.i31
				v, err := pop()
				if err != nil {
					return 0, err
				}
				stack = append(stack, gcConstStackValue{ref: gc.I31New(int32(v.bits)), kind: gcConstCollectorRef})
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
