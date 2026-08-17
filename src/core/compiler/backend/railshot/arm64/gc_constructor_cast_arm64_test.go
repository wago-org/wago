//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcConstructorCastModuleARM64(t testing.TB, adjacent bool) *wasm.Module {
	t.Helper()
	structType := []byte{0x5f, 0x00}
	arrayType := []byte{0x5e, 0x7f, 0x01}
	between := []byte(nil)
	if !adjacent {
		between = []byte{0xd4} // ref.as_non_null
	}
	body := []byte{0xfb, 0x01, 0x00} // struct.new_default 0
	body = append(body, between...)
	body = append(body, 0xfb, 0x16, 0x00, 0xd1)       // ref.cast 0; ref.is_null
	body = append(body, 0x41, 0x02, 0xfb, 0x07, 0x01) // array.new_default 1
	body = append(body, between...)
	body = append(body, 0xfb, 0x17, 0x01, 0xd1, 0x6a, 0x0b) // ref.cast_null 1; is_null; add
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
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

func TestGCConstructorCastARM64(t *testing.T) {
	compile := func(enabled bool) *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(gcConstructorCastModuleARM64(t, true), CompileOptions{
			GCStructHelpers: true,
			GCArrayHelpers:  true,
			Stats:           &stats,
			Optimizations:   map[string]bool{"gc-constructor-cast": enabled},
		}); err != nil {
			t.Fatal(err)
		}
		return stats.Funcs[0]
	}
	on, off := compile(true), compile(false)
	if got := on.Peephole["gc-constructor-cast"]; got != 2 {
		t.Fatalf("gc-constructor-cast = %d, want 2 (all: %v)", got, on.Peephole)
	}
	if got := off.Peephole["gc-constructor-cast"]; got != 0 {
		t.Fatalf("disabled gc-constructor-cast = %d, want 0", got)
	}
	if on.Calls[callKindHostSync] != 2 || off.Calls[callKindHostSync] != 4 {
		t.Fatalf("helper calls enabled/disabled = %d/%d, want 2/4", on.Calls[callKindHostSync], off.Calls[callKindHostSync])
	}
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("code bytes enabled/disabled = %d/%d, want reduction", on.CodeBytes, off.CodeBytes)
	}
}

func TestGCConstructorCastRejectsNonAdjacentARM64(t *testing.T) {
	var stats ModuleStats
	if _, err := CompileModuleWith(gcConstructorCastModuleARM64(t, false), CompileOptions{
		GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats,
	}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["gc-constructor-cast"]; got != 0 {
		t.Fatalf("non-adjacent gc-constructor-cast = %d, want 0", got)
	}
	if got := stats.Funcs[0].Calls[callKindHostSync]; got != 4 {
		t.Fatalf("non-adjacent helper calls = %d, want 4", got)
	}
}

func BenchmarkGCConstructorCastCompileARM64(b *testing.B) {
	m := gcConstructorCastModuleARM64(b, true)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Optimizations: map[string]bool{"gc-constructor-cast": enabled}}
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
