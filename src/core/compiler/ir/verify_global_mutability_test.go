package ir

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestVerifyGlobalSetRequiresMutableGlobal(t *testing.T) {
	f := instFunc(OpGlobalSet, []wasm.ValType{wasm.I32}, nil, EffectWriteGlobal)
	for _, idx := range []uint64{0, 1} {
		f.Insts[0].Aux = idx
		m := &Module{Globals: []wasm.GlobalType{
			{Type: wasm.I32, Mutable: true},
			{Type: wasm.I32, Mutable: true},
		}}
		if err := VerifyFuncInModule(f, m); err != nil {
			t.Fatalf("mutable global %d: %v", idx, err)
		}
		m.Globals[idx].Mutable = false
		if err := VerifyFuncInModule(f, m); err == nil {
			t.Fatalf("immutable global %d was accepted", idx)
		}
	}
}

func BenchmarkVerifyMutableGlobalSet(b *testing.B) {
	f := instFunc(OpGlobalSet, []wasm.ValType{wasm.I32}, nil, EffectWriteGlobal)
	m := &Module{Globals: []wasm.GlobalType{{Type: wasm.I32, Mutable: true}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := VerifyFuncInModule(f, m); err != nil {
			b.Fatal(err)
		}
	}
}
