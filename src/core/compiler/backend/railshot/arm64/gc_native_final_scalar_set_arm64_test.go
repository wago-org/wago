//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeFinalScalarSetModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x7f, 0x01}, []byte{0x7f, 0x01})...)
	initBody := []byte{0x00, 0xfb, 0x01, 0x00, 0x24, 0x00, 0x0b}
	runBody := []byte{0x01, 0x01, 0x63, 0x00, 0x23, 0x00, 0x21, 0x00}
	for i := 0; i < 8; i++ {
		runBody = append(runBody, 0x20, 0x00, 0x41, byte(10+i), 0xfb, 0x05, 0x00, byte(i&1))
	}
	runBody = append(runBody, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x01, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
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

func compileNativeFinalScalarSetARM64(t testing.TB, enabled bool, objective *OptimizationObjective) (*CodegenStats, int) {
	t.Helper()
	m := nativeFinalScalarSetModuleARM64(t)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-scalar-set": enabled},
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

func TestNativeFinalScalarSetARM64(t *testing.T) {
	on, onBytes := compileNativeFinalScalarSetARM64(t, true, nil)
	off, offBytes := compileNativeFinalScalarSetARM64(t, false, nil)
	if got := on.Peephole["gc-native-final-struct-scalar-set"]; got != 8 {
		t.Fatalf("native struct scalar sets = %d, want 8", got)
	}
	if got := on.Peephole["gc-native-resolve-reuse"]; got != 8 {
		t.Fatalf("native struct resolution reuse = %d, want 8", got)
	}
	if off.Peephole["gc-native-final-struct-scalar-set"] != 0 {
		t.Fatalf("disabled native struct sets = %d, want 0", off.Peephole["gc-native-final-struct-scalar-set"])
	}
	if growth := onBytes - offBytes; growth > 256 {
		t.Fatalf("native struct set code growth = %d bytes, want <=256 (%d versus %d)", growth, onBytes, offBytes)
	}
	size := OptimizeSize
	compact, _ := compileNativeFinalScalarSetARM64(t, true, &size)
	if got := compact.Peephole["gc-native-final-struct-scalar-set"]; got != 0 {
		t.Fatalf("Size native struct scalar sets = %d, want 0", got)
	}
}

func BenchmarkNativeFinalScalarSetCompileARM64(b *testing.B) {
	m := nativeFinalScalarSetModuleARM64(b)
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
			opts := CompileOptions{GCStructHelpers: true, Optimizations: map[string]bool{"gc-native-final-scalar-set": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}}
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
