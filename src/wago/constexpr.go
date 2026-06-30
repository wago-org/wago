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
	GlobalIndex int
}

func (i constExprInit) GlobalRef() (int, bool) { return i.GlobalIndex, i.GlobalIndex >= 0 }

type constExprResult struct {
	Value
	GlobalIndex int
}

func (r constExprResult) Init() constExprInit {
	return constExprInit{Bits: r.bits, GlobalIndex: r.GlobalIndex}
}

func applyGlobalInit(g *GlobalDef, init constExprInit) {
	g.Bits = init.Bits
	if idx, ok := init.GlobalRef(); ok {
		g.HasInitGlobal = true
		g.InitGlobal = idx
	}
}

func applyOffsetInit(o *OffsetInit, init constExprInit) {
	o.Base = uint32(init.Bits)
	if idx, ok := init.GlobalRef(); ok {
		o.HasGlobal = true
		o.Global = idx
	}
}

func applyElemOffset(e *ElemInit, init constExprInit) { applyOffsetInit(&e.Offset, init) }
func applyDataOffset(d *DataInit, init constExprInit) { applyOffsetInit(&d.Offset, init) }

func evalConstExpr(b []byte, want wasm.ValType) (Value, error) {
	res, err := evalConstExprBytes(b, want)
	return res.Value, err
}

func evalConstExprBytes(b []byte, want wasm.ValType) (constExprResult, error) {
	r := wasm.NewReader(b)
	op, err := r.Byte()
	if err != nil {
		return constExprResult{}, err
	}
	got := constExprResult{GlobalIndex: -1}
	switch op {
	case 0x41: // i32.const
		v, err := r.I32()
		if err != nil {
			return constExprResult{}, err
		}
		got.Value = valueOf(wasm.I32, uint64(uint32(v)))
	case 0x42: // i64.const
		v, err := r.I64()
		if err != nil {
			return constExprResult{}, err
		}
		got.Value = valueOf(wasm.I64, uint64(v))
	case 0x43: // f32.const
		bb, err := r.Bytes(4)
		if err != nil {
			return constExprResult{}, err
		}
		got.Value = valueOf(wasm.F32, uint64(binary.LittleEndian.Uint32(bb)))
	case 0x44: // f64.const
		bb, err := r.Bytes(8)
		if err != nil {
			return constExprResult{}, err
		}
		got.Value = valueOf(wasm.F64, binary.LittleEndian.Uint64(bb))
	default:
		return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x%02x", op)
	}
	end, err := r.Byte()
	if err != nil {
		return constExprResult{}, fmt.Errorf("const expression missing end: %w", err)
	}
	if end != 0x0B {
		return constExprResult{}, fmt.Errorf("const expression missing end")
	}
	if r.BytesLeft() != 0 {
		return constExprResult{}, fmt.Errorf("const expression has trailing bytes")
	}
	if !valTypeEqual(got.Type(), want) {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.Type(), want)
	}
	return got, nil
}

// evalConstExprWithModule intentionally stays narrower than full wasm validation:
// wasm.ValidateModule checks const-expression shape/type rules before compile
// reaches here, while this helper converts the supported MVP operators into
// instantiate-time bits or deferred imported-global references.
func evalConstExprWithModule(e wasm.Expr, want wasm.ValType, m *wasm.Module) (constExprResult, error) {
	if len(e.Instrs) != 1 {
		return constExprResult{}, fmt.Errorf("const expression must contain one instruction")
	}
	in := e.Instrs[0]
	got := constExprResult{GlobalIndex: -1}
	switch in.Kind {
	case wasm.InstrI32Const:
		got.Value = valueOf(wasm.I32, uint64(uint32(in.I32)))
	case wasm.InstrI64Const:
		got.Value = valueOf(wasm.I64, uint64(in.I64))
	case wasm.InstrF32Const:
		got.Value = valueOf(wasm.F32, uint64(in.F32Bits))
	case wasm.InstrF64Const:
		got.Value = valueOf(wasm.F64, in.F64Bits)
	case wasm.InstrGlobalGet:
		if m == nil {
			return constExprResult{}, fmt.Errorf("unsupported const expression opcode 0x23")
		}
		gt, ok := m.GlobalTypeByIndex(in.Index)
		if !ok || int(in.Index) >= m.ImportedGlobalCount() || gt.Mutable {
			return constExprResult{}, fmt.Errorf("unsupported const expression global.get %d", in.Index)
		}
		got.Value = valueOf(gt.Type, 0)
		got.GlobalIndex = int(in.Index)
	default:
		return constExprResult{}, fmt.Errorf("unsupported const expression opcode %s", in.Kind)
	}
	if !valTypeEqual(got.Type(), want) {
		return constExprResult{}, fmt.Errorf("const expression type %s, want %s", got.Type(), want)
	}
	return got, nil
}
