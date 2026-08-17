//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeFinalArrayRefGetModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	childType := []byte{0x5f, 0x01, 0x7f, 0x01}
	arrayType := []byte{0x5e, 0x6e, 0x01}
	body := []byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x63, 0x01,
		0xfb, 0x01, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x08, 0xfb, 0x06, 0x01, 0x21, 0x01,
	}
	for i := 0; i < 7; i++ {
		body = append(body, 0x20, 0x01, 0x41, byte(i), 0xfb, 0x0b, 0x01, 0x1a)
	}
	body = append(body,
		0x20, 0x01, 0x41, 0x07, 0xfb, 0x0b, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0xfb, 0x16, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			childType,
			arrayType,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
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

func compileNativeFinalArrayRefGetARM64(t testing.TB, enabled bool, objective *OptimizationObjective) (*CodegenStats, int) {
	t.Helper()
	m := nativeFinalArrayRefGetModuleARM64(t)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-ref-get": enabled},
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
	return stats.Funcs[0], codeBytes
}

func TestNativeFinalArrayRefGetARM64(t *testing.T) {
	on, onBytes := compileNativeFinalArrayRefGetARM64(t, true, nil)
	off, offBytes := compileNativeFinalArrayRefGetARM64(t, false, nil)
	if got := on.Peephole["gc-native-final-array-ref-get"]; got != 8 {
		t.Fatalf("native array reference gets = %d, want 8", got)
	}
	if got := on.Peephole["gc-native-resolve-reuse"]; got != 7 {
		t.Fatalf("native array resolution reuse = %d, want 7", got)
	}
	if off.Peephole["gc-native-final-array-ref-get"] != 0 {
		t.Fatalf("disabled native array reference gets = %d, want 0", off.Peephole["gc-native-final-array-ref-get"])
	}
	if growth := onBytes - offBytes; growth > 256 {
		t.Fatalf("native array reference get code growth = %d bytes, want <=256 (%d versus %d)", growth, onBytes, offBytes)
	}
	size := OptimizeSize
	compact, _ := compileNativeFinalArrayRefGetARM64(t, true, &size)
	if got := compact.Peephole["gc-native-final-array-ref-get"]; got != 0 {
		t.Fatalf("Size native array reference gets = %d, want 0", got)
	}
}

func TestNativeFinalArrayRefGetRejectsFunctionReferenceARM64(t *testing.T) {
	arrayType := []byte{0x5e, 0x70, 0x01}
	body := []byte{
		0xd0, byte(wasm.HeapFunc), 0x41, 0x01, 0xfb, 0x06, 0x00,
		0x41, 0x00, 0xfb, 0x0b, 0x00, 0xd1, 0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
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
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-ref-get": true},
		Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["gc-native-final-array-ref-get"]; got != 0 {
		t.Fatalf("function-reference native array gets = %d, want 0", got)
	}
	if got := stats.Funcs[0].Calls[callKindHostSync]; got != 2 {
		t.Fatalf("function-reference array helper calls = %d, want 2", got)
	}
}

func BenchmarkNativeFinalArrayRefGetCompileARM64(b *testing.B) {
	m := nativeFinalArrayRefGetModuleARM64(b)
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
			opts := CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Optimizations: map[string]bool{"gc-native-final-ref-get": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}}
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
