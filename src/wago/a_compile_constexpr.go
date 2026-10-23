package wago

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (i constExprInit) GlobalRef() (int, bool) { return i.GlobalIndex, i.GlobalIndex >= 0 }

func (i constExprInit) FuncRef() (int, bool) { return i.FuncIndex, i.FuncIndex >= 0 }

func (r constExprResult) Init() constExprInit {
	return constExprInit{Bits: r.bits, V128: r.v128, GlobalIndex: r.GlobalIndex, FuncIndex: r.FuncIndex, Expr: r.Expr}
}

func applyGlobalInit(g *GlobalDef, init constExprInit) {
	g.Bits = init.Bits
	g.V128 = init.V128
	if idx, ok := init.GlobalRef(); ok {
		g.HasInitGlobal = true
		g.InitGlobal = idx
	}
	if idx, ok := init.FuncRef(); ok {
		g.HasInitFunc = true
		g.InitFunc = uint32(idx)
	}
	g.InitExpr = append([]byte(nil), init.Expr...)
}

func applyOffsetInit(o *OffsetInit, init constExprInit) {
	o.Base = uint32(init.Bits)
	if idx, ok := init.GlobalRef(); ok {
		o.HasGlobal = true
		o.Global = idx
	}
	o.Expr = append([]byte(nil), init.Expr...)
}

func applyElemOffset(e *ElemInit, init constExprInit) { applyOffsetInit(&e.Offset, init) }

func applyDataOffset(d *DataInit, init constExprInit) { applyOffsetInit(&d.Offset, init) }

func evalConstExprBytesWithContext(b []byte, want wasm.ValType, ctx *constExprCompileContext) (constExprResult, error) {
	var m *wasm.Module
	if ctx != nil {
		m = ctx.module
	}
	if got, matched, err := evalNullExternConversionConstExpr(b, want); matched {
		return got, err
	}
	if isI31RefType(want) || isAnyRefType(want) {
		if got, matched, err := evalI31ConstExprBytes(b, want, ctx); matched && err == nil {
			return got, nil
		}
	}
	if want.Kind() == wasm.ValRef && m != nil && len(b) != 0 && b[0] == 0xfb {
		return constExprResult{vtype: want, GlobalIndex: -1, FuncIndex: -1, Expr: append([]byte(nil), b...)}, nil
	}
	r := wasm.NewReader(b)
	op, err := r.Byte()
	if err != nil {
		return constExprResult{}, err
	}
	got := constExprResult{GlobalIndex: -1, FuncIndex: -1}
	switch op {
	case 0x23: // global.get (valid in const expressions only for imported immutable globals)
		x, err := r.U32()
		if err != nil {
			return constExprResult{}, err
		}
		if m == nil {
			return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x23")
		}
		gt, ok := m.GlobalTypeByIndex(x)
		if !ok || gt.Mutable {
			return constExprResult{}, fmt.Errorf("unsupported const expression global.get %d", x)
		}
		got.bits, got.vtype = 0, gt.Type
		got.GlobalIndex = int(x)
	case 0x41: // i32.const
		v, err := r.I32()
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = uint64(uint32(v)), wasm.I32
	case 0x42: // i64.const
		v, err := r.I64()
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = uint64(v), wasm.I64
	case 0x43: // f32.const
		bb, err := r.Bytes(4)
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = uint64(binary.LittleEndian.Uint32(bb)), wasm.F32
	case 0x44: // f64.const
		bb, err := r.Bytes(8)
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = binary.LittleEndian.Uint64(bb), wasm.F64
	case 0xd0: // ref.null
		heap, err := r.S33()
		if err != nil {
			return constExprResult{}, err
		}
		switch heap {
		case -16: // func (0x70): null funcref
			got.vtype = wasm.FuncRef
		case -13: // nofunc (0x73): bottom null funcref
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapNoFunc))
		case -17: // extern (0x6f): null externref
			got.vtype = wasm.ExternRef
		case -14: // noextern (0x72): bottom null externref
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapNoExtern))
		case -18: // any (0x6e): null GC-category reference
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapAny))
		case -19: // eq (0x6d): null equality reference
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapEq))
		case -20: // i31 (0x6c): null i31 reference
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapI31))
		case -21: // struct (0x6b): null struct reference
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapStruct))
		case -22: // array (0x6a): null array reference
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapArray))
		case -15: // none (0x71): bottom null GC-category reference
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapNone))
		case -23: // exn (0x69): null exception reference
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapExn))
		case -12: // noexn (0x74): bottom null exception reference
			got.vtype = wasm.RefVal(wasm.AbsRef(wasm.HeapNoExn))
		default:
			if heap < 0 || m == nil {
				return constExprResult{}, fmt.Errorf("unsupported ref.null heap type %d", heap)
			}
			typeCount := 0
			for i := range m.Types {
				typeCount += len(m.Types[i].SubTypes)
			}
			if uint64(heap) >= uint64(typeCount) {
				return constExprResult{}, fmt.Errorf("unsupported ref.null heap type %d", heap)
			}
			got.vtype = wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: uint32(heap)}), false))
		}
		got.bits = 0
	case 0xd2: // ref.func
		idx, err := r.U32()
		if err != nil {
			return constExprResult{}, err
		}
		got.FuncIndex = int(idx)
		got.vtype = wasm.FuncRef
		if m != nil {
			if typeIndex, ok := m.FuncTypeIndex(idx); ok {
				got.vtype = wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(typeIndex), false))
			}
		}
	case 0xfd: // v128.const
		sub, err := r.U32()
		if err != nil {
			return constExprResult{}, err
		}
		if sub != 12 {
			return constExprResult{}, fmt.Errorf("unsupported const expression 0xfd %d", sub)
		}
		bb, err := r.Bytes(16)
		if err != nil {
			return constExprResult{}, err
		}
		copy(got.v128[:], bb)
		got.vtype = wasm.V128
	default:
		return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x%02x", op)
	}
	end, err := r.Byte()
	if err != nil {
		return constExprResult{}, fmt.Errorf("const expression missing end: %w", err)
	}
	if end != 0x0B {
		if want.Kind() == wasm.ValRef && m != nil {
			// Validation has already type-checked the complete Core 3 constant
			// expression. Preserve object-building programs for collector-backed
			// evaluation after preceding globals and function descriptors exist.
			return constExprResult{vtype: want, GlobalIndex: -1, FuncIndex: -1, Expr: append([]byte(nil), b...)}, nil
		}
		bits, usesGlobal, err := evalScalarConstExprProgram(b, want, moduleConstExprGlobalResolver(m))
		if err != nil {
			return constExprResult{}, err
		}
		got.bits, got.vtype = bits, want
		got.GlobalIndex, got.FuncIndex = -1, -1
		if usesGlobal {
			got.Expr = append([]byte(nil), b...)
		}
		return got, nil
	}
	if r.BytesLeft() != 0 {
		return constExprResult{}, fmt.Errorf("const expression has trailing bytes")
	}
	if !constExprTypeMatches(got.vtype, want, ctx) {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.vtype, want)
	}
	return got, nil
}

func evalNullExternConversionConstExpr(b []byte, want wasm.ValType) (constExprResult, bool, error) {
	r := wasm.NewReader(b)
	op, err := r.Byte()
	if err != nil || op != 0xd0 {
		return constExprResult{}, false, nil
	}
	heap, err := r.S33()
	if err != nil {
		return constExprResult{}, true, err
	}
	// A null converted any number of times between the any and extern
	// hierarchies is still null; the last conversion decides the type.
	// Validation has already proved each step well typed, but keep the
	// hierarchy check so a malformed chain still fails closed.
	got := constExprResult{GlobalIndex: -1, FuncIndex: -1}
	inExtern := heap == -17 || heap == -14 // extern / noextern
	conversions := 0
	for {
		if r.BytesLeft() == 0 {
			break
		}
		if b[len(b)-r.BytesLeft()] != 0xfb {
			break
		}
		if _, err := r.Byte(); err != nil {
			return constExprResult{}, true, err
		}
		sub, err := r.U32()
		if err != nil {
			return constExprResult{}, true, err
		}
		switch sub {
		case 26: // any.convert_extern
			if !inExtern {
				return constExprResult{}, true, fmt.Errorf("any.convert_extern constant source heap %d is not extern", heap)
			}
			got.vtype, inExtern = wasm.AnyRef, false
		case 27: // extern.convert_any
			if inExtern {
				return constExprResult{}, true, fmt.Errorf("extern.convert_any constant source heap %d is not any", heap)
			}
			got.vtype, inExtern = wasm.ExternRef, true
		default:
			if conversions == 0 {
				return constExprResult{}, false, nil
			}
			return constExprResult{}, true, fmt.Errorf("GC conversion constant expression has trailing instructions")
		}
		conversions++
	}
	if conversions == 0 {
		return constExprResult{}, false, nil
	}
	end, err := r.Byte()
	if err != nil {
		return constExprResult{}, true, fmt.Errorf("GC conversion constant expression missing end: %w", err)
	}
	if end != 0x0b || r.BytesLeft() != 0 {
		return constExprResult{}, true, fmt.Errorf("GC conversion constant expression has trailing instructions")
	}
	if !constExprTypeMatches(got.vtype, want, nil) {
		return constExprResult{}, true, fmt.Errorf("const expression type %s, want %s", got.vtype, want)
	}
	return got, true, nil
}

func isI31RefType(t wasm.ValType) bool {
	return t.Kind() == wasm.ValRef && t.Ref().Heap().Kind() == wasm.HeapAbs && t.Ref().Heap().Abs() == wasm.HeapI31
}

func isAnyRefType(t wasm.ValType) bool {
	return t.Kind() == wasm.ValRef && t.Ref().Heap().Kind() == wasm.HeapAbs && t.Ref().Heap().Abs() == wasm.HeapAny
}

func constExprTypeMatches(actual, required wasm.ValType, ctx *constExprCompileContext) bool {
	if valTypeEqual(actual, required) {
		return true
	}
	if ctx == nil || actual.Kind() != wasm.ValRef || required.Kind() != wasm.ValRef {
		return false
	}
	a, err := ctx.converter.valueType(actual, -1)
	if err != nil {
		return false
	}
	b, err := ctx.converter.valueType(required, -1)
	return err == nil && valueTypeSubtype(a, ctx.types, b, ctx.types)
}

func evalConstExprWithContext(e wasm.Expr, want wasm.ValType, ctx *constExprCompileContext) (constExprResult, error) {
	var m *wasm.Module
	if ctx != nil {
		m = ctx.module
	}
	if len(e.Instrs) == 0 && len(e.BodyBytes) != 0 {
		return evalConstExprBytesWithContext(e.BodyBytes, want, ctx)
	}
	if len(e.Instrs) != 1 {
		encoded, err := wasm.EncodeExpr(e)
		if err != nil {
			return constExprResult{}, fmt.Errorf("encode const expression: %w", err)
		}
		return evalConstExprBytesWithContext(encoded, want, ctx)
	}
	in := e.Instrs[0]
	got := constExprResult{GlobalIndex: -1, FuncIndex: -1}
	switch in.Kind {
	case wasm.InstrI32Const:
		got.bits, got.vtype = uint64(uint32(in.I32)), wasm.I32
	case wasm.InstrI64Const:
		got.bits, got.vtype = uint64(in.I64), wasm.I64
	case wasm.InstrF32Const:
		got.bits, got.vtype = uint64(in.F32Bits), wasm.F32
	case wasm.InstrF64Const:
		got.bits, got.vtype = in.F64Bits, wasm.F64
	case wasm.InstrV128Const:
		lanes := in.Lanes()
		for i, b := range lanes {
			got.v128[i] = byte(b)
		}
		got.vtype = wasm.V128
	case wasm.InstrRefNull:
		refType := wasm.RefVal(in.RefType())
		var exact ValueTypeDescriptor
		var types []DefinedTypeDescriptor
		var err error
		if ctx != nil {
			types = ctx.types
			exact, err = ctx.converter.valueType(refType, -1)
		} else {
			exact, err = valueTypeDescriptorInModule(nil, refType)
		}
		if err != nil {
			return constExprResult{}, fmt.Errorf("unsupported ref.null type %s", refType)
		}
		if _, ok := exact.ABIType(types); err != nil || !ok {
			return constExprResult{}, fmt.Errorf("unsupported ref.null type %s", refType)
		}
		got.bits, got.vtype = 0, refType
	case wasm.InstrRefFunc:
		got.vtype, got.FuncIndex = wasm.FuncRef, int(in.Index)
		if m != nil {
			if typeIndex, ok := m.FuncTypeIndex(in.Index); ok {
				got.vtype = wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(typeIndex), false))
			}
		}
	case wasm.InstrGlobalGet:
		if m == nil {
			return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x23")
		}
		gt, ok := m.GlobalTypeByIndex(in.Index)
		if !ok || gt.Mutable {
			return constExprResult{}, fmt.Errorf("unsupported const expression global.get %d", in.Index)
		}
		got.bits, got.vtype = 0, gt.Type
		got.GlobalIndex = int(in.Index)
	default:
		return constExprResult{}, fmt.Errorf("unsupported const expression opcode %s", in.Kind)
	}
	if !constExprTypeMatches(got.vtype, want, ctx) {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.vtype, want)
	}
	return got, nil
}
