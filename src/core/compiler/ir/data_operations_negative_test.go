package ir

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestVerifyRejectsMalformedDataOperations(t *testing.T) {
	const initEffects = EffectCanTrap | EffectReadData | EffectWriteMem
	m := &Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}, Data: []DataMeta{{Passive: true, Len: 1}}}
	for _, tc := range []struct {
		name      string
		op        Op
		change    func(*Func)
		errorText string
	}{
		{"init/memory_index", OpMemoryInit, func(f *Func) { f.Insts[0].Aux = 1 }, "memory index"},
		{"init/data_index", OpMemoryInit, func(f *Func) { f.Insts[0].Aux = 1 << 32 }, "unknown data segment"},
		{"init/destination_type", OpMemoryInit, func(f *Func) { f.Values[0].Type = wasm.I64 }, "memory.init type mismatch"},
		{"init/source_type", OpMemoryInit, func(f *Func) { f.Values[1].Type = wasm.I64 }, "memory.init type mismatch"},
		{"init/length_type", OpMemoryInit, func(f *Func) { f.Values[2].Type = wasm.I64 }, "memory.init type mismatch"},
		{"init/missing_trap", OpMemoryInit, func(f *Func) { f.Insts[0].Effects &^= EffectCanTrap }, "effects"},
		{"init/missing_read_data", OpMemoryInit, func(f *Func) { f.Insts[0].Effects &^= EffectReadData }, "effects"},
		{"init/missing_write_memory", OpMemoryInit, func(f *Func) { f.Insts[0].Effects &^= EffectWriteMem }, "effects"},
		{"init/extra_effect", OpMemoryInit, func(f *Func) { f.Insts[0].Effects |= EffectWriteData }, "effects"},
		{"drop/data_index", OpDataDrop, func(f *Func) { f.Insts[0].Aux = 1 }, "unknown data segment"},
		{"drop/missing_write_data", OpDataDrop, func(f *Func) { f.Insts[0].Effects = EffectNone }, "effects"},
		{"drop/extra_effect", OpDataDrop, func(f *Func) { f.Insts[0].Effects |= EffectCanTrap }, "effects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, effects := []wasm.ValType(nil), EffectWriteData
			if tc.op == OpMemoryInit {
				args, effects = []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, initEffects
			}
			f := instFunc(tc.op, args, nil, effects)
			if err := VerifyFuncInModule(f, m); err != nil {
				t.Fatalf("valid control: %v", err)
			}
			tc.change(f)
			wantErr(t, VerifyFuncInModule(f, m), tc.errorText)
		})
	}
}

func TestVerifyUnknownPoisonBoundaries(t *testing.T) {
	t.Run("unreachable_stack", func(t *testing.T) {
		f := &Func{Entry: 0, Values: []Value{{DefKind: ValueDefPoison}}, Blocks: []Block{{Term: Term{Kind: TermTrap}}}}
		if err := VerifyFunc(f); err != nil {
			t.Fatal(err)
		}
		f.Values[0].DefKind = ValueDefInst
		wantErr(t, VerifyFunc(f), "invalid type")
	})
	t.Run("reachable_instruction", func(t *testing.T) {
		f := instFunc(OpITest, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, EffectNone)
		if err := VerifyFunc(f); err != nil {
			t.Fatalf("valid control: %v", err)
		}
		f.Values = append(f.Values, Value{DefKind: ValueDefPoison})
		f.Insts[0].Args = Range{Start: uint32(len(f.ValueIDs)), Len: 1}
		f.ValueIDs = append(f.ValueIDs, ValueID(len(f.Values)-1))
		wantErr(t, VerifyFunc(f), "uses poison")
	})
	t.Run("branch_argument", func(t *testing.T) {
		f := validConstReturnI32Func()
		f.Values = append(f.Values, Value{Type: wasm.I32, DefKind: ValueDefBlockParam, Def: 1})
		f.ValueIDs = append(f.ValueIDs, 1)
		f.Blocks[0].Term = Term{Kind: TermBr, Edges: Range{Len: 1}}
		f.Edges = []Edge{{To: 1, Args: Range{Start: 1, Len: 1}}}
		f.Blocks = append(f.Blocks, Block{Params: Range{Start: 2, Len: 1}, Term: Term{Kind: TermReturn, Args: Range{Start: 2, Len: 1}}})
		if err := VerifyFunc(f); err != nil {
			t.Fatalf("valid control: %v", err)
		}
		f.Values = append(f.Values, Value{DefKind: ValueDefPoison})
		f.ValueIDs[1] = 2
		wantErr(t, VerifyFunc(f), "edge 0 uses poison")
	})
	t.Run("function_return", func(t *testing.T) {
		f := validConstReturnI32Func()
		if err := VerifyFunc(f); err != nil {
			t.Fatalf("valid control: %v", err)
		}
		f.Values = append(f.Values, Value{DefKind: ValueDefPoison})
		f.ValueIDs[1] = 1
		wantErr(t, VerifyFunc(f), "return arg 0 type")
		f.Values[1].Type = wasm.I32
		wantErr(t, VerifyFunc(f), "returns poison")
	})
}
