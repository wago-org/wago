package ir

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func VerifyModule(m *Module) error {
	if m == nil {
		return fmt.Errorf("ir: nil module")
	}
	for i := range m.FuncTypes {
		if int(m.FuncTypes[i]) >= len(m.Types) {
			return fmt.Errorf("ir: function %d has unknown type %d", i, m.FuncTypes[i])
		}
	}
	for i := range m.Funcs {
		if err := VerifyFunc(&m.Funcs[i]); err != nil {
			return fmt.Errorf("ir: func %d: %w", i, err)
		}
	}
	return nil
}

func VerifyFunc(f *Func) error {
	if f == nil {
		return fmt.Errorf("ir: nil func")
	}
	if int(f.Entry) >= len(f.Blocks) {
		return fmt.Errorf("entry block %d out of range", f.Entry)
	}
	for i := range f.Values {
		v := f.Values[i]
		if !validValType(v.Type) {
			return fmt.Errorf("value %d has invalid type %s", i, v.Type)
		}
		switch v.DefKind {
		case ValueDefBlockParam:
			if int(v.Def) >= len(f.Blocks) {
				return fmt.Errorf("value %d has invalid block def %d", i, v.Def)
			}
		case ValueDefInst:
			if int(v.Def) >= len(f.Insts) {
				return fmt.Errorf("value %d has invalid inst def %d", i, v.Def)
			}
		case ValueDefPoison:
			// Poison values are allowed only as dead values; edge/inst checks below reject reachable uses.
		default:
			return fmt.Errorf("value %d has invalid def kind %d", i, v.DefKind)
		}
	}
	for i := range f.Insts {
		if err := verifyInst(f, InstID(i), &f.Insts[i]); err != nil {
			return err
		}
	}
	covered := make([]bool, len(f.Insts))
	for bi := range f.Blocks {
		b := &f.Blocks[bi]
		if err := verifyValueRange(f, b.Params, "block params"); err != nil {
			return fmt.Errorf("block %d: %w", bi, err)
		}
		for j := b.Params.Start; j < b.Params.End(); j++ {
			v := f.ValueIDs[j]
			if f.Values[v].DefKind != ValueDefBlockParam || f.Values[v].Def != uint32(bi) {
				return fmt.Errorf("block %d param value %d has wrong def", bi, v)
			}
		}
		if b.Term.Kind == TermInvalid {
			return fmt.Errorf("block %d has no terminator", bi)
		}
		if int(b.Insts.End()) > len(f.Insts) {
			return fmt.Errorf("block %d instruction range out of bounds", bi)
		}
		for j := b.Insts.Start; j < b.Insts.End(); j++ {
			if covered[j] {
				return fmt.Errorf("inst %d appears in multiple blocks", j)
			}
			covered[j] = true
		}
		if err := verifyTerm(f, BlockID(bi), &b.Term); err != nil {
			return err
		}
	}
	for i, ok := range covered {
		if !ok {
			return fmt.Errorf("inst %d is not in any block", i)
		}
	}
	return nil
}

func verifyInst(f *Func, id InstID, in *Inst) error {
	if in.Op == OpInvalid {
		return fmt.Errorf("inst %d has invalid op", id)
	}
	if err := verifyValueRange(f, in.Args, fmt.Sprintf("inst %d args", id)); err != nil {
		return err
	}
	if err := verifyValueRange(f, in.Results, fmt.Sprintf("inst %d results", id)); err != nil {
		return err
	}
	for _, v := range f.ValueIDs[in.Args.Start:in.Args.End()] {
		if f.Values[v].DefKind == ValueDefPoison {
			return fmt.Errorf("inst %d uses poison value %d", id, v)
		}
	}
	for _, v := range f.ValueIDs[in.Results.Start:in.Results.End()] {
		if f.Values[v].DefKind != ValueDefInst || f.Values[v].Def != uint32(id) {
			return fmt.Errorf("inst %d result value %d has wrong def", id, v)
		}
	}
	argc := int(in.Args.Len)
	resc := int(in.Results.Len)
	argt := func(i int) wasm.ValType { return f.Values[f.ValueIDs[in.Args.Start+uint32(i)]].Type }
	rest := func(i int) wasm.ValType { return f.Values[f.ValueIDs[in.Results.Start+uint32(i)]].Type }
	want := func(a, r int) error {
		if argc != a || resc != r {
			return fmt.Errorf("inst %d %s has args/results %d/%d, want %d/%d", id, opName(in.Op), argc, resc, a, r)
		}
		return nil
	}
	sameArgRes := func(a int) error {
		if err := want(a, 1); err != nil {
			return err
		}
		for i := 0; i < a; i++ {
			if argt(i) != rest(0) {
				return fmt.Errorf("inst %d type mismatch", id)
			}
		}
		return nil
	}
	switch in.Op {
	case OpConst, OpLocalGet, OpGlobalGet, OpMemorySize:
		return want(0, 1)
	case OpLocalSet, OpGlobalSet:
		return want(1, 0)
	case OpLocalTee:
		if err := want(1, 1); err != nil {
			return err
		}
		if argt(0) != rest(0) {
			return fmt.Errorf("inst %d local.tee type mismatch", id)
		}
	case OpIUnary, OpFUnary, OpConvert, OpReinterpret:
		return want(1, 1)
	case OpIBinary, OpFBinary:
		return sameArgRes(2)
	case OpICmp, OpFCmp:
		if err := want(2, 1); err != nil {
			return err
		}
		if argt(0) != argt(1) || rest(0) != wasm.I32 {
			return fmt.Errorf("inst %d compare type mismatch", id)
		}
	case OpITest:
		if err := want(1, 1); err != nil {
			return err
		}
		if rest(0) != wasm.I32 {
			return fmt.Errorf("inst %d test result is not i32", id)
		}
	case OpSelect:
		if err := want(3, 1); err != nil {
			return err
		}
		if argt(0) != argt(1) || argt(0) != rest(0) || argt(2) != wasm.I32 {
			return fmt.Errorf("inst %d select type mismatch", id)
		}
	case OpLoad:
		if err := want(1, 1); err != nil {
			return err
		}
		if argt(0) != wasm.I32 {
			return fmt.Errorf("inst %d load address is not i32", id)
		}
		if (in.Effects&EffectCanTrap) == 0 || (in.Effects&EffectReadMem) == 0 {
			return fmt.Errorf("inst %d load missing effects", id)
		}
	case OpStore:
		if err := want(2, 0); err != nil {
			return err
		}
		if argt(0) != wasm.I32 {
			return fmt.Errorf("inst %d store address is not i32", id)
		}
		if (in.Effects&EffectCanTrap) == 0 || (in.Effects&EffectWriteMem) == 0 {
			return fmt.Errorf("inst %d store missing effects", id)
		}
	case OpMemoryGrow:
		if err := want(1, 1); err != nil {
			return err
		}
		if argt(0) != wasm.I32 || rest(0) != wasm.I32 {
			return fmt.Errorf("inst %d memory.grow type mismatch", id)
		}
	case OpMemoryCopy, OpMemoryFill:
		if err := want(3, 0); err != nil {
			return err
		}
		for i := 0; i < 3; i++ {
			if argt(i) != wasm.I32 {
				return fmt.Errorf("inst %d memory bulk arg %d not i32", id, i)
			}
		}
	case OpCall, OpCallImport, OpCallIndirect:
		if (in.Effects&EffectCall) == 0 || (in.Effects&EffectCanTrap) == 0 {
			return fmt.Errorf("inst %d call missing effects", id)
		}
	default:
		return fmt.Errorf("inst %d has unsupported op %d", id, in.Op)
	}
	return nil
}

func verifyTerm(f *Func, bid BlockID, t *Term) error {
	switch t.Kind {
	case TermBr:
		if t.Edges.Len != 1 {
			return fmt.Errorf("block %d br has %d edges", bid, t.Edges.Len)
		}
		return verifyEdges(f, bid, t.Edges)
	case TermCondBr:
		if t.Edges.Len != 2 {
			return fmt.Errorf("block %d condbr has %d edges", bid, t.Edges.Len)
		}
		if err := verifyValue(f, t.Cond, "condbr condition"); err != nil {
			return fmt.Errorf("block %d: %w", bid, err)
		}
		if f.Values[t.Cond].Type != wasm.I32 {
			return fmt.Errorf("block %d condbr condition is not i32", bid)
		}
		return verifyEdges(f, bid, t.Edges)
	case TermSwitch:
		if t.Edges.Len == 0 {
			return fmt.Errorf("block %d switch has no edges", bid)
		}
		if err := verifyValue(f, t.Index, "switch index"); err != nil {
			return fmt.Errorf("block %d: %w", bid, err)
		}
		if f.Values[t.Index].Type != wasm.I32 {
			return fmt.Errorf("block %d switch index is not i32", bid)
		}
		return verifyEdges(f, bid, t.Edges)
	case TermReturn:
		if err := verifyValueRange(f, t.Args, "return args"); err != nil {
			return fmt.Errorf("block %d: %w", bid, err)
		}
		if int(t.Args.Len) != len(f.Sig.Results) {
			return fmt.Errorf("block %d return arity %d, want %d", bid, t.Args.Len, len(f.Sig.Results))
		}
		for i := range f.Sig.Results {
			v := f.ValueIDs[t.Args.Start+uint32(i)]
			if f.Values[v].Type != f.Sig.Results[i] {
				return fmt.Errorf("block %d return arg %d type %s, want %s", bid, i, f.Values[v].Type, f.Sig.Results[i])
			}
			if f.Values[v].DefKind == ValueDefPoison {
				return fmt.Errorf("block %d returns poison value %d", bid, v)
			}
		}
	case TermTrap:
	case TermInvalid:
		return fmt.Errorf("block %d invalid terminator", bid)
	default:
		return fmt.Errorf("block %d unknown terminator %d", bid, t.Kind)
	}
	return nil
}

func verifyEdges(f *Func, bid BlockID, r Range) error {
	if int(r.End()) > len(f.Edges) {
		return fmt.Errorf("block %d edge range out of bounds", bid)
	}
	for ei := r.Start; ei < r.End(); ei++ {
		e := f.Edges[ei]
		if int(e.To) >= len(f.Blocks) {
			return fmt.Errorf("block %d edge %d target %d out of range", bid, ei, e.To)
		}
		if err := verifyValueRange(f, e.Args, "edge args"); err != nil {
			return fmt.Errorf("block %d edge %d: %w", bid, ei, err)
		}
		params := f.Blocks[e.To].Params
		if e.Args.Len != params.Len {
			return fmt.Errorf("block %d edge %d arg arity %d, target b%d params %d", bid, ei, e.Args.Len, e.To, params.Len)
		}
		for j := uint32(0); j < e.Args.Len; j++ {
			a := f.ValueIDs[e.Args.Start+j]
			p := f.ValueIDs[params.Start+j]
			if f.Values[a].DefKind == ValueDefPoison {
				return fmt.Errorf("block %d edge %d uses poison value %d", bid, ei, a)
			}
			if f.Values[a].Type != f.Values[p].Type {
				return fmt.Errorf("block %d edge %d arg %d type %s, want %s", bid, ei, j, f.Values[a].Type, f.Values[p].Type)
			}
		}
	}
	return nil
}

func verifyValueRange(f *Func, r Range, what string) error {
	if int(r.End()) > len(f.ValueIDs) {
		return fmt.Errorf("%s range out of bounds", what)
	}
	for _, v := range f.ValueIDs[r.Start:r.End()] {
		if err := verifyValue(f, v, what); err != nil {
			return err
		}
	}
	return nil
}
func verifyValue(f *Func, v ValueID, what string) error {
	if v == InvalidValue || int(v) >= len(f.Values) {
		return fmt.Errorf("%s invalid value %d", what, v)
	}
	return nil
}
