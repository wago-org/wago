//go:build arm64

package arm64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

var branchHintCompileCases = []struct{ functions, hinted int }{
	{0, 0}, {1, 0}, {1, 1}, {8, 0}, {8, 1}, {8, 8}, {512, 512}, {4096, 4096},
}

func branchHintCompileModule(tb testing.TB, functions, hinted int) *wasm.Module {
	tb.Helper()
	funcs, bodies := make([][]byte, functions), make([][]byte, functions)
	for i := range funcs {
		funcs[i] = wasmtest.ULEB(0)
		// (param i32) (result i32): local.get 0; if (result i32)
		// i32.const 1; else; i32.const 0; end; end.
		bodies[i] = wasmtest.Code([]byte{0x20, 0x00, 0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x00, 0x0b, 0x0b})
	}
	payload := wasmtest.ULEB(uint32(hinted))
	for i := 0; i < hinted; i++ {
		// Hint the last functions, so partial coverage includes absent lookups.
		payload = append(payload, wasmtest.ULEB(uint32(functions-hinted+i))...)
		// One hint: if at offset 3 (including the local declaration byte),
		// followed by its one-byte direction payload.
		payload = append(payload, 1, 3, 1, byte(i%2))
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
	}
	if hinted != 0 {
		sections = append(sections, wasmtest.Custom("metadata.code.branch_hint", payload))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(bodies...)))
	m, err := wasm.DecodeModule(wasmtest.Module(sections...))
	if err != nil {
		tb.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	if len(m.BranchHints) != hinted {
		tb.Fatalf("decoded %d hinted functions, want %d", len(m.BranchHints), hinted)
	}
	return m
}

// This benchmark exercises the ARM64 hint scan and code-generation consumers.
// Decode and validation are outside timing; generated code is never executed.
func BenchmarkCompileBranchHintLookupARM64(b *testing.B) {
	for _, tc := range branchHintCompileCases {
		b.Run(fmt.Sprintf("functions=%d/hints=%d", tc.functions, tc.hinted), func(b *testing.B) {
			m := branchHintCompileModule(b, tc.functions, tc.hinted)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					if err := cm.CodeImage.Close(); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func TestCompileBranchHintLookupARM64Fixtures(t *testing.T) {
	for _, tc := range branchHintCompileCases {
		t.Run(fmt.Sprintf("functions=%d/hints=%d", tc.functions, tc.hinted), func(t *testing.T) {
			m := branchHintCompileModule(t, tc.functions, tc.hinted)
			for i := range m.Code {
				hints := m.BranchHintsForFunc(uint32(i))
				if i < tc.functions-tc.hinted {
					if len(hints) != 0 {
						t.Fatalf("unexpected hint for function %d", i)
					}
				} else if len(hints) != 1 || hints[0].Offset != 3 || hints[0].Likely != ((i-(tc.functions-tc.hinted))%2 != 0) {
					t.Fatalf("incorrect hint for function %d: %+v", i, hints)
				}
			}
			cm, err := CompileModuleWith(m, CompileOptions{Workers: 1})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				if err := cm.CodeImage.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
