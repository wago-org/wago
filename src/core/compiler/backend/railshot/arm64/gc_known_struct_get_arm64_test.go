//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func knownGCStructGetModuleARM64(t testing.TB, dynamic bool) *wasm.Module {
	t.Helper()
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec(
		[]byte{0x7f, 0x00}, // immutable i32
		[]byte{0x78, 0x00}, // immutable i8
	)...)
	params := []wasm.ValType(nil)
	body := []byte(nil)
	if dynamic {
		params = []wasm.ValType{wasm.I32}
		body = append(body, 0x20, 0x00, 0x41, 0x00) // dynamic field 0; field 1 = 0
		body = append(body, 0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b)
	} else {
		body = append(body,
			0x41, 0x2a, 0x41, 0x7f, // 42, -1
			0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, // new; get field 0 => 42
			0x41, 0x07, 0x41, 0x7f,
			0xfb, 0x00, 0x00, 0xfb, 0x03, 0x00, 0x01, 0x6a, // get_s field 1 => -1; add
			0xfb, 0x01, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a, // default; get field 0 => 0; add
			0x0b,
		)
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
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
	return m
}

func TestKnownGCStructGetARM64(t *testing.T) {
	compile := func(enabled bool) *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(knownGCStructGetModuleARM64(t, false), CompileOptions{
			GCStructHelpers: true,
			Stats:           &stats,
			Optimizations:   map[string]bool{"gc-const-struct-get": enabled},
		}); err != nil {
			t.Fatal(err)
		}
		return stats.Funcs[0]
	}
	on, off := compile(true), compile(false)
	if got := on.Peephole["gc-known-struct-get"]; got != 3 {
		t.Fatalf("gc-known-struct-get = %d, want 3 (all: %v)", got, on.Peephole)
	}
	if got := off.Peephole["gc-known-struct-get"]; got != 0 {
		t.Fatalf("disabled gc-known-struct-get = %d, want 0", got)
	}
	if on.Calls[callKindHostSync] != 3 || off.Calls[callKindHostSync] != 6 {
		t.Fatalf("helper calls enabled/disabled = %d/%d, want 3/6", on.Calls[callKindHostSync], off.Calls[callKindHostSync])
	}
	if on.CodeBytes >= off.CodeBytes {
		t.Fatalf("code bytes enabled/disabled = %d/%d, want reduction", on.CodeBytes, off.CodeBytes)
	}
}

func TestKnownGCStructGetRejectsDynamicInitializerARM64(t *testing.T) {
	var stats ModuleStats
	if _, err := CompileModuleWith(knownGCStructGetModuleARM64(t, true), CompileOptions{
		GCStructHelpers: true,
		Stats:           &stats,
	}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["gc-known-struct-get"]; got != 0 {
		t.Fatalf("dynamic gc-known-struct-get = %d, want 0", got)
	}
	if got := stats.Funcs[0].Calls[callKindHostSync]; got != 2 {
		t.Fatalf("dynamic helper calls = %d, want 2", got)
	}
}

func BenchmarkKnownGCStructGetCompileARM64(b *testing.B) {
	m := knownGCStructGetModuleARM64(b, false)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{GCStructHelpers: true, Optimizations: map[string]bool{"gc-const-struct-get": enabled}}
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
