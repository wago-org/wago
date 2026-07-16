package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// constExprInit is the internal reusable form for MVP const expressions whose
// literal bits are known at compile time unless an imported immutable global must
// be read later, after import values are supplied during instantiation.
type constExprInit struct {
	Bits        uint64
	V128        V128
	GlobalIndex int
	FuncIndex   int
	Expr        []byte
}

func (i constExprInit) GlobalRef() (int, bool) { return i.GlobalIndex, i.GlobalIndex >= 0 }
func (i constExprInit) FuncRef() (int, bool)   { return i.FuncIndex, i.FuncIndex >= 0 }

type constExprResult struct {
	bits        uint64
	v128        V128
	vtype       wasm.ValType
	GlobalIndex int
	FuncIndex   int
	Expr        []byte
}

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

func evalConstExpr(b []byte, want wasm.ValType) (uint64, error) {
	res, err := evalConstExprBytes(b, want)
	return res.bits, err
}

func evalConstExprBytes(b []byte, want wasm.ValType) (constExprResult, error) {
	return evalConstExprBytesWithModule(b, want, nil)
}

func evalConstExprBytesWithModule(b []byte, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
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
			if _, ok := m.TypeFunc(uint32(heap)); !ok {
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
	if !constExprTypeMatches(got.vtype, want, m) {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.vtype, want)
	}
	return got, nil
}

func constExprTypeMatches(actual, required wasm.ValType, m *wasm.Module) bool {
	if valTypeEqual(actual, required) {
		return true
	}
	if m == nil || actual.Kind != wasm.ValRef || required.Kind != wasm.ValRef {
		return false
	}
	types, err := typeDescriptorsFromWasm(m)
	if err != nil {
		return false
	}
	a, err := valueTypeDescriptorInModule(m, actual)
	if err != nil {
		return false
	}
	b, err := valueTypeDescriptorInModule(m, required)
	return err == nil && valueTypeSubtype(a, types, b, types)
}

// evalConstExprWithModule intentionally stays narrower than full wasm validation:
// wasm.ValidateModule checks const-expression shape/type rules before compile
// reaches here, while this helper converts the supported MVP operators into
// instantiate-time bits or deferred imported-global references.
func evalConstExprWithModule(e wasm.Expr, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
	if len(e.Instrs) == 0 && len(e.BodyBytes) != 0 {
		return evalConstExprBytesWithModule(e.BodyBytes, want, m)
	}
	if len(e.Instrs) != 1 {
		encoded, err := wasm.EncodeExpr(e)
		if err != nil {
			return constExprResult{}, fmt.Errorf("encode const expression: %w", err)
		}
		return evalConstExprBytesWithModule(encoded, want, m)
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
		exact, err := valueTypeDescriptorInModule(m, refType)
		if err != nil {
			return constExprResult{}, fmt.Errorf("unsupported ref.null type %s", refType)
		}
		var types []DefinedTypeDescriptor
		if m != nil {
			types, err = typeDescriptorsFromWasm(m)
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
	if !constExprTypeMatches(got.vtype, want, m) {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.vtype, want)
	}
	return got, nil
}

type constExprGlobalResolver func(uint32) (bits uint64, typ wasm.ValType, mutable bool, ok bool)

func moduleConstExprGlobalResolver(m *wasm.Module) constExprGlobalResolver {
	if m == nil {
		return nil
	}
	return func(index uint32) (uint64, wasm.ValType, bool, bool) {
		gt, ok := m.GlobalTypeByIndex(index)
		if !ok {
			return 0, wasm.ValType{}, false, false
		}
		return 0, gt.Type, gt.Mutable, true
	}
}

type scalarConstValue struct {
	bits uint64
	typ  wasm.ValType
}

// evalScalarConstExprProgram evaluates the scalar portion of WebAssembly 3.0
// extended constant expressions. The same strict parser is used for compile-time
// folding, codec validation, and instantiation so malformed persisted programs
// fail closed.
func evalScalarConstExprProgram(b []byte, want wasm.ValType, resolve constExprGlobalResolver) (bits uint64, usesGlobal bool, err error) {
	if !wasm.EqualValType(want, wasm.I32) && !wasm.EqualValType(want, wasm.I64) {
		return 0, false, fmt.Errorf("extended const expression type %s is not scalar integer", want)
	}
	r := wasm.NewReader(b)
	stack := make([]scalarConstValue, 0, 4)
	push := func(v scalarConstValue) { stack = append(stack, v) }
	pop2 := func(typ wasm.ValType) (scalarConstValue, scalarConstValue, error) {
		if len(stack) < 2 {
			return scalarConstValue{}, scalarConstValue{}, fmt.Errorf("extended const expression stack underflow")
		}
		rhs := stack[len(stack)-1]
		lhs := stack[len(stack)-2]
		stack = stack[:len(stack)-2]
		if !wasm.EqualValType(lhs.typ, typ) || !wasm.EqualValType(rhs.typ, typ) {
			return scalarConstValue{}, scalarConstValue{}, fmt.Errorf("extended const expression operand type mismatch")
		}
		return lhs, rhs, nil
	}

	for r.HasNext() {
		op, readErr := r.Byte()
		if readErr != nil {
			return 0, usesGlobal, readErr
		}
		switch op {
		case 0x0b:
			if r.BytesLeft() != 0 {
				return 0, usesGlobal, fmt.Errorf("extended const expression has trailing bytes")
			}
			if len(stack) != 1 || !wasm.EqualValType(stack[0].typ, want) {
				return 0, usesGlobal, fmt.Errorf("extended const expression result type mismatch")
			}
			return stack[0].bits, usesGlobal, nil
		case 0x23:
			index, readErr := r.U32()
			if readErr != nil {
				return 0, usesGlobal, readErr
			}
			if resolve == nil {
				return 0, usesGlobal, fmt.Errorf("extended const expression global.get %d has no resolver", index)
			}
			value, typ, mutable, ok := resolve(index)
			if !ok {
				return 0, usesGlobal, fmt.Errorf("extended const expression global.get %d is unavailable", index)
			}
			if mutable {
				return 0, usesGlobal, fmt.Errorf("extended const expression global.get %d is mutable", index)
			}
			if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) {
				return 0, usesGlobal, fmt.Errorf("extended const expression global.get %d has type %s", index, typ)
			}
			usesGlobal = true
			push(scalarConstValue{bits: value, typ: typ})
		case 0x41:
			value, readErr := r.I32()
			if readErr != nil {
				return 0, usesGlobal, readErr
			}
			push(scalarConstValue{bits: uint64(uint32(value)), typ: wasm.I32})
		case 0x42:
			value, readErr := r.I64()
			if readErr != nil {
				return 0, usesGlobal, readErr
			}
			push(scalarConstValue{bits: uint64(value), typ: wasm.I64})
		case 0x6a, 0x6b, 0x6c:
			lhs, rhs, popErr := pop2(wasm.I32)
			if popErr != nil {
				return 0, usesGlobal, popErr
			}
			a, b := uint32(lhs.bits), uint32(rhs.bits)
			var value uint32
			switch op {
			case 0x6a:
				value = a + b
			case 0x6b:
				value = a - b
			case 0x6c:
				value = a * b
			}
			push(scalarConstValue{bits: uint64(value), typ: wasm.I32})
		case 0x7c, 0x7d, 0x7e:
			lhs, rhs, popErr := pop2(wasm.I64)
			if popErr != nil {
				return 0, usesGlobal, popErr
			}
			var value uint64
			switch op {
			case 0x7c:
				value = lhs.bits + rhs.bits
			case 0x7d:
				value = lhs.bits - rhs.bits
			case 0x7e:
				value = lhs.bits * rhs.bits
			}
			push(scalarConstValue{bits: value, typ: wasm.I64})
		default:
			return 0, usesGlobal, fmt.Errorf("unsupported extended const expression opcode 0x%02x", op)
		}
	}
	return 0, usesGlobal, fmt.Errorf("extended const expression missing end")
}

func wasmScalarValType(t ValType) (wasm.ValType, bool) {
	switch t {
	case ValI32:
		return wasm.I32, true
	case ValI64:
		return wasm.I64, true
	default:
		return wasm.ValType{}, false
	}
}

func validateCompiledScalarConstExpr(b []byte, want ValType, defs []GlobalDef, globalLimit int) error {
	wasmWant, ok := wasmScalarValType(want)
	if !ok {
		return fmt.Errorf("extended const expression has unsupported result type %s", want)
	}
	resolve := func(index uint32) (uint64, wasm.ValType, bool, bool) {
		i := int(index)
		if i < 0 || i >= globalLimit || i >= len(defs) {
			return 0, wasm.ValType{}, false, false
		}
		typ, ok := wasmScalarValType(defs[i].Type)
		if !ok {
			return 0, wasm.ValType{}, defs[i].Mutable, false
		}
		return 0, typ, defs[i].Mutable, true
	}
	_, _, err := evalScalarConstExprProgram(b, wasmWant, resolve)
	return err
}

func evalCompiledScalarConstExpr(b []byte, want ValType, globals []*Global, defs []GlobalDef, globalLimit int) (uint64, error) {
	wasmWant, ok := wasmScalarValType(want)
	if !ok {
		return 0, fmt.Errorf("extended const expression has unsupported result type %s", want)
	}
	resolve := func(index uint32) (uint64, wasm.ValType, bool, bool) {
		i := int(index)
		if i < 0 || i >= globalLimit || i >= len(globals) || i >= len(defs) || globals[i] == nil {
			return 0, wasm.ValType{}, false, false
		}
		typ, ok := wasmScalarValType(defs[i].Type)
		if !ok {
			return 0, wasm.ValType{}, defs[i].Mutable, false
		}
		return readGlobalObject(globals[i], defs[i].Type), typ, defs[i].Mutable, true
	}
	bits, _, err := evalScalarConstExprProgram(b, wasmWant, resolve)
	return bits, err
}
