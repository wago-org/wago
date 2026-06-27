package ir

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// VerifyModule checks every function with module metadata, including memory,
// table, global, and function-index references. Prefer this entry point for IR
// produced from a complete module.
func VerifyModule(m *Module) error {
	if m == nil {
		return fmt.Errorf("ir: nil module")
	}
	if len(m.Memories) > 1 {
		return fmt.Errorf("ir: multi-memory unsupported")
	}
	for i := range m.FuncTypes {
		if int(m.FuncTypes[i]) >= len(m.Types) {
			return fmt.Errorf("ir: function %d has unknown type %d", i, m.FuncTypes[i])
		}
	}
	for i := range m.Funcs {
		if err := verifyFunc(&m.Funcs[i], m); err != nil {
			return fmt.Errorf("ir: func %d: %w", i, err)
		}
	}
	return nil
}

// VerifyFunc checks a standalone function's shape, value definitions, effects,
// and dominance. It cannot validate module-indexed references such as memories,
// tables, globals, or callees; use VerifyModule when module metadata is
// available.
func VerifyFunc(f *Func) error {
	return verifyFunc(f, nil)
}

// VerifyFuncInModule checks one function with module metadata. It is useful for
// focused tests and tools that build or mutate a single function but still need
// index validation.
func VerifyFuncInModule(f *Func, m *Module) error {
	return verifyFunc(f, m)
}

func verifyFunc(f *Func, m *Module) error {
	if f == nil {
		return fmt.Errorf("ir: nil func")
	}
	if int(f.Entry) >= len(f.Blocks) {
		return fmt.Errorf("entry block %d out of range", f.Entry)
	}
	if err := verifyLocalLayout(f); err != nil {
		return err
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
		if err := verifyInst(f, m, InstID(i), &f.Insts[i]); err != nil {
			return err
		}
	}
	covered := make([]bool, len(f.Insts))
	for bi := range f.Blocks {
		b := &f.Blocks[bi]
		paramsEnd, err := verifyValueRange(f, b.Params, "block params")
		if err != nil {
			return fmt.Errorf("block %d: %w", bi, err)
		}
		for j := b.Params.Start; j < paramsEnd; j++ {
			v := f.ValueIDs[j]
			if f.Values[v].DefKind != ValueDefBlockParam || f.Values[v].Def != uint32(bi) {
				return fmt.Errorf("block %d param value %d has wrong def", bi, v)
			}
		}
		if b.Term.Kind == TermInvalid {
			return fmt.Errorf("block %d has no terminator", bi)
		}
		instEnd, err := verifyRange(b.Insts, len(f.Insts), fmt.Sprintf("block %d instruction", bi))
		if err != nil {
			return err
		}
		for j := b.Insts.Start; j < instEnd; j++ {
			if covered[j] {
				return fmt.Errorf("inst %d appears in multiple blocks", j)
			}
			covered[j] = true
		}
		if err := verifyTerm(f, BlockID(bi), &b.Term); err != nil {
			return err
		}
		if b.Flags&BlockSyntheticReturn != 0 && b.Term.Kind != TermReturn {
			return fmt.Errorf("block %d synthetic return flag on %d terminator", bi, b.Term.Kind)
		}
	}
	for i, ok := range covered {
		if !ok {
			return fmt.Errorf("inst %d is not in any block", i)
		}
	}
	return verifyDominance(f)
}

func verifyLocalLayout(f *Func) error {
	// Locals are explicit mutable state in this IR stage. The compact layout keeps
	// function parameters in Locals and declared locals in LocalRuns, but hand-built
	// test IR may still use a fully expanded Locals slice with no runs. Verify the
	// parameter prefix in both cases so local indexes always follow Wasm order.
	if len(f.Locals) < len(f.Sig.Params) {
		return fmt.Errorf("locals prefix has %d params, want %d", len(f.Locals), len(f.Sig.Params))
	}
	for i, want := range f.Sig.Params {
		if f.Locals[i] != want {
			return fmt.Errorf("local %d type %s, want param type %s", i, f.Locals[i], want)
		}
	}
	for i, run := range f.LocalRuns {
		if !validValType(run.Type) {
			return fmt.Errorf("local run %d has invalid type %s", i, run.Type)
		}
	}
	return nil
}

func verifyInst(f *Func, m *Module, id InstID, in *Inst) error {
	if in.Op == OpInvalid {
		return fmt.Errorf("inst %d has invalid op", id)
	}
	if _, err := verifyValueRange(f, in.Args, fmt.Sprintf("inst %d args", id)); err != nil {
		return err
	}
	if _, err := verifyValueRange(f, in.Results, fmt.Sprintf("inst %d results", id)); err != nil {
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
	case OpConst:
		return want(0, 1)
	case OpLocalGet:
		if err := want(0, 1); err != nil {
			return err
		}
		return verifyLocalAccess(f, id, in, rest(0), EffectReadLocal)
	case OpLocalSet:
		if err := want(1, 0); err != nil {
			return err
		}
		return verifyLocalAccess(f, id, in, argt(0), EffectWriteLocal)
	case OpLocalTee:
		if err := want(1, 1); err != nil {
			return err
		}
		if argt(0) != rest(0) {
			return fmt.Errorf("inst %d local.tee type mismatch", id)
		}
		if in.Effects&EffectReadLocal != 0 {
			return fmt.Errorf("inst %d local.tee has read effect", id)
		}
		return verifyLocalAccess(f, id, in, argt(0), EffectWriteLocal)
	case OpGlobalGet:
		if err := want(0, 1); err != nil {
			return err
		}
		if (in.Effects & EffectReadGlobal) == 0 {
			return fmt.Errorf("inst %d global.get missing effect", id)
		}
		return verifyGlobalAccess(m, id, in, rest(0))
	case OpGlobalSet:
		if err := want(1, 0); err != nil {
			return err
		}
		if (in.Effects & EffectWriteGlobal) == 0 {
			return fmt.Errorf("inst %d global.set missing effect", id)
		}
		return verifyGlobalAccess(m, id, in, argt(0))
	case OpIUnary:
		if err := want(1, 1); err != nil {
			return err
		}
		return verifyIUnary(id, in, argt(0), rest(0))
	case OpIBinary:
		if err := sameArgRes(2); err != nil {
			return err
		}
		return verifyIntAux(id, in, argt(0), uint8(IBinAdd), uint8(IBinRotr), "integer binary")
	case OpICmp:
		if err := want(2, 1); err != nil {
			return err
		}
		if argt(0) != argt(1) || rest(0) != wasm.I32 {
			return fmt.Errorf("inst %d compare type mismatch", id)
		}
		return verifyIntAux(id, in, argt(0), uint8(ICmpEq), uint8(ICmpGeU), "integer compare")
	case OpITest:
		if err := want(1, 1); err != nil {
			return err
		}
		if rest(0) != wasm.I32 {
			return fmt.Errorf("inst %d test result is not i32", id)
		}
		return verifyIntAux(id, in, argt(0), uint8(ITestEqz), uint8(ITestEqz), "integer test")
	case OpFUnary:
		if err := want(1, 1); err != nil {
			return err
		}
		return verifyFloatAux(id, in, argt(0), rest(0), uint8(FUnAbs), uint8(FUnSqrt), "float unary")
	case OpFBinary:
		if err := sameArgRes(2); err != nil {
			return err
		}
		return verifyFloatAux(id, in, argt(0), rest(0), uint8(FBinAdd), uint8(FBinCopySign), "float binary")
	case OpFCmp:
		if err := want(2, 1); err != nil {
			return err
		}
		if argt(0) != argt(1) || rest(0) != wasm.I32 {
			return fmt.Errorf("inst %d compare type mismatch", id)
		}
		return verifyFloatAux(id, in, argt(0), argt(0), uint8(FCmpEq), uint8(FCmpGe), "float compare")
	case OpConvert:
		if err := want(1, 1); err != nil {
			return err
		}
		if !validConvert(argt(0), rest(0), ConvertOp(auxKind(in.Aux))) || auxType(in.Aux) != rest(0) {
			return fmt.Errorf("inst %d convert type mismatch", id)
		}
	case OpReinterpret:
		if err := want(1, 1); err != nil {
			return err
		}
		if !validReinterpret(argt(0), rest(0), ReinterpretOp(auxKind(in.Aux))) || auxType(in.Aux) != rest(0) {
			return fmt.Errorf("inst %d reinterpret type mismatch", id)
		}
	case OpSelect:
		if err := want(3, 1); err != nil {
			return err
		}
		if argt(0) != argt(1) || argt(0) != rest(0) || argt(2) != wasm.I32 || wasm.ValType(byte(in.Aux)) != rest(0) {
			return fmt.Errorf("inst %d select type mismatch", id)
		}
	case OpLoad:
		if err := want(1, 1); err != nil {
			return err
		}
		if argt(0) != wasm.I32 {
			return fmt.Errorf("inst %d load address is not i32", id)
		}
		if got, ok := memLoadResult(memKind(in.Aux)); !ok || got != rest(0) {
			return fmt.Errorf("inst %d load type mismatch", id)
		}
		if err := verifyMemoryIndex(m, id, memIndex(in.Aux)); err != nil {
			return err
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
		if got, ok := memStoreValue(memKind(in.Aux)); !ok || got != argt(1) {
			return fmt.Errorf("inst %d store type mismatch", id)
		}
		if err := verifyMemoryIndex(m, id, memIndex(in.Aux)); err != nil {
			return err
		}
		if (in.Effects&EffectCanTrap) == 0 || (in.Effects&EffectWriteMem) == 0 {
			return fmt.Errorf("inst %d store missing effects", id)
		}
	case OpMemorySize:
		if err := want(0, 1); err != nil {
			return err
		}
		if rest(0) != wasm.I32 {
			return fmt.Errorf("inst %d memory.size type mismatch", id)
		}
		if err := verifyMemoryIndex(m, id, uint32(in.Aux)); err != nil {
			return err
		}
		if (in.Effects & EffectReadMem) == 0 {
			return fmt.Errorf("inst %d memory.size missing effects", id)
		}
	case OpMemoryGrow:
		if err := want(1, 1); err != nil {
			return err
		}
		if argt(0) != wasm.I32 || rest(0) != wasm.I32 {
			return fmt.Errorf("inst %d memory.grow type mismatch", id)
		}
		if err := verifyMemoryIndex(m, id, uint32(in.Aux)); err != nil {
			return err
		}
		if (in.Effects&EffectCanTrap) == 0 || (in.Effects&EffectReadMem) == 0 || (in.Effects&EffectWriteMem) == 0 {
			return fmt.Errorf("inst %d memory.grow missing effects", id)
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
		if in.Op == OpMemoryCopy {
			if err := verifyMemoryIndex(m, id, uint32(in.Aux)); err != nil {
				return err
			}
			if err := verifyMemoryIndex(m, id, uint32(in.Aux>>32)); err != nil {
				return err
			}
			if (in.Effects&EffectCanTrap) == 0 || (in.Effects&EffectReadMem) == 0 || (in.Effects&EffectWriteMem) == 0 {
				return fmt.Errorf("inst %d memory.copy missing effects", id)
			}
		} else {
			if err := verifyMemoryIndex(m, id, uint32(in.Aux)); err != nil {
				return err
			}
			if (in.Effects&EffectCanTrap) == 0 || (in.Effects&EffectWriteMem) == 0 {
				return fmt.Errorf("inst %d memory.fill missing effects", id)
			}
		}
	case OpCall, OpCallImport, OpCallIndirect:
		if err := verifyCallEffects(id, in); err != nil {
			return err
		}
		return verifyCall(m, id, in, argc, resc, argt, rest)
	default:
		return fmt.Errorf("inst %d has unsupported op %d", id, in.Op)
	}
	return nil
}

func verifyLocalAccess(f *Func, id InstID, in *Inst, got wasm.ValType, required EffectFlags) error {
	idx := uint32(in.Aux)
	want, ok := localType(f, idx)
	if !ok {
		return fmt.Errorf("inst %d local index %d out of range", id, idx)
	}
	if want != got {
		return fmt.Errorf("inst %d local type %s, want %s", id, got, want)
	}
	if in.Effects&required != required {
		return fmt.Errorf("inst %d local access missing effects", id)
	}
	return nil
}

func verifyGlobalAccess(m *Module, id InstID, in *Inst, got wasm.ValType) error {
	if m == nil {
		return nil
	}
	idx := uint32(in.Aux)
	if int(idx) >= len(m.Globals) {
		return fmt.Errorf("inst %d global index %d out of range", id, idx)
	}
	if m.Globals[idx].Val != got {
		return fmt.Errorf("inst %d global type %s, want %s", id, got, m.Globals[idx].Val)
	}
	return nil
}

func verifyIntAux(id InstID, in *Inst, t wasm.ValType, min, max uint8, what string) error {
	k := auxKind(in.Aux)
	if (t != wasm.I32 && t != wasm.I64) || auxType(in.Aux) != t || k < min || k > max {
		return fmt.Errorf("inst %d %s type mismatch", id, what)
	}
	return nil
}

func verifyIUnary(id InstID, in *Inst, arg, res wasm.ValType) error {
	if arg != res {
		return fmt.Errorf("inst %d integer unary type mismatch", id)
	}
	if err := verifyIntAux(id, in, arg, uint8(IUnClz), uint8(IUnExtend32S), "integer unary"); err != nil {
		return err
	}
	if IUnaryOp(auxKind(in.Aux)) == IUnExtend32S && arg != wasm.I64 {
		return fmt.Errorf("inst %d integer unary type mismatch", id)
	}
	return nil
}

func verifyFloatAux(id InstID, in *Inst, arg, res wasm.ValType, min, max uint8, what string) error {
	k := auxKind(in.Aux)
	if arg != res || (arg != wasm.F32 && arg != wasm.F64) || auxType(in.Aux) != arg || k < min || k > max {
		return fmt.Errorf("inst %d %s type mismatch", id, what)
	}
	return nil
}

func validConvert(src, dst wasm.ValType, k ConvertOp) bool {
	switch k {
	case ConvWrapI64ToI32:
		return src == wasm.I64 && dst == wasm.I32
	case ConvTruncFToIS, ConvTruncFToIU, ConvTruncSatFToIS, ConvTruncSatFToIU:
		return (src == wasm.F32 || src == wasm.F64) && (dst == wasm.I32 || dst == wasm.I64)
	case ConvExtendI32S, ConvExtendI32U:
		return src == wasm.I32 && dst == wasm.I64
	case ConvConvertIToFS, ConvConvertIToFU:
		return (src == wasm.I32 || src == wasm.I64) && (dst == wasm.F32 || dst == wasm.F64)
	case ConvDemoteF64ToF32:
		return src == wasm.F64 && dst == wasm.F32
	case ConvPromoteF32ToF64:
		return src == wasm.F32 && dst == wasm.F64
	default:
		return false
	}
}

func validReinterpret(src, dst wasm.ValType, k ReinterpretOp) bool {
	switch k {
	case ReinterpF32ToI32:
		return src == wasm.F32 && dst == wasm.I32
	case ReinterpF64ToI64:
		return src == wasm.F64 && dst == wasm.I64
	case ReinterpI32ToF32:
		return src == wasm.I32 && dst == wasm.F32
	case ReinterpI64ToF64:
		return src == wasm.I64 && dst == wasm.F64
	default:
		return false
	}
}

func memLoadResult(k MemOp) (wasm.ValType, bool) {
	switch k {
	case MemI32, MemI32Load8S, MemI32Load8U, MemI32Load16S, MemI32Load16U:
		return wasm.I32, true
	case MemI64, MemI64Load8S, MemI64Load8U, MemI64Load16S, MemI64Load16U, MemI64Load32S, MemI64Load32U:
		return wasm.I64, true
	case MemF32:
		return wasm.F32, true
	case MemF64:
		return wasm.F64, true
	default:
		return 0, false
	}
}

func memStoreValue(k MemOp) (wasm.ValType, bool) {
	switch k {
	case MemI32, MemI32Store8, MemI32Store16:
		return wasm.I32, true
	case MemI64, MemI64Store8, MemI64Store16, MemI64Store32:
		return wasm.I64, true
	case MemF32:
		return wasm.F32, true
	case MemF64:
		return wasm.F64, true
	default:
		return 0, false
	}
}

func verifyMemoryIndex(m *Module, id InstID, idx uint32) error {
	// The IR is deliberately single-memory until wago implements multi-memory
	// end-to-end. Reject non-zero indexes even if hand-built metadata contains
	// multiple memories, rather than letting codegen silently lower memory 0.
	if idx != 0 {
		return fmt.Errorf("inst %d multi-memory unsupported: memory index %d", id, idx)
	}
	if m != nil && int(idx) >= len(m.Memories) {
		return fmt.Errorf("inst %d memory index %d out of range", id, idx)
	}
	return nil
}

func verifyCallEffects(id InstID, in *Inst) error {
	// Call effect flags are opcode-level barriers in this IR. Imported calls must
	// stay visible as host boundaries, and indirect calls must stay visible as
	// table reads; otherwise later scheduling/codegen can move work across the
	// wrong boundary or miss the table dependency.
	required := EffectCall | EffectCanTrap
	switch in.Op {
	case OpCallImport:
		required |= EffectHost
	case OpCallIndirect:
		required |= EffectReadTable
	}
	if in.Effects&required != required {
		return fmt.Errorf("inst %d call missing effects", id)
	}
	if in.Op != OpCallImport && in.Effects&EffectHost != 0 {
		return fmt.Errorf("inst %d call has host effect on non-import", id)
	}
	if in.Op != OpCallIndirect && in.Effects&EffectReadTable != 0 {
		return fmt.Errorf("inst %d call has table effect on non-indirect", id)
	}
	return nil
}

func verifyCall(m *Module, id InstID, in *Inst, argc, resc int, argt, rest func(int) wasm.ValType) error {
	if m == nil {
		return nil
	}
	if in.Op == OpCallIndirect {
		typeIdx := callIndirectType(in.Aux)
		tableIdx := callIndirectTable(in.Aux)
		if int(typeIdx) >= len(m.Types) {
			return fmt.Errorf("inst %d call_indirect type %d out of range", id, typeIdx)
		}
		if int(tableIdx) >= len(m.Tables) {
			return fmt.Errorf("inst %d call_indirect table %d out of range", id, tableIdx)
		}
		ft := m.Types[typeIdx]
		if argc != len(ft.Params)+1 || resc != len(ft.Results) {
			return fmt.Errorf("inst %d call_indirect signature arity mismatch", id)
		}
		for i := range ft.Params {
			if argt(i) != ft.Params[i] {
				return fmt.Errorf("inst %d call_indirect arg %d type %s, want %s", id, i, argt(i), ft.Params[i])
			}
		}
		if argt(len(ft.Params)) != wasm.I32 {
			return fmt.Errorf("inst %d call_indirect callee is not i32", id)
		}
		for i := range ft.Results {
			if rest(i) != ft.Results[i] {
				return fmt.Errorf("inst %d call_indirect result %d type %s, want %s", id, i, rest(i), ft.Results[i])
			}
		}
		return nil
	}
	fi := uint32(in.Aux)
	if int(fi) >= len(m.FuncTypes) {
		return fmt.Errorf("inst %d call function %d out of range", id, fi)
	}
	if in.Op == OpCallImport && fi >= m.ImportedFuncCount {
		return fmt.Errorf("inst %d call_import function %d is not imported", id, fi)
	}
	if in.Op == OpCall && fi < m.ImportedFuncCount {
		return fmt.Errorf("inst %d call function %d is imported", id, fi)
	}
	typeIdx := m.FuncTypes[fi]
	if int(typeIdx) >= len(m.Types) {
		return fmt.Errorf("inst %d call function %d has unknown type %d", id, fi, typeIdx)
	}
	ft := m.Types[typeIdx]
	if argc != len(ft.Params) || resc != len(ft.Results) {
		return fmt.Errorf("inst %d call signature arity mismatch", id)
	}
	for i := range ft.Params {
		if argt(i) != ft.Params[i] {
			return fmt.Errorf("inst %d call arg %d type %s, want %s", id, i, argt(i), ft.Params[i])
		}
	}
	for i := range ft.Results {
		if rest(i) != ft.Results[i] {
			return fmt.Errorf("inst %d call result %d type %s, want %s", id, i, rest(i), ft.Results[i])
		}
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
		if _, err := verifyValueRange(f, t.Args, "return args"); err != nil {
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
	end, err := verifyRange(r, len(f.Edges), fmt.Sprintf("block %d edge", bid))
	if err != nil {
		return err
	}
	for ei := r.Start; ei < end; ei++ {
		e := f.Edges[ei]
		if int(e.To) >= len(f.Blocks) {
			return fmt.Errorf("block %d edge %d target %d out of range", bid, ei, e.To)
		}
		if _, err := verifyValueRange(f, e.Args, "edge args"); err != nil {
			return fmt.Errorf("block %d edge %d: %w", bid, ei, err)
		}
		params := f.Blocks[e.To].Params
		if _, err := verifyValueRange(f, params, fmt.Sprintf("target b%d params", e.To)); err != nil {
			return fmt.Errorf("block %d edge %d: %w", bid, ei, err)
		}
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

func verifyDominance(f *Func) error {
	instBlock := make([]BlockID, len(f.Insts))
	for i := range instBlock {
		instBlock[i] = InvalidBlock
	}
	preds := make([][]BlockID, len(f.Blocks))
	succs := make([][]BlockID, len(f.Blocks))
	for bi := range f.Blocks {
		b := &f.Blocks[bi]
		instEnd := b.Insts.End()
		for ii := b.Insts.Start; ii < instEnd; ii++ {
			instBlock[ii] = BlockID(bi)
		}
		if edges, ok := branchEdges(&b.Term); ok {
			edgesEnd := edges.End()
			for ei := edges.Start; ei < edgesEnd; ei++ {
				to := f.Edges[ei].To
				preds[to] = append(preds[to], BlockID(bi))
				succs[bi] = append(succs[bi], to)
			}
		}
	}
	// Use immediate dominators instead of one full bitset per block. That keeps
	// verifier memory linear in blocks+edges and avoids fixed-point bitset
	// allocations for branch-heavy modules on small devices.
	reachable, rpo := reversePostorder(f.Entry, succs)
	idom, order := computeIDoms(f.Entry, preds, reachable, rpo)

	checkUse := func(v ValueID, use BlockID, before InstID, what string) error {
		if err := verifyValue(f, v, what); err != nil {
			return err
		}
		val := f.Values[v]
		if val.DefKind == ValueDefPoison {
			return fmt.Errorf("%s uses poison value %d", what, v)
		}
		var defBlock BlockID
		switch val.DefKind {
		case ValueDefBlockParam:
			defBlock = BlockID(val.Def)
		case ValueDefInst:
			defInst := InstID(val.Def)
			defBlock = instBlock[defInst]
			if defBlock == InvalidBlock {
				return fmt.Errorf("%s value %d has unplaced defining inst %d", what, v, defInst)
			}
			if defBlock == use && before != InvalidInst && defInst >= before {
				return fmt.Errorf("%s value %d is used before its definition", what, v)
			}
		default:
			return fmt.Errorf("%s value %d has invalid def kind %d", what, v, val.DefKind)
		}
		// A value is available at a use only when its defining block dominates the
		// use block. For unreachable blocks there is no CFG path from entry, so keep
		// verification local and reject cross-block values that codegen could not
		// materialize by following predecessor edges.
		if !reachable[use] {
			if defBlock != use {
				return fmt.Errorf("%s value %d from b%d does not dominate unreachable b%d", what, v, defBlock, use)
			}
			return nil
		}
		if int(defBlock) >= len(reachable) || !reachable[defBlock] || !dominatesIDOM(idom, order, defBlock, use) {
			return fmt.Errorf("%s value %d from b%d does not dominate b%d", what, v, defBlock, use)
		}
		return nil
	}

	for bi := range f.Blocks {
		b := &f.Blocks[bi]
		instEnd := b.Insts.End()
		for ii := b.Insts.Start; ii < instEnd; ii++ {
			in := &f.Insts[ii]
			argEnd := in.Args.End()
			for ai := in.Args.Start; ai < argEnd; ai++ {
				if err := checkUse(f.ValueIDs[ai], BlockID(bi), InstID(ii), fmt.Sprintf("inst %d arg", ii)); err != nil {
					return err
				}
			}
		}
		switch b.Term.Kind {
		case TermCondBr:
			if err := checkUse(b.Term.Cond, BlockID(bi), InvalidInst, fmt.Sprintf("block %d condbr condition", bi)); err != nil {
				return err
			}
		case TermSwitch:
			if err := checkUse(b.Term.Index, BlockID(bi), InvalidInst, fmt.Sprintf("block %d switch index", bi)); err != nil {
				return err
			}
		case TermReturn:
			argEnd := b.Term.Args.End()
			for ai := b.Term.Args.Start; ai < argEnd; ai++ {
				if err := checkUse(f.ValueIDs[ai], BlockID(bi), InvalidInst, fmt.Sprintf("block %d return", bi)); err != nil {
					return err
				}
			}
		}
		if edges, ok := branchEdges(&b.Term); ok {
			edgesEnd := edges.End()
			for ei := edges.Start; ei < edgesEnd; ei++ {
				e := f.Edges[ei]
				argEnd := e.Args.End()
				for ai := e.Args.Start; ai < argEnd; ai++ {
					if err := checkUse(f.ValueIDs[ai], BlockID(bi), InvalidInst, fmt.Sprintf("block %d edge %d", bi, ei)); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func branchEdges(t *Term) (Range, bool) {
	switch t.Kind {
	case TermBr, TermCondBr, TermSwitch:
		return t.Edges, true
	default:
		return Range{}, false
	}
}

func reversePostorder(entry BlockID, succs [][]BlockID) ([]bool, []BlockID) {
	reachable := make([]bool, len(succs))
	if int(entry) >= len(succs) {
		return reachable, nil
	}
	type frame struct {
		b    BlockID
		next int
	}
	var post []BlockID
	stack := []frame{{b: entry}}
	reachable[entry] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next < len(succs[top.b]) {
			s := succs[top.b][top.next]
			top.next++
			if !reachable[s] {
				reachable[s] = true
				stack = append(stack, frame{b: s})
			}
			continue
		}
		post = append(post, top.b)
		stack = stack[:len(stack)-1]
	}
	for i, j := 0, len(post)-1; i < j; i, j = i+1, j-1 {
		post[i], post[j] = post[j], post[i]
	}
	return reachable, post
}

func computeIDoms(entry BlockID, preds [][]BlockID, reachable []bool, rpo []BlockID) ([]BlockID, []int32) {
	idom := make([]BlockID, len(preds))
	order := make([]int32, len(preds))
	for i := range idom {
		idom[i] = InvalidBlock
		order[i] = -1
	}
	for i, b := range rpo {
		order[b] = int32(i)
	}
	if int(entry) >= len(idom) {
		return idom, order
	}
	idom[entry] = entry
	for changed := true; changed; {
		changed = false
		for _, b := range rpo {
			if b == entry {
				continue
			}
			newIDom := InvalidBlock
			for _, p := range preds[b] {
				if reachable[p] && idom[p] != InvalidBlock {
					newIDom = p
					break
				}
			}
			if newIDom == InvalidBlock {
				continue
			}
			for _, p := range preds[b] {
				if p == newIDom || !reachable[p] || idom[p] == InvalidBlock {
					continue
				}
				newIDom = intersectIDOM(p, newIDom, idom, order)
			}
			if idom[b] != newIDom {
				idom[b] = newIDom
				changed = true
			}
		}
	}
	return idom, order
}

func intersectIDOM(a, b BlockID, idom []BlockID, order []int32) BlockID {
	for a != b {
		for order[a] > order[b] {
			a = idom[a]
		}
		for order[b] > order[a] {
			b = idom[b]
		}
	}
	return a
}

func dominatesIDOM(idom []BlockID, order []int32, a, b BlockID) bool {
	if int(a) >= len(idom) || int(b) >= len(idom) || order[a] < 0 || order[b] < 0 {
		return false
	}
	for {
		if a == b {
			return true
		}
		if idom[b] == InvalidBlock || idom[b] == b {
			return false
		}
		b = idom[b]
	}
}

func verifyValueRange(f *Func, r Range, what string) (uint32, error) {
	end, err := verifyRange(r, len(f.ValueIDs), what)
	if err != nil {
		return 0, err
	}
	for _, v := range f.ValueIDs[r.Start:end] {
		if err := verifyValue(f, v, what); err != nil {
			return 0, err
		}
	}
	return end, nil
}

func verifyRange(r Range, total int, what string) (uint32, error) {
	// Range is intentionally compact, but Start+Len can wrap uint32. Check in
	// uint64 before using the end as a slice bound or loop limit so malformed IR
	// fails verification instead of panicking or silently skipping entries.
	if uint64(r.Start) > uint64(total) || uint64(r.Len) > uint64(total)-uint64(r.Start) {
		return 0, fmt.Errorf("%s range out of bounds", what)
	}
	return r.Start + r.Len, nil
}
func verifyValue(f *Func, v ValueID, what string) error {
	if v == InvalidValue || int(v) >= len(f.Values) {
		return fmt.Errorf("%s invalid value %d", what, v)
	}
	return nil
}
