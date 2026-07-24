package wago

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// constExprInit is the reusable form for supported constant expressions. Literal
// expressions are folded at compile time; expressions depending on imported
// immutable globals retain their encoded program for instantiation-time evaluation.
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
	expr        []byte
}

func (r constExprResult) Init() constExprInit {
	return constExprInit{Bits: r.bits, V128: r.v128, GlobalIndex: r.GlobalIndex, FuncIndex: r.FuncIndex, Expr: r.expr}
}

func applyGlobalInit(g *GlobalDef, init constExprInit) {
	g.Bits = init.Bits
	g.V128 = init.V128
	g.InitExpr = append(g.InitExpr[:0], init.Expr...)
	if idx, ok := init.GlobalRef(); ok {
		g.HasInitGlobal = true
		g.InitGlobal = idx
	}
	if idx, ok := init.FuncRef(); ok {
		g.HasInitFunc = true
		g.InitFunc = uint32(idx)
	}
}

func applyOffsetInit(o *OffsetInit, init constExprInit) {
	o.Base = uint32(init.Bits)
	o.Expr = append(o.Expr[:0], init.Expr...)
	if idx, ok := init.GlobalRef(); ok {
		o.HasGlobal = true
		o.Global = idx
	}
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

type constExprStackValue struct {
	bits       uint64
	v128       V128
	vtype      wasm.ValType
	unresolved bool
	global     int
	funcIndex  int
}

type constExprGlobalResolver func(index uint32) (bits uint64, typ wasm.ValType, err error)

func evalConstExprBytesWithModule(b []byte, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
	return evalConstExprBytesResolved(b, want, m, nil)
}

func evalConstExprBytesResolved(b []byte, want wasm.ValType, m *wasm.Module, resolve constExprGlobalResolver) (constExprResult, error) {
	r := wasm.NewReader(b)
	stack := make([]constExprStackValue, 0, 4)
	extended := false
	for {
		op, err := r.Byte()
		if err != nil {
			return constExprResult{}, fmt.Errorf("const expression missing end: %w", err)
		}
		switch op {
		case 0x0b: // end
			if r.BytesLeft() != 0 {
				return constExprResult{}, fmt.Errorf("const expression has trailing bytes")
			}
			if len(stack) != 1 {
				return constExprResult{}, fmt.Errorf("const expression leaves %d values", len(stack))
			}
			v := stack[0]
			if !valTypeEqual(v.vtype, want) {
				return constExprResult{}, fmt.Errorf("const expression type %s, want %s", v.vtype, want)
			}
			out := constExprResult{bits: v.bits, v128: v.v128, vtype: v.vtype, GlobalIndex: -1, FuncIndex: v.funcIndex}
			if extended {
				// Preserve the expression even when every operand is literal so codec
				// validation can infer and enforce its required feature bit.
				out.expr = append([]byte(nil), b...)
			} else if v.unresolved && v.global >= 0 {
				out.GlobalIndex = v.global
			}
			return out, nil
		case 0x23: // global.get
			x, err := r.U32()
			if err != nil {
				return constExprResult{}, err
			}
			v := constExprStackValue{global: int(x), funcIndex: -1}
			if resolve != nil {
				v.bits, v.vtype, err = resolve(x)
				if err != nil {
					return constExprResult{}, err
				}
			} else {
				if m == nil {
					return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x23")
				}
				gt, ok := m.GlobalTypeByIndex(x)
				if !ok || int(x) >= m.ImportedGlobalCount() || gt.Mutable {
					return constExprResult{}, fmt.Errorf("unsupported const expression global.get %d", x)
				}
				v.vtype, v.unresolved = gt.Type, true
			}
			stack = append(stack, v)
		case 0x41: // i32.const
			v, err := r.I32()
			if err != nil {
				return constExprResult{}, err
			}
			stack = append(stack, constExprStackValue{bits: uint64(uint32(v)), vtype: wasm.I32, global: -1, funcIndex: -1})
		case 0x42: // i64.const
			v, err := r.I64()
			if err != nil {
				return constExprResult{}, err
			}
			stack = append(stack, constExprStackValue{bits: uint64(v), vtype: wasm.I64, global: -1, funcIndex: -1})
		case 0x43: // f32.const
			bb, err := r.Bytes(4)
			if err != nil {
				return constExprResult{}, err
			}
			stack = append(stack, constExprStackValue{bits: uint64(binary.LittleEndian.Uint32(bb)), vtype: wasm.F32, global: -1, funcIndex: -1})
		case 0x44: // f64.const
			bb, err := r.Bytes(8)
			if err != nil {
				return constExprResult{}, err
			}
			stack = append(stack, constExprStackValue{bits: binary.LittleEndian.Uint64(bb), vtype: wasm.F64, global: -1, funcIndex: -1})
		case 0x6a, 0x6b, 0x6c: // i32.add/sub/mul
			extended = true
			var err error
			stack, err = evalConstExprBinary(stack, wasm.I32, op)
			if err != nil {
				return constExprResult{}, err
			}
		case 0x7c, 0x7d, 0x7e: // i64.add/sub/mul
			extended = true
			var err error
			stack, err = evalConstExprBinary(stack, wasm.I64, op)
			if err != nil {
				return constExprResult{}, err
			}
		case 0xd0: // ref.null
			heap, err := r.S33()
			if err != nil {
				return constExprResult{}, err
			}
			v := constExprStackValue{global: -1, funcIndex: -1}
			switch heap {
			case -16, -13:
				v.vtype = wasm.FuncRef
			case -17, -14:
				v.vtype = wasm.ExternRef
			default:
				return constExprResult{}, fmt.Errorf("unsupported ref.null heap type %d", heap)
			}
			stack = append(stack, v)
		case 0xd2: // ref.func
			idx, err := r.U32()
			if err != nil {
				return constExprResult{}, err
			}
			stack = append(stack, constExprStackValue{vtype: wasm.FuncRef, global: -1, funcIndex: int(idx)})
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
			v := constExprStackValue{vtype: wasm.V128, global: -1, funcIndex: -1}
			copy(v.v128[:], bb)
			stack = append(stack, v)
		default:
			return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x%02x", op)
		}
	}
}

func evalConstExprWithGlobalCells(expr []byte, typ ValType, cells []*Global, defs []GlobalDef) (uint64, error) {
	want, ok := wasmTypeFromValType(typ)
	if !ok {
		return 0, fmt.Errorf("extended const expression has unsupported result type %s", typ)
	}
	res, err := evalConstExprBytesResolved(expr, want, nil, func(index uint32) (uint64, wasm.ValType, error) {
		i := int(index)
		if i < 0 || i >= len(cells) || i >= len(defs) || cells[i] == nil {
			return 0, wasm.ValType{}, fmt.Errorf("extended const expression global %d is unavailable", index)
		}
		gt, ok := wasmTypeFromValType(defs[i].Type)
		if !ok {
			return 0, wasm.ValType{}, fmt.Errorf("extended const expression global %d has unsupported type %s", index, defs[i].Type)
		}
		return readGlobalObject(cells[i], defs[i].Type), gt, nil
	})
	if err != nil {
		return 0, err
	}
	return res.bits, nil
}

func validateCompiledConstExpr(expr []byte, typ ValType, c *Compiled) error {
	want, ok := wasmTypeFromValType(typ)
	if !ok {
		return fmt.Errorf("unsupported result type %s", typ)
	}
	_, err := evalConstExprBytesResolved(expr, want, nil, func(index uint32) (uint64, wasm.ValType, error) {
		i := int(index)
		if c == nil || i < 0 || i >= len(c.GlobalImports) || i >= len(c.Globals) {
			return 0, wasm.ValType{}, fmt.Errorf("global %d is not an imported global", index)
		}
		g := c.Globals[i]
		if g.Mutable {
			return 0, wasm.ValType{}, fmt.Errorf("global %d is mutable", index)
		}
		gt, ok := wasmTypeFromValType(g.Type)
		if !ok {
			return 0, wasm.ValType{}, fmt.Errorf("global %d has unsupported type %s", index, g.Type)
		}
		return 0, gt, nil
	})
	return err
}

func wasmTypeFromValType(t ValType) (wasm.ValType, bool) {
	switch t {
	case ValI32:
		return wasm.I32, true
	case ValI64:
		return wasm.I64, true
	case ValF32:
		return wasm.F32, true
	case ValF64:
		return wasm.F64, true
	case ValV128:
		return wasm.V128, true
	case ValFuncRef:
		return wasm.FuncRef, true
	case ValExternRef:
		return wasm.ExternRef, true
	default:
		return wasm.ValType{}, false
	}
}

func evalConstExprBinary(stack []constExprStackValue, typ wasm.ValType, op byte) ([]constExprStackValue, error) {
	if len(stack) < 2 {
		return nil, fmt.Errorf("const expression stack underflow at opcode 0x%02x", op)
	}
	a, b := stack[len(stack)-2], stack[len(stack)-1]
	if !valTypeEqual(a.vtype, typ) || !valTypeEqual(b.vtype, typ) {
		return nil, fmt.Errorf("const expression opcode 0x%02x operand type mismatch", op)
	}
	v := constExprStackValue{vtype: typ, unresolved: a.unresolved || b.unresolved, global: -1, funcIndex: -1}
	if valTypeEqual(typ, wasm.I32) {
		x, y := uint32(a.bits), uint32(b.bits)
		switch op {
		case 0x6a:
			v.bits = uint64(x + y)
		case 0x6b:
			v.bits = uint64(x - y)
		case 0x6c:
			v.bits = uint64(x * y)
		}
	} else {
		switch op {
		case 0x7c:
			v.bits = a.bits + b.bits
		case 0x7d:
			v.bits = a.bits - b.bits
		case 0x7e:
			v.bits = a.bits * b.bits
		}
	}
	return append(stack[:len(stack)-2], v), nil
}

// evalConstExprWithModule intentionally stays narrower than full wasm validation:
// wasm.ValidateModule checks shape and type rules before compile reaches here,
// while this helper folds the supported literal/arithmetic operators or records
// deferred imported-global expressions for instantiation.
func evalConstExprWithModule(e wasm.Expr, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
	if len(e.Instrs) == 0 && len(e.BodyBytes) != 0 {
		return evalConstExprBytesWithModule(e.BodyBytes, want, m)
	}
	if len(e.Instrs) > 1 {
		body, err := encodeExtendedConstInstructions(e.Instrs)
		if err != nil {
			return constExprResult{}, err
		}
		return evalConstExprBytesWithModule(body, want, m)
	}
	if len(e.Instrs) != 1 {
		return constExprResult{}, fmt.Errorf("const expression must contain one instruction")
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
		if !wasm.EqualValType(refType, wasm.FuncRef) && !wasm.EqualValType(refType, wasm.ExternRef) {
			return constExprResult{}, fmt.Errorf("unsupported ref.null type %s", refType)
		}
		got.bits, got.vtype = 0, refType
	case wasm.InstrRefFunc:
		got.vtype, got.FuncIndex = wasm.FuncRef, int(in.Index)
	case wasm.InstrGlobalGet:
		if m == nil {
			return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x23")
		}
		gt, ok := m.GlobalTypeByIndex(in.Index)
		if !ok || int(in.Index) >= m.ImportedGlobalCount() || gt.Mutable {
			return constExprResult{}, fmt.Errorf("unsupported const expression global.get %d", in.Index)
		}
		got.bits, got.vtype = 0, gt.Type
		got.GlobalIndex = int(in.Index)
	default:
		return constExprResult{}, fmt.Errorf("unsupported const expression opcode %s", in.Kind)
	}
	if !valTypeEqual(got.vtype, want) {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.vtype, want)
	}
	return got, nil
}

func encodeExtendedConstInstructions(instrs []wasm.Instruction) ([]byte, error) {
	out := make([]byte, 0, len(instrs)*2+1)
	for _, in := range instrs {
		switch in.Kind {
		case wasm.InstrI32Const:
			out = append(out, 0x41)
			out = appendSignedLEB(out, int64(in.I32), 32)
		case wasm.InstrI64Const:
			out = append(out, 0x42)
			out = appendSignedLEB(out, in.I64, 64)
		case wasm.InstrGlobalGet:
			out = append(out, 0x23)
			out = appendUnsignedLEB(out, uint64(in.Index))
		case wasm.InstrI32Add:
			out = append(out, 0x6a)
		case wasm.InstrI32Sub:
			out = append(out, 0x6b)
		case wasm.InstrI32Mul:
			out = append(out, 0x6c)
		case wasm.InstrI64Add:
			out = append(out, 0x7c)
		case wasm.InstrI64Sub:
			out = append(out, 0x7d)
		case wasm.InstrI64Mul:
			out = append(out, 0x7e)
		default:
			return nil, fmt.Errorf("unsupported extended const expression opcode %s", in.Kind)
		}
	}
	return append(out, 0x0b), nil
}

func appendUnsignedLEB(dst []byte, value uint64) []byte {
	for {
		b := byte(value & 0x7f)
		value >>= 7
		if value == 0 {
			return append(dst, b)
		}
		dst = append(dst, b|0x80)
	}
}

func appendSignedLEB(dst []byte, value int64, bits int) []byte {
	remaining := bits
	for {
		b := byte(value & 0x7f)
		value >>= 7
		remaining -= 7
		done := (value == 0 && b&0x40 == 0) || (value == -1 && b&0x40 != 0) || remaining <= 0
		if done {
			return append(dst, b)
		}
		dst = append(dst, b|0x80)
	}
}
