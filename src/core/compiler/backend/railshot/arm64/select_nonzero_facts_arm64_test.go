//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func selectNonZeroFactModuleARM64(t testing.TB, bothNonNull bool) *wasm.Module {
	t.Helper()
	body := []byte{}
	for range 8 {
		if bothNonNull {
			body = append(body, 0xd2, 0x00)
		} else {
			body = append(body, 0xd0, byte(wasm.HeapFunc))
		}
		body = append(body,
			0xd2, 0x00,
			0x20, 0x00,
			0x1c, 0x01, 0x70,
			0xd1, 0x1a,
		)
		if bothNonNull {
			body = append(body, 0xd2, 0x00)
		} else {
			body = append(body, 0xd0, byte(wasm.HeapFunc))
		}
		body = append(body,
			0xd2, 0x00,
			0x20, 0x00,
			0x1c, 0x01, 0x70,
			0xd4, 0x1a,
		)
	}
	body = append(body, 0x41, 0x00, 0x0b)
	declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code(body),
		)),
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

func TestSelectNonZeroFactsARM64(t *testing.T) {
	compile := func(m *wasm.Module, enabled bool) (*CodegenStats, int) {
		var stats ModuleStats
		compiled, err := CompileModuleWith(m, CompileOptions{
			Stats:         &stats,
			Optimizations: map[string]bool{"value-facts": enabled},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.CodeImage.Close()
		return stats.Funcs[1], len(compiled.Code)
	}
	positive := selectNonZeroFactModuleARM64(t, true)
	on, onBytes := compile(positive, true)
	off, offBytes := compile(positive, false)
	if got := on.Peephole["ref-is-null-fold"]; got != 8 {
		t.Fatalf("ref.is_null folds = %d, want 8", got)
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
	nearMiss, _ := compile(selectNonZeroFactModuleARM64(t, false), true)
	if got := nearMiss.Peephole["ref-is-null-fold"] + nearMiss.Peephole["gc-null-check-elide"]; got != 0 {
		t.Fatalf("nullable select fact-driven rewrites = %d, want 0", got)
	}
}

func BenchmarkSelectNonZeroFactsCompileARM64(b *testing.B) {
	m := selectNonZeroFactModuleARM64(b, true)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{Optimizations: map[string]bool{"value-facts": enabled}}
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
