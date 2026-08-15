//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeFinalScalarGetModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x7f, 0x01})...)
	body := []byte{
		0x41, 0x2a,
		0xfb, 0x00, 0x00, // struct.new type 0
		0x24, 0x00, // global.set 0: erase constructor provenance
		0x23, 0x00, // global.get 0
		0xfb, 0x16, 0x00, // ref.cast (ref type 0)
		0xfb, 0x02, 0x00, 0x00, // struct.get type 0 field 0
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.RefVal(wasm.AbsRef(wasm.HeapAny)), true, []byte{0xd0, 0x6e, 0x0b}))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
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

func nativeFinalDirectScalarGetModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x7f, 0x01})...)
	body := []byte{
		0x41, 0x2a,
		0xfb, 0x00, 0x00, // struct.new type 0
		0x24, 0x00, // global.set 0: erase constructor provenance
		0x23, 0x00, // global.get 0: statically (ref null 0)
		0xfb, 0x02, 0x00, 0x00, // struct.get type 0 field 0
		0x0b,
	}
	definedGlobal := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b} // (mut (ref null 0)) (ref.null 0)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(definedGlobal)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
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

func TestNativeFinalScalarGetARM64(t *testing.T) {
	m := nativeFinalScalarGetModuleARM64(t)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compile := func(enabled bool, objective *OptimizationObjective) *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			Stats:           &stats,
			Optimizations:   map[string]bool{"gc-native-final-scalar-get": enabled},
			Objective:       objective,
			Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
		}); err != nil {
			t.Fatal(err)
		}
		return stats.Funcs[0]
	}
	on, off := compile(true, nil), compile(false, nil)
	if got := on.Peephole["gc-native-final-struct-scalar-get"]; got != 1 {
		t.Fatalf("gc-native-final-struct-scalar-get = %d, want 1 (all: %v)", got, on.Peephole)
	}
	if got := off.Peephole["gc-native-final-struct-scalar-get"]; got != 0 {
		t.Fatalf("disabled gc-native-final-struct-scalar-get = %d, want 0", got)
	}
	if on.Calls[callKindHostSync] != 1 || off.Calls[callKindHostSync] != 2 {
		t.Fatalf("helper calls enabled/disabled = %d/%d, want 1/2", on.Calls[callKindHostSync], off.Calls[callKindHostSync])
	}
	size := OptimizeSize
	compact := compile(true, &size)
	if got := compact.Peephole["gc-native-final-struct-scalar-get"]; got != 0 {
		t.Fatalf("size gc-native-final-struct-scalar-get = %d, want 0", got)
	}
	if got := compact.Calls[callKindHostSync]; got != 2 {
		t.Fatalf("size helper calls = %d, want 2", got)
	}
}

func TestNativeFinalDirectScalarGetARM64(t *testing.T) {
	m := nativeFinalDirectScalarGetModuleARM64(t)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compile := func(enabled bool) *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			Stats:           &stats,
			Optimizations:   map[string]bool{"gc-native-final-scalar-get": enabled},
			Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
		}); err != nil {
			t.Fatal(err)
		}
		return stats.Funcs[0]
	}
	on, off := compile(true), compile(false)
	if got := on.Peephole["gc-native-final-struct-scalar-get"]; got != 1 {
		t.Fatalf("direct gc-native-final-struct-scalar-get = %d, want 1", got)
	}
	if got := off.Peephole["gc-native-final-struct-scalar-get"]; got != 0 {
		t.Fatalf("disabled direct gc-native-final-struct-scalar-get = %d, want 0", got)
	}
	if on.Calls[callKindHostSync] != 1 || off.Calls[callKindHostSync] != 2 {
		t.Fatalf("direct helper calls enabled/disabled = %d/%d, want 1/2", on.Calls[callKindHostSync], off.Calls[callKindHostSync])
	}
}

func TestNativeFinalScalarGetRejectsReferenceFieldARM64(t *testing.T) {
	ref := wasm.RefVal(wasm.AbsRef(wasm.HeapAny))
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x63, byte(wasm.HeapAny), 0x01})...)
	body := []byte{
		0xd0, 0x6e,
		0xfb, 0x00, 0x00,
		0x24, 0x00,
		0x23, 0x00,
		0xfb, 0x16, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0xd1,
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(ref, true, []byte{0xd0, 0x6e, 0x0b}))),
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
	if _, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		Stats:           &stats,
		Optimizations:   map[string]bool{"gc-native-final-scalar-get": true, "gc-native-final-ref-get": false},
		Codegen:         codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["gc-native-final-struct-scalar-get"]; got != 0 {
		t.Fatalf("reference-field gc-native-final-struct-scalar-get = %d, want 0", got)
	}
	if got := stats.Funcs[0].Calls[callKindHostSync]; got != 2 {
		t.Fatalf("reference-field helper calls = %d, want 2", got)
	}
}

func BenchmarkNativeFinalScalarGetCompileARM64(b *testing.B) {
	m := nativeFinalScalarGetModuleARM64(b)
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
			opts := CompileOptions{GCStructHelpers: true, Optimizations: map[string]bool{"gc-native-final-scalar-get": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				b.Fatal(err)
			}
			codeBytes := len(compiled.Code)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := CompileModuleWith(m, opts); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
	}
}

func BenchmarkNativeFinalDirectScalarGetCompileARM64(b *testing.B) {
	m := nativeFinalDirectScalarGetModuleARM64(b)
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
			opts := CompileOptions{GCStructHelpers: true, Optimizations: map[string]bool{"gc-native-final-scalar-get": enabled}, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: metadata.Layouts}}}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				b.Fatal(err)
			}
			codeBytes := len(compiled.Code)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := CompileModuleWith(m, opts); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
	}
}
