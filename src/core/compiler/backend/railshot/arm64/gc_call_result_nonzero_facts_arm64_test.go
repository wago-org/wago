//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func callResultNonZeroFactModuleARM64(t testing.TB, resultNullable, throughLocal bool) *wasm.Module {
	t.Helper()
	callee := []byte{0x20, 0x00, 0x0b}
	caller := []byte{}
	if throughLocal {
		caller = append(caller, 0x01, 0x01, 0x63, 0x00) // one (ref null 0) local
	}
	for range 8 {
		caller = append(caller, 0x20, 0x00, 0x10, 0x00)
		if throughLocal {
			caller = append(caller, 0x21, 0x01, 0x20, 0x01)
		}
		caller = append(caller, 0xd1, 0x1a) // ref.is_null; drop
		caller = append(caller, 0x20, 0x00, 0x10, 0x00)
		if throughLocal {
			caller = append(caller, 0x21, 0x01, 0x20, 0x01)
		}
		caller = append(caller, 0xd4, 0x1a) // ref.as_non_null; drop
	}
	caller = append(caller, 0x41, 0x00, 0x0b)
	resultRefCode := byte(0x64)
	if resultNullable {
		resultRefCode = 0x63
	}
	callerCode := wasmtest.Code(caller)
	if throughLocal {
		callerCode = append(wasmtest.ULEB(uint32(len(caller))), caller...)
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x60, 0x01, 0x64, 0x00, 0x01, resultRefCode, 0x00},
			[]byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f}, // (ref 0) -> i32
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee), callerCode)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCallResultNonZeroFactsARM64(t *testing.T) {
	compile := func(m *wasm.Module, enabled bool) (*CodegenStats, int) {
		var stats ModuleStats
		compiled, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			Stats:           &stats,
			Optimizations: map[string]bool{
				"inline":      false,
				"value-facts": enabled,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.CodeImage.Close()
		return stats.Funcs[1], len(compiled.Code)
	}
	direct := callResultNonZeroFactModuleARM64(t, false, false)
	on, onBytes := compile(direct, true)
	off, offBytes := compile(direct, false)
	if got := on.Peephole["ref-is-null-fold"]; got != 8 {
		t.Fatalf("ref.is_null folds = %d, want 8 (all: %v)", got, on.Peephole)
	}
	if got := on.Peephole["gc-null-check-elide"]; got != 8 {
		t.Fatalf("null-check elisions = %d, want 8", got)
	}
	if got := off.Peephole["ref-is-null-fold"] + off.Peephole["gc-null-check-elide"]; got != 0 {
		t.Fatalf("disabled fact-driven rewrites = %d, want 0", got)
	}
	if onBytes >= offBytes {
		t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
	}
	fused, _ := compile(callResultNonZeroFactModuleARM64(t, false, true), true)
	if got := fused.Peephole["ref-is-null-fold"] + fused.Peephole["gc-null-check-elide"]; got != 16 {
		t.Fatalf("local-sunk result fact rewrites = %d, want 16", got)
	}
	nullable, _ := compile(callResultNonZeroFactModuleARM64(t, true, false), true)
	if got := nullable.Peephole["ref-is-null-fold"] + nullable.Peephole["gc-null-check-elide"]; got != 0 {
		t.Fatalf("nullable call result fact-driven rewrites = %d, want 0", got)
	}
}

func BenchmarkCallResultNonZeroFactsCompileARM64(b *testing.B) {
	m := callResultNonZeroFactModuleARM64(b, false, false)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{GCStructHelpers: true, Optimizations: map[string]bool{"inline": false, "value-facts": enabled}}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				b.Fatal(err)
			}
			codeBytes := len(compiled.Code)
			compiled.CodeImage.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				cm.CodeImage.Close()
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
	}
}
