//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeFinalRefGetModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	childType := []byte{0x5f, 0x01, 0x7f, 0x01}
	parentType := []byte{0x5f, 0x01, 0x63, 0x00, 0x01}
	body := []byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x63, 0x01,
		0xfb, 0x01, 0x00, 0x21, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x01, 0x21, 0x01,
	}
	for range 7 {
		body = append(body, 0x20, 0x01, 0xfb, 0x02, 0x01, 0x00, 0x1a)
	}
	body = append(body,
		0x20, 0x01, 0xfb, 0x02, 0x01, 0x00,
		0xfb, 0x01, 0x00, 0x1a,
		0xfb, 0x02, 0x00, 0x00,
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(childType, parentType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
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

func compileNativeFinalRefGetARM64(t testing.TB, enabled bool, objective *OptimizationObjective) (*CodegenStats, int) {
	t.Helper()
	m := nativeFinalRefGetModuleARM64(t)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
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

func TestNativeFinalRefGetARM64(t *testing.T) {
	on, onBytes := compileNativeFinalRefGetARM64(t, true, nil)
	off, offBytes := compileNativeFinalRefGetARM64(t, false, nil)
	if got := on.Peephole["gc-native-final-struct-ref-get"]; got != 8 {
		t.Fatalf("native struct reference gets = %d, want 8", got)
	}
	if got := on.Peephole["gc-native-resolve-reuse"]; got != 7 {
		t.Fatalf("native struct resolution reuse = %d, want 7", got)
	}
	if off.Peephole["gc-native-final-struct-ref-get"] != 0 {
		t.Fatalf("disabled native struct reference gets = %d, want 0", off.Peephole["gc-native-final-struct-ref-get"])
	}
	if growth := onBytes - offBytes; growth > 256 {
		t.Fatalf("native struct reference get code growth = %d bytes, want <=256 (%d versus %d)", growth, onBytes, offBytes)
	}
	size := OptimizeSize
	compact, _ := compileNativeFinalRefGetARM64(t, true, &size)
	if got := compact.Peephole["gc-native-final-struct-ref-get"]; got != 0 {
		t.Fatalf("Size native struct reference gets = %d, want 0", got)
	}
}

func TestNativeFinalRefGetRejectsFunctionReferenceFieldARM64(t *testing.T) {
	structType := []byte{0x5f, 0x01, 0x70, 0x01}
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0xd0, byte(wasm.HeapFunc), 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xd1, 0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
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
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	compiled, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-ref-get": true},
		Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["gc-native-final-struct-ref-get"]; got != 0 {
		t.Fatalf("function-reference native struct gets = %d, want 0", got)
	}
	if got := stats.Funcs[0].Calls[callKindHostSync]; got != 2 {
		t.Fatalf("function-reference helper calls = %d, want 2", got)
	}
}

func TestNativeFinalCastRefGetFusionARM64(t *testing.T) {
	ref := wasm.RefVal(wasm.AbsRef(wasm.HeapAny))
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x63, byte(wasm.HeapAny), 0x01})...)
	body := []byte{
		0x23, 0x00,
		0xfb, 0x16, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0xd1,
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(ref, true, []byte{0xd0, byte(wasm.HeapAny), 0x0b}))),
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
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-ref-get": true},
		Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["final-cast-struct-get-fuse"]; got != 1 {
		t.Fatalf("final cast reference get fusions = %d, want 1", got)
	}
	if got := stats.Funcs[0].Peephole["gc-native-final-struct-ref-get"]; got != 1 {
		t.Fatalf("native final cast reference gets = %d, want 1", got)
	}
	if got := stats.Funcs[0].Calls[callKindHostSync]; got != 0 {
		t.Fatalf("native final cast reference helper calls = %d, want 0", got)
	}
	if got := stats.Funcs[0].Peephole["ref-is-null-fold"]; got != 0 {
		t.Fatalf("nullable field ref.is_null folds = %d, want 0", got)
	}
}

func BenchmarkNativeFinalRefGetCompileARM64(b *testing.B) {
	m := nativeFinalRefGetModuleARM64(b)
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
			opts := CompileOptions{GCStructHelpers: true, Optimizations: map[string]bool{"gc-native-final-ref-get": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}}
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
