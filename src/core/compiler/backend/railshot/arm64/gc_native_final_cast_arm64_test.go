//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeFinalCastModuleARM64(t testing.TB, final, array bool) *wasm.Module {
	t.Helper()
	typeDef := []byte{0x5f, 0x01, 0x7f, 0x01}
	initBody := []byte{0xfb, 0x01, 0x00, 0x24, 0x00, 0x0b}
	if array {
		typeDef = []byte{0x5e, 0x7f, 0x01}
		initBody = []byte{0x41, 0x01, 0xfb, 0x07, 0x00, 0x24, 0x00, 0x0b}
	}
	if !final {
		typeDef = append([]byte{0x50, 0x00}, typeDef...)
	}
	runBody := []byte{0x01, 0x01, 0x6e, 0x23, 0x00, 0x21, 0x00}
	for range 8 {
		runBody = append(runBody, 0x20, 0x00, 0xfb, 0x16, 0x00, 0x1a)
	}
	runBody = append(runBody, 0x41, 0x2a, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			typeDef,
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.RefVal(wasm.AbsRef(wasm.HeapAny)), true, []byte{0xd0, 0x6e, 0x0b}))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(initBody),
			append(wasmtest.ULEB(uint32(len(runBody))), runBody...),
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

func compileNativeFinalCastARM64(t testing.TB, m *wasm.Module, enabled bool, objective *OptimizationObjective) (*CodegenStats, int) {
	t.Helper()
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-cast": enabled},
		Objective:       objective,
		Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	codeBytes := len(compiled.Code)
	if err := compiled.CodeImage.Close(); err != nil {
		t.Fatal(err)
	}
	return stats.Funcs[1], codeBytes
}

func TestNativeFinalCastARM64(t *testing.T) {
	for _, tc := range []struct {
		name  string
		array bool
	}{{name: "struct"}, {name: "array", array: true}} {
		t.Run(tc.name, func(t *testing.T) {
			m := nativeFinalCastModuleARM64(t, true, tc.array)
			on, onBytes := compileNativeFinalCastARM64(t, m, true, nil)
			off, offBytes := compileNativeFinalCastARM64(t, m, false, nil)
			if got := on.Peephole["gc-native-final-cast"]; got != 8 {
				t.Fatalf("native final casts = %d, want 8", got)
			}
			if got := on.Peephole["gc-native-resolve-reuse"]; got != 7 {
				t.Fatalf("native final cast resolver reuses = %d, want 7", got)
			}
			if got := on.Peephole["gc-native-final-cast-elide"]; got != 7 {
				t.Fatalf("native final cast elisions = %d, want 7", got)
			}
			if got := off.Peephole["gc-native-final-cast"]; got != 0 {
				t.Fatalf("disabled native final casts = %d, want 0", got)
			}
			if growth := onBytes - offBytes; growth > 512 {
				t.Fatalf("native final cast code growth = %d bytes, want <=512 (%d versus %d)", growth, onBytes, offBytes)
			}
			size := OptimizeSize
			compact, _ := compileNativeFinalCastARM64(t, m, true, &size)
			if got := compact.Peephole["gc-native-final-cast"]; got != 0 {
				t.Fatalf("Size native final casts = %d, want 0", got)
			}
		})
	}
}

func TestNativeFinalCastRejectsOpenTypeARM64(t *testing.T) {
	stats, _ := compileNativeFinalCastARM64(t, nativeFinalCastModuleARM64(t, false, false), true, nil)
	if got := stats.Peephole["gc-native-final-cast"]; got != 0 {
		t.Fatalf("open-type native final casts = %d, want 0", got)
	}
	if got := stats.Calls[callKindHostSync]; got != 8 {
		t.Fatalf("open-type helper calls = %d, want 8", got)
	}
}

func BenchmarkNativeFinalCastCompileARM64(b *testing.B) {
	m := nativeFinalCastModuleARM64(b, true, false)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		b.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{
				GCStructHelpers: true,
				GCArrayHelpers:  true,
				Optimizations:   map[string]bool{"gc-native-final-cast": enabled},
				Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
			}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				b.Fatal(err)
			}
			codeBytes := len(compiled.Code)
			if err := compiled.CodeImage.Close(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				if err := cm.CodeImage.Close(); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
	}
}
