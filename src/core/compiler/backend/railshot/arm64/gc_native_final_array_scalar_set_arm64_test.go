//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeFinalArrayScalarSetModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	arrayType := []byte{0x5e, 0x7f, 0x01}
	initBody := []byte{0x00, 0x41, 0x08, 0xfb, 0x07, 0x00, 0x24, 0x00, 0x0b}
	runBody := []byte{0x01, 0x01, 0x63, 0x00, 0x23, 0x00, 0x21, 0x00}
	for i := 0; i < 8; i++ {
		runBody = append(runBody, 0x20, 0x00, 0x41, byte(i), 0x41, byte(10+i), 0xfb, 0x0e, 0x00)
	}
	runBody = append(runBody, 0x20, 0x00, 0x41, 0x07, 0xfb, 0x0b, 0x00, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
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

func compileNativeFinalArrayScalarSetARM64(t testing.TB, enabled bool, objective *OptimizationObjective) (*CodegenStats, int) {
	t.Helper()
	m := nativeFinalArrayScalarSetModuleARM64(t)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-array-scalar-set": enabled},
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

func TestNativeFinalArrayScalarSetARM64(t *testing.T) {
	on, onBytes := compileNativeFinalArrayScalarSetARM64(t, true, nil)
	off, offBytes := compileNativeFinalArrayScalarSetARM64(t, false, nil)
	if got := on.Peephole["gc-native-final-array-scalar-set"]; got != 8 {
		t.Fatalf("native array scalar sets = %d, want 8", got)
	}
	if got := on.Peephole["gc-native-resolve-reuse"]; got != 8 {
		t.Fatalf("native array resolution reuse = %d, want 8", got)
	}
	if off.Peephole["gc-native-final-array-scalar-set"] != 0 {
		t.Fatalf("disabled native array sets = %d, want 0", off.Peephole["gc-native-final-array-scalar-set"])
	}
	if growth := onBytes - offBytes; growth > 256 {
		t.Fatalf("native array set code growth = %d bytes, want <=256 (%d versus %d)", growth, onBytes, offBytes)
	}
	size := OptimizeSize
	compact, _ := compileNativeFinalArrayScalarSetARM64(t, true, &size)
	if got := compact.Peephole["gc-native-final-array-scalar-set"]; got != 0 {
		t.Fatalf("Size native array scalar sets = %d, want 0", got)
	}
}

func BenchmarkNativeFinalArrayScalarSetCompileARM64(b *testing.B) {
	m := nativeFinalArrayScalarSetModuleARM64(b)
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
			opts := CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Optimizations: map[string]bool{"gc-native-final-array-scalar-set": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}}
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
