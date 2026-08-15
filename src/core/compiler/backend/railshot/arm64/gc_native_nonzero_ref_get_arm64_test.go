//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeNonZeroRefGetModuleARM64(t testing.TB) (*wasm.Module, []codegen.GCTypeLayout) {
	t.Helper()
	structBody := []byte{}
	arrayBody := []byte{}
	for range 8 {
		structBody = append(structBody,
			0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xd1, 0x1a,
			0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xd4, 0x1a,
		)
		arrayBody = append(arrayBody,
			0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x01, 0xd1, 0x1a,
			0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x01, 0xd4, 0x1a,
		)
	}
	structBody = append(structBody, 0x41, 0x00, 0x0b)
	arrayBody = append(arrayBody, 0x41, 0x00, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x64, 0x00, 0x00},       // struct { (ref 0) }
			[]byte{0x5e, 0x64, 0x00, 0x00},             // array (ref 0)
			[]byte{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f}, // (ref null 0) -> i32
			[]byte{0x60, 0x01, 0x63, 0x01, 0x01, 0x7f}, // (ref null 1) -> i32
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(structBody), wasmtest.Code(arrayBody))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	return m, metadata.Layouts
}

func TestNativeNonZeroRefGetFactsARM64(t *testing.T) {
	m, layouts := nativeNonZeroRefGetModuleARM64(t)
	compile := func(enabled bool) ([]*CodegenStats, int) {
		var stats ModuleStats
		compiled, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			GCArrayHelpers:  true,
			Stats:           &stats,
			Optimizations: map[string]bool{
				"gc-native-final-ref-get": true,
				"value-facts":             enabled,
			},
			Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: layouts}},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.CodeImage.Close()
		return stats.Funcs, len(compiled.Code)
	}
	on, onBytes := compile(true)
	off, offBytes := compile(false)
	for i, name := range []string{"struct", "array"} {
		if got := on[i].Peephole["ref-is-null-fold"]; got != 8 {
			t.Fatalf("%s ref.is_null folds = %d, want 8", name, got)
		}
		if got := on[i].Peephole["gc-null-check-elide"]; got != 8 {
			t.Fatalf("%s null-check elisions = %d, want 8", name, got)
		}
		if got := off[i].Peephole["ref-is-null-fold"] + off[i].Peephole["gc-null-check-elide"]; got != 0 {
			t.Fatalf("disabled %s fact-driven rewrites = %d, want 0", name, got)
		}
	}
	if onBytes >= offBytes {
		t.Fatalf("native bytes enabled/disabled = %d/%d, want reduction", onBytes, offBytes)
	}
}

func BenchmarkNativeNonZeroRefGetFactsCompileARM64(b *testing.B) {
	m, layouts := nativeNonZeroRefGetModuleARM64(b)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{
				GCStructHelpers: true,
				GCArrayHelpers:  true,
				Optimizations: map[string]bool{
					"gc-native-final-ref-get": true,
					"value-facts":             enabled,
				},
				Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: layouts}},
			}
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
