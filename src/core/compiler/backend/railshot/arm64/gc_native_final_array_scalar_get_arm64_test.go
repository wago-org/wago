//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeFinalArrayScalarGetModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	arrayType := []byte{0x5e, 0x7f, 0x01}
	initBody := []byte{0x00, 0x41, 0x2a, 0x41, 0x08, 0xfb, 0x06, 0x00, 0x24, 0x00, 0x0b}
	runBody := []byte{0x01, 0x01, 0x63, 0x00, 0x23, 0x00, 0x21, 0x00}
	for range 8 {
		runBody = append(runBody, 0x20, 0x00, 0x41, 0x03, 0xfb, 0x0b, 0x00)
	}
	for i := 1; i < 8; i++ {
		runBody = append(runBody, 0x6a)
	}
	runBody = append(runBody, 0x0b)
	getBody := []byte{0x00, 0x23, 0x00, 0x20, 0x00, 0xfb, 0x0b, 0x00, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			arrayType,
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			append(wasmtest.ULEB(uint32(len(runBody))), runBody...),
			append(wasmtest.ULEB(uint32(len(getBody))), getBody...),
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

func compileNativeFinalArrayScalarGetARM64(t testing.TB, enabled bool, objective *OptimizationObjective) (*CodegenStats, int) {
	t.Helper()
	m := nativeFinalArrayScalarGetModuleARM64(t)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-array-scalar-get": enabled},
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

func TestNativeFinalArrayScalarGetARM64(t *testing.T) {
	on, onBytes := compileNativeFinalArrayScalarGetARM64(t, true, nil)
	off, offBytes := compileNativeFinalArrayScalarGetARM64(t, false, nil)
	if got := on.Peephole["gc-native-final-array-scalar-get"]; got != 8 {
		t.Fatalf("native array scalar gets = %d, want 8", got)
	}
	if got := on.Peephole["gc-native-resolve-reuse"]; got != 7 {
		t.Fatalf("native array resolution reuse = %d, want 7", got)
	}
	if off.Peephole["gc-native-final-array-scalar-get"] != 0 || off.Peephole["gc-native-resolve-reuse"] != 0 {
		t.Fatalf("disabled native/reuse hits = %d/%d, want 0/0", off.Peephole["gc-native-final-array-scalar-get"], off.Peephole["gc-native-resolve-reuse"])
	}
	if growth := onBytes - offBytes; growth > 256 {
		t.Fatalf("native array code growth = %d bytes, want <=256 (%d versus %d)", growth, onBytes, offBytes)
	}
	size := OptimizeSize
	compact, _ := compileNativeFinalArrayScalarGetARM64(t, true, &size)
	if got := compact.Peephole["gc-native-final-array-scalar-get"]; got != 0 {
		t.Fatalf("Size native array scalar gets = %d, want 0", got)
	}
}

func BenchmarkNativeFinalArrayScalarGetCompileARM64(b *testing.B) {
	m := nativeFinalArrayScalarGetModuleARM64(b)
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
			opts := CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Optimizations: map[string]bool{"gc-native-final-array-scalar-get": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}}
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
