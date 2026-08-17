//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcConversionNonZeroFactModuleARM64(t testing.TB, externToAny, nullable bool) *wasm.Module {
	t.Helper()
	heapByte := byte(0x6e) // any
	sub := byte(0x1b)      // extern.convert_any
	if externToAny {
		heapByte = 0x6f // extern
		sub = 0x1a      // any.convert_extern
	}
	refCode := byte(0x64) // ref
	if nullable {
		refCode = 0x63 // ref null
	}
	funcType := []byte{0x60, 0x01, refCode, heapByte, 0x01, 0x7f}
	body := []byte{0x00} // no locals
	for range 8 {
		body = append(body,
			0x20, 0x00, // local.get reference
			0xfb, sub, // reference conversion
			0xd4, // ref.as_non_null
			0xd1, // ref.is_null
		)
	}
	for range 7 {
		body = append(body, 0x6a) // i32.add
	}
	body = append(body, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
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

func gcConversionNonZeroFactStatsARM64(t testing.TB, externToAny, nullable, enabled bool) (*CodegenStats, int) {
	t.Helper()
	var stats ModuleStats
	compiled, err := CompileModuleWith(gcConversionNonZeroFactModuleARM64(t, externToAny, nullable), CompileOptions{
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

func TestGCConversionNonZeroFactsARM64(t *testing.T) {
	for _, externToAny := range []bool{false, true} {
		name := "extern_convert_any"
		if externToAny {
			name = "any_convert_extern"
		}
		t.Run(name, func(t *testing.T) {
			on, onBytes := gcConversionNonZeroFactStatsARM64(t, externToAny, false, true)
			off, offBytes := gcConversionNonZeroFactStatsARM64(t, externToAny, false, false)
			if got := on.Peephole["gc-null-check-elide"]; got != 8 {
				t.Fatalf("conversion null-check elisions = %d, want 8", got)
			}
			if got := on.Peephole["ref-is-null-fold"]; got != 8 {
				t.Fatalf("conversion null folds = %d, want 8", got)
			}
			if got := off.Peephole["gc-null-check-elide"] + off.Peephole["ref-is-null-fold"]; got != 0 {
				t.Fatalf("disabled conversion fact hits = %d, want 0", got)
			}
			if onBytes >= offBytes {
				t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
			}

			nullable, _ := gcConversionNonZeroFactStatsARM64(t, externToAny, true, true)
			if got := nullable.Peephole["gc-null-check-elide"]; got != 0 {
				t.Fatalf("nullable conversion null-check elisions = %d, want 0", got)
			}
		})
	}
}

func BenchmarkGCConversionNonZeroFactsCompileARM64(b *testing.B) {
	m := gcConversionNonZeroFactModuleARM64(b, true, false)
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
