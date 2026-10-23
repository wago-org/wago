package wago

import (
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

type constExprCompileContext struct {
	module    *wasm.Module
	types     []DefinedTypeDescriptor
	converter wasmTypeDescriptorConverter
}

func newConstExprCompileContext(m *wasm.Module) (*constExprCompileContext, error) {
	if m == nil {
		return nil, nil
	}
	types, err := typeDescriptorsFromWasm(m)
	if err != nil {
		return nil, err
	}
	return &constExprCompileContext{module: m, types: types, converter: newWasmTypeDescriptorConverter(m)}, nil
}

type constExprResult struct {
	bits        uint64
	v128        V128
	vtype       wasm.ValType
	GlobalIndex int
	FuncIndex   int
	Expr        []byte
}

func evalConstExpr(b []byte, want wasm.ValType) (uint64, error) {
	res, err := evalConstExprBytes(b, want)
	return res.bits, err
}

func evalConstExprBytes(b []byte, want wasm.ValType) (constExprResult, error) {
	return evalConstExprBytesWithModule(b, want, nil)
}

func evalConstExprBytesWithModule(b []byte, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
	ctx, err := newConstExprCompileContext(m)
	if err != nil {
		return constExprResult{}, err
	}
	return evalConstExprBytesWithContext(b, want, ctx)
}

func evalI31ConstExprBytes(b []byte, want wasm.ValType, ctx *constExprCompileContext) (constExprResult, bool, error) {
	var m *wasm.Module
	if ctx != nil {
		m = ctx.module
	}
	r := wasm.NewReader(b)
	op, err := r.Byte()
	if err != nil {
		return constExprResult{}, false, err
	}
	got := constExprResult{GlobalIndex: -1, FuncIndex: -1}
	switch op {
	case 0x41:
		v, err := r.I32()
		if err != nil {
			return constExprResult{}, true, err
		}
		got.bits = uint64(uint32(v)<<1 | 1)
	case 0x23:
		index, err := r.U32()
		if err != nil {
			return constExprResult{}, true, err
		}
		if m == nil {
			return constExprResult{}, true, fmt.Errorf("unsupported i31 const expression global.get %d", index)
		}
		gt, ok := m.GlobalTypeByIndex(index)
		if !ok || gt.Mutable || !wasm.EqualValType(gt.Type, wasm.I32) {
			return constExprResult{}, true, fmt.Errorf("unsupported i31 const expression global.get %d", index)
		}
		got.Expr = append([]byte(nil), b...)
	default:
		return constExprResult{}, false, nil
	}
	prefix, err := r.Byte()
	if err != nil {
		return constExprResult{}, true, err
	}
	if prefix != 0xfb {
		return constExprResult{}, false, nil
	}
	sub, err := r.U32()
	if err != nil {
		return constExprResult{}, true, err
	}
	if sub != 28 {
		return constExprResult{}, false, nil
	}
	end, err := r.Byte()
	if err != nil {
		return constExprResult{}, true, fmt.Errorf("i31 const expression missing end: %w", err)
	}
	if end != 0x0b || r.BytesLeft() != 0 {
		// The ref.i31 may be an operand of a longer validated GC constant
		// expression (for example an extern/any conversion round trip). Let the
		// general collector-backed evaluator preserve that complete program.
		return constExprResult{}, false, nil
	}
	got.vtype = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false))
	if !constExprTypeMatches(got.vtype, want, ctx) {
		return constExprResult{}, true, fmt.Errorf("const expression type %s, want %s", got.vtype, want)
	}
	return got, true, nil
}

// evalConstExprWithModule intentionally stays narrower than full wasm validation:
// wasm.ValidateModule checks const-expression shape/type rules before compile
// reaches here, while this helper converts the supported MVP operators into
// instantiate-time bits or deferred imported-global references.
func evalConstExprWithModule(e wasm.Expr, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
	ctx, err := newConstExprCompileContext(m)
	if err != nil {
		return constExprResult{}, err
	}
	return evalConstExprWithContext(e, want, ctx)
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
	// Validated expressions are normally shallow. Keep the common operand stack
	// inline; append provides a bounded spill path for deeper valid programs.
	var inline [8]scalarConstValue
	stack := inline[:0]
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

type constExprContext uint8

const (
	constExprGlobalInitializer constExprContext = iota
	constExprDataOffset
	constExprElementOffset
)

type constExprGlobalScope struct {
	context constExprContext
	limit   int // exclusive absolute global index visible in this expression context
}

func validateCompiledScalarConstExpr(b []byte, want ValType, defs []GlobalDef, scope constExprGlobalScope) error {
	if want == ValI31Ref {
		_, err := evalCompiledI31ConstExpr(b, nil, defs, scope)
		return err
	}
	wasmWant, ok := wasmScalarValType(want)
	if !ok {
		return fmt.Errorf("extended const expression has unsupported result type %s", want)
	}
	resolve := func(index uint32) (uint64, wasm.ValType, bool, bool) {
		i := int(index)
		if i < 0 || i >= scope.limit || i >= len(defs) {
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

func evalCompiledScalarConstExpr(b []byte, want ValType, globals []*Global, defs []GlobalDef, scope constExprGlobalScope) (uint64, error) {
	if want == ValI31Ref {
		return evalCompiledI31ConstExpr(b, globals, defs, scope)
	}
	wasmWant, ok := wasmScalarValType(want)
	if !ok {
		return 0, fmt.Errorf("extended const expression has unsupported result type %s", want)
	}
	resolve := func(index uint32) (uint64, wasm.ValType, bool, bool) {
		i := int(index)
		if i < 0 || i >= scope.limit || i >= len(globals) || i >= len(defs) || globals[i] == nil {
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

// evalCompiledI31ConstExpr evaluates `<i32 extended constant> ref.i31 end`.
// The i32 operand is any program the scalar evaluator accepts (constants,
// immutable global.get, add/sub/mul), so compile-time admission and
// instantiation agree on every valid i31 initializer. With nil globals it
// only validates the shape.
func evalCompiledI31ConstExpr(b []byte, globals []*Global, defs []GlobalDef, scope constExprGlobalScope) (uint64, error) {
	r := wasm.NewReader(b)
	last := -1
	for {
		start := len(b) - r.BytesLeft()
		op, err := r.Byte()
		if err != nil {
			return 0, fmt.Errorf("i31 initializer has invalid end")
		}
		if op == 0x0b {
			if r.BytesLeft() != 0 {
				return 0, fmt.Errorf("i31 initializer has invalid end")
			}
			break
		}
		if _, err := wasm.ClassifyInstructionImmediate(r, op); err != nil {
			return 0, fmt.Errorf("i31 initializer: %w", err)
		}
		last = start
	}
	if last < 0 || b[last] != 0xfb {
		return 0, fmt.Errorf("i31 initializer missing ref.i31")
	}
	tail := wasm.NewReader(b[last+1:])
	if sub, err := tail.U32(); err != nil || sub != 28 {
		return 0, fmt.Errorf("i31 initializer has unsupported 0xfb opcode %d", sub)
	}
	operand := append(append([]byte(nil), b[:last]...), 0x0b)
	resolve := func(index uint32) (uint64, wasm.ValType, bool, bool) {
		i := int(index)
		if i < 0 || i >= scope.limit || i >= len(defs) {
			return 0, wasm.ValType{}, false, false
		}
		typ, ok := wasmScalarValType(defs[i].Type)
		if !ok {
			return 0, wasm.ValType{}, defs[i].Mutable, false
		}
		if globals == nil {
			return 0, typ, defs[i].Mutable, true
		}
		if i >= len(globals) || globals[i] == nil {
			return 0, wasm.ValType{}, false, false
		}
		return readGlobalObject(globals[i], defs[i].Type), typ, defs[i].Mutable, true
	}
	bits, _, err := evalScalarConstExprProgram(operand, wasm.I32, resolve)
	if err != nil {
		return 0, fmt.Errorf("i31 initializer operand: %w", err)
	}
	if globals == nil {
		return 0, nil
	}
	return uint64(uint32(bits)<<1 | 1), nil
}
