//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func immediateDeadGCConstructorsModuleARM64(t testing.TB, drop bool) *wasm.Module {
	t.Helper()
	arrayType := []byte{0x5e, 0x7f, 0x01} // (array (mut i32))
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec(
		[]byte{0x7f, 0x00}, // immutable i32
		[]byte{0x7f, 0x00}, // immutable i32
	)...)
	body := []byte{
		0x41, 0x2a, // retained function result 42
		0x41, 0x01, 0x41, 0x02,
		0xfb, 0x00, 0x01, // struct.new type 1
	}
	if drop {
		body = append(body, 0x1a)
	} else {
		body = append(body, 0xd1, 0x1a) // ref.is_null; drop: constructor is observable before its drop
	}
	if drop {
		body = append(body,
			0x41, 0x03, 0x41, 0x04,
			0xfb, 0x08, 0x00, 0x02, // array.new_fixed type 0, count 2
			0x1a,
		)
	}
	body = append(body, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, structType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
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

func TestImmediateDeadGCConstructorsARM64(t *testing.T) {
	m := immediateDeadGCConstructorsModuleARM64(t, true)
	compile := func(enabled bool) *CodegenStats {
		var stats ModuleStats
		if _, err := CompileModuleWith(m, CompileOptions{
			GCStructHelpers: true,
			GCArrayHelpers:  true,
			Stats:           &stats,
			Optimizations:   map[string]bool{"gc-dead-new": enabled},
		}); err != nil {
			t.Fatal(err)
		}
		return stats.Funcs[0]
	}
	on, off := compile(true), compile(false)
	if got := on.Peephole["gc-dead-new"]; got != 2 {
		t.Fatalf("gc-dead-new = %d, want 2 (all: %v)", got, on.Peephole)
	}
	if got := off.Peephole["gc-dead-new"]; got != 0 {
		t.Fatalf("disabled gc-dead-new = %d, want 0", got)
	}
	if on.Calls[callKindHostSync] != 2 || off.Calls[callKindHostSync] != 2 {
		t.Fatalf("helper calls enabled/disabled = %d/%d, want 2/2", on.Calls[callKindHostSync], off.Calls[callKindHostSync])
	}
	if on.GCCodeBytes.Allocation == 0 || on.GCCodeBytes.Allocation >= off.GCCodeBytes.Allocation {
		t.Fatalf("allocation bytes enabled/disabled = %d/%d, want a nonzero reduction", on.GCCodeBytes.Allocation, off.GCCodeBytes.Allocation)
	}
	t.Logf("code bytes enabled/disabled = %d/%d; allocation-family bytes = %d/%d",
		on.CodeBytes, off.CodeBytes, on.GCCodeBytes.Allocation, off.GCCodeBytes.Allocation)
}

func TestDeadGCConstructorObservableNearMissARM64(t *testing.T) {
	var stats ModuleStats
	if _, err := CompileModuleWith(immediateDeadGCConstructorsModuleARM64(t, false), CompileOptions{
		GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats,
	}); err != nil {
		t.Fatal(err)
	}
	if got := stats.Funcs[0].Peephole["gc-dead-new"]; got != 0 {
		t.Fatalf("observable constructor gc-dead-new = %d, want 0", got)
	}
}

func BenchmarkImmediateDeadGCConstructorsCompileARM64(b *testing.B) {
	m := immediateDeadGCConstructorsModuleARM64(b, true)
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			opts := CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Optimizations: map[string]bool{"gc-dead-new": enabled}}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(len(compiled.Code)), "code-B")
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := CompileModuleWith(m, opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
