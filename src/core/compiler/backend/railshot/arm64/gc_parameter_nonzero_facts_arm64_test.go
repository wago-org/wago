//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func parameterNonZeroFactModuleARM64(t testing.TB, nullable bool) *wasm.Module {
	t.Helper()
	body := []byte{}
	for range 8 {
		body = append(body,
			0x20, 0x00, 0xd1, 0x1a, // ref.is_null; drop
			0x20, 0x00, 0xd4, 0x1a, // ref.as_non_null; drop
		)
	}
	body = append(body, 0x41, 0x00, 0x0b)
	refCode := byte(0x64)
	if nullable {
		refCode = 0x63
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			[]byte{0x60, 0x01, refCode, 0x00, 0x01, 0x7f},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	ft, ok := m.ResolvedLocalFuncType(0)
	if !ok || len(ft.Params) != 1 || ft.Params[0].Kind() != wasm.ValRef || ft.Params[0].Ref().Nullable() != nullable {
		t.Fatalf("decoded parameter type = %#v, want nullable=%v reference", ft, nullable)
	}
	hints, err := scanBodyBytes(m.Code[0].BodyBytes, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if hints.hasControlFlow {
		t.Fatal("parameter fact fixture unexpectedly has control flow")
	}
	return m
}

func TestParameterNonZeroFactsARM64(t *testing.T) {
	compile := func(m *wasm.Module, enabled bool) (*CodegenStats, int) {
		var stats ModuleStats
		compiled, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			Stats:           &stats,
			Optimizations:   map[string]bool{"value-facts": enabled},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.CodeImage.Close()
		return stats.Funcs[0], len(compiled.Code)
	}
	nonNull := parameterNonZeroFactModuleARM64(t, false)
	on, onBytes := compile(nonNull, true)
	off, offBytes := compile(nonNull, false)
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
	nullable, _ := compile(parameterNonZeroFactModuleARM64(t, true), true)
	if got := nullable.Peephole["ref-is-null-fold"] + nullable.Peephole["gc-null-check-elide"]; got != 0 {
		t.Fatalf("nullable parameter fact-driven rewrites = %d, want 0", got)
	}
}

func BenchmarkParameterNonZeroFactsCompileARM64(b *testing.B) {
	m := parameterNonZeroFactModuleARM64(b, false)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{GCStructHelpers: true, Optimizations: map[string]bool{"value-facts": enabled}}
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
