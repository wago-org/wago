//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func i31NonZeroFactModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	body := []byte{0x01, 0x01, 0x6c}
	for range 8 {
		body = append(body,
			0x41, 0x2a, 0xfb, 0x1c,
			0x21, 0x00,
			0x20, 0x00, 0xd1, 0x1a,
			0x20, 0x00, 0xd4, 0xfb, 0x1e,
		)
	}
	for range 7 {
		body = append(body, 0x6a)
	}
	body = append(body, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
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

func TestI31NonZeroFactsARM64(t *testing.T) {
	compile := func(enabled bool) (*CodegenStats, int) {
		var stats ModuleStats
		compiled, err := CompileModuleWith(i31NonZeroFactModuleARM64(t), CompileOptions{
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
	on, onBytes := compile(true)
	off, offBytes := compile(false)
	if got := on.Peephole["gc-null-check-elide"]; got != 16 {
		t.Fatalf("i31 null-check elisions = %d, want 16", got)
	}
	if got := off.Peephole["gc-null-check-elide"]; got != 0 {
		t.Fatalf("disabled i31 null-check elisions = %d, want 0", got)
	}
	if got := on.Peephole["ref-is-null-fold"]; got != 8 {
		t.Fatalf("ref.is_null folds = %d, want 8", got)
	}
	if got := off.Peephole["ref-is-null-fold"]; got != 0 {
		t.Fatalf("disabled ref.is_null folds = %d, want 0", got)
	}
	if onBytes >= offBytes {
		t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
	}
}

func BenchmarkI31NonZeroFactsCompileARM64(b *testing.B) {
	m := i31NonZeroFactModuleARM64(b)
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
