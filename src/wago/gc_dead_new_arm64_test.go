//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func deadGCConstructorsProductARM64() []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01} // (array (mut i32))
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec(
		[]byte{0x7f, 0x00},
		[]byte{0x7f, 0x00},
	)...)
	body := []byte{
		0x41, 0x2a,
		0x41, 0x01, 0x41, 0x02, 0xfb, 0x00, 0x01, 0x1a,
		0x41, 0x03, 0x41, 0x04, 0xfb, 0x08, 0x00, 0x02, 0x1a,
		0x0b,
	}
	trapBody := []byte{
		0x41, 0x2a, // retained function result 42
		0x41, 0x01,
		0x41, 0x07, 0x20, 0x00, 0x6d, // 7 / divisor: traps before constructor allocation
		0xfb, 0x00, 0x01, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			arrayType,
			structType,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 0),
			wasmtest.ExportEntry("trap", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(trapBody))),
	)
}

func TestDeadGCConstructorsExecuteARM64(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), deadGCConstructorsProductARM64())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
		CollectEveryAlloc:  true,
		StressNurseryBytes: 64,
		VerifyAfterCollect: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for i := 0; i < 100; i++ {
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("iteration %d: run = %v, %v; want [42]", i, got, err)
		}
	}
	if _, err := instance.Invoke("trap", I32(0)); err == nil {
		t.Fatal("division-by-zero constructor operand did not trap")
	}
	got, err := instance.Invoke("trap", I32(1))
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("nontrapping constructor operand = %v, %v; want [42]", got, err)
	}
	if stats := instance.gc.Stats(); stats.Allocations != 201 {
		t.Fatalf("collector allocations = %d, want 201 retained allocations", stats.Allocations)
	}
}

func BenchmarkDeadGCConstructorsExecuteARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
				WithBoundsChecks(BoundsChecksExplicit).
				WithOptimization("gc-dead-new", enabled)
			compiled, err := Compile(cfg, deadGCConstructorsProductARM64())
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 64 << 10}})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, err := instance.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 42 {
					b.Fatalf("run = %v, %v; want [42]", got, err)
				}
			}
		})
	}
}
