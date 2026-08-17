//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func nativeFinalArrayLenModuleARM64(t testing.TB) *wasm.Module {
	t.Helper()
	structType := []byte{0x5f, 0x00}
	arrayType := []byte{0x5e, 0x7f, 0x01}
	body := []byte{
		0x41, 0x03,
		0xfb, 0x07, 0x01, // array.new_default type 1
		0x24, 0x00, // global.set 0: erase constructor provenance
		0x23, 0x00, // global.get 0
		0xfb, 0x16, 0x01, // ref.cast (ref type 1)
		0xfb, 0x0f, // array.len
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
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

func TestNativeFinalArrayLenARM64(t *testing.T) {
	compile := func(enabled bool, objective *OptimizationObjective) *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(nativeFinalArrayLenModuleARM64(t), CompileOptions{
			GCStructHelpers: true,
			GCArrayHelpers:  true,
			Stats:           &stats,
			Optimizations:   map[string]bool{"gc-native-final-array-len": enabled},
			Objective:       objective,
		}); err != nil {
			t.Fatal(err)
		}
		return stats.Funcs[0]
	}
	on, off := compile(true, nil), compile(false, nil)
	if got := on.Peephole["gc-native-final-array-len"]; got != 1 {
		t.Fatalf("gc-native-final-array-len = %d, want 1 (all: %v)", got, on.Peephole)
	}
	if got := off.Peephole["gc-native-final-array-len"]; got != 0 {
		t.Fatalf("disabled gc-native-final-array-len = %d, want 0", got)
	}
	if on.Calls[callKindHostSync] != 1 || off.Calls[callKindHostSync] != 2 {
		t.Fatalf("helper calls enabled/disabled = %d/%d, want 1/2", on.Calls[callKindHostSync], off.Calls[callKindHostSync])
	}
	size := OptimizeSize
	compact := compile(true, &size)
	if got := compact.Peephole["gc-native-final-array-len"]; got != 0 {
		t.Fatalf("size gc-native-final-array-len = %d, want 0", got)
	}
	if got := compact.Calls[callKindHostSync]; got != 2 {
		t.Fatalf("size helper calls = %d, want 2", got)
	}
}

func BenchmarkNativeFinalArrayLenCompileARM64(b *testing.B) {
	m := nativeFinalArrayLenModuleARM64(b)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Optimizations: map[string]bool{"gc-native-final-array-len": enabled}}
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
