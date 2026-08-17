//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcCastNonZeroFactModuleARM64(t testing.TB, nullable bool) *wasm.Module {
	t.Helper()
	body := []byte{0x00} // no locals
	sub := byte(0x16)    // ref.cast
	if nullable {
		sub = 0x17 // ref.cast_null
	}
	for range 8 {
		body = append(body,
			0x20, 0x00, // local.get nullable anyref
			0xfb, sub, 0x00, // ref.cast[_null] type 0
			0xd4, // ref.as_non_null
			0xd1, // ref.is_null
		)
	}
	for range 7 {
		body = append(body, 0x6a) // i32.add
	}
	body = append(body, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00}, // empty struct type
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
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

func gcCastNonZeroFactStatsARM64(t testing.TB, nullable, enabled bool) (*CodegenStats, int) {
	t.Helper()
	var stats ModuleStats
	compiled, err := CompileModuleWith(gcCastNonZeroFactModuleARM64(t, nullable), CompileOptions{
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

func TestGCCastNonZeroFactsARM64(t *testing.T) {
	on, onBytes := gcCastNonZeroFactStatsARM64(t, false, true)
	off, offBytes := gcCastNonZeroFactStatsARM64(t, false, false)
	if got := on.Peephole["gc-null-check-elide"]; got != 8 {
		t.Fatalf("ref.cast null-check elisions = %d, want 8", got)
	}
	if got := on.Peephole["ref-is-null-fold"]; got != 8 {
		t.Fatalf("ref.cast null folds = %d, want 8", got)
	}
	if got := off.Peephole["gc-null-check-elide"] + off.Peephole["ref-is-null-fold"]; got != 0 {
		t.Fatalf("disabled ref.cast fact hits = %d, want 0", got)
	}
	if onBytes >= offBytes {
		t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
	}

	nullable, _ := gcCastNonZeroFactStatsARM64(t, true, true)
	if got := nullable.Peephole["gc-null-check-elide"]; got != 0 {
		t.Fatalf("ref.cast_null null-check elisions = %d, want 0", got)
	}
}

func BenchmarkGCCastNonZeroFactsCompileARM64(b *testing.B) {
	m := gcCastNonZeroFactModuleARM64(b, false)
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
