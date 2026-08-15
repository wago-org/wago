//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcConstructorCastProductARM64() []byte {
	structType := []byte{0x5f, 0x00}
	arrayType := []byte{0x5e, 0x7f, 0x01}
	body := []byte{
		0xfb, 0x01, 0x00, 0xfb, 0x16, 0x00, 0xd1,
		0x41, 0x02, 0xfb, 0x07, 0x01, 0xfb, 0x17, 0x01, 0xd1, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCConstructorCastExecuteARM64(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
			WithBoundsChecks(BoundsChecksExplicit).
			WithOptimization("gc-constructor-cast", enabled)
		compiled, err := Compile(cfg, gcConstructorCastProductARM64())
		if err != nil {
			t.Fatal(err)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
			CollectEveryAlloc:  true,
			StressNurseryBytes: 64,
			VerifyAfterCollect: true,
		}})
		if err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		for i := 0; i < 100; i++ {
			got, invokeErr := instance.Invoke("run")
			if invokeErr != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("enabled=%v iteration %d: run = %v, %v; want [0]", enabled, i, got, invokeErr)
			}
		}
		if stats := instance.gc.Stats(); stats.Allocations != 200 {
			t.Fatalf("enabled=%v collector allocations = %d, want 200", enabled, stats.Allocations)
		}
		instance.Close()
		compiled.Close()
	}
}

func BenchmarkGCConstructorCastExecuteARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
				WithBoundsChecks(BoundsChecksExplicit).
				WithOptimization("gc-constructor-cast", enabled)
			compiled, err := Compile(cfg, gcConstructorCastProductARM64())
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
				if err != nil || len(got) != 1 || got[0] != 0 {
					b.Fatalf("run = %v, %v; want [0]", got, err)
				}
			}
		})
	}
}
