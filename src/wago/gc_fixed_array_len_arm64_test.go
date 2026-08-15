//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func fixedGCArrayLenProductARM64() []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01} // (array (mut i32))
	body := []byte{
		0x41, 0x03, 0x41, 0x04,
		0xfb, 0x08, 0x00, 0x02, // array.new_fixed type 0, count 2
		0xfb, 0x0f, // array.len
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func knownDynamicGCArrayLenProductARM64() []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01} // (array (mut i32))
	body := []byte{
		0x41, 0x07, 0x41, 0x03,
		0xfb, 0x06, 0x00, 0xfb, 0x0f, // array.new 0; array.len => 3
		0x41, 0x04, 0xfb, 0x07, 0x00, 0xfb, 0x0f, 0x6a, // default => +4
		0x41, 0x00, 0x41, 0x02,
		0xfb, 0x09, 0x00, 0x00, 0xfb, 0x0f, 0x6a, // data => +2
		0x0b,
	}
	dataEntry := append([]byte{0x01}, wasmtest.ULEB(8)...)
	dataEntry = append(dataEntry, 1, 0, 0, 0, 2, 0, 0, 0)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		wasmtest.Section(11, wasmtest.Vec(dataEntry)),
	)
}

func TestFixedGCArrayLenExecuteARM64(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
			WithBoundsChecks(BoundsChecksExplicit).
			WithOptimization("gc-fixed-array-len", enabled)
		compiled, err := Compile(cfg, fixedGCArrayLenProductARM64())
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
			if invokeErr != nil || len(got) != 1 || got[0] != 2 {
				t.Fatalf("enabled=%v iteration %d: run = %v, %v; want [2]", enabled, i, got, invokeErr)
			}
		}
		if stats := instance.gc.Stats(); stats.Allocations != 100 {
			t.Fatalf("enabled=%v collector allocations = %d, want 100", enabled, stats.Allocations)
		}
		instance.Close()
		compiled.Close()
	}
}

func TestKnownDynamicGCArrayLenExecuteARM64(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
			WithBoundsChecks(BoundsChecksExplicit).
			WithOptimization("gc-fixed-array-len", enabled)
		compiled, err := Compile(cfg, knownDynamicGCArrayLenProductARM64())
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
			if invokeErr != nil || len(got) != 1 || got[0] != 9 {
				t.Fatalf("enabled=%v iteration %d: run = %v, %v; want [9]", enabled, i, got, invokeErr)
			}
		}
		if stats := instance.gc.Stats(); stats.Allocations != 300 {
			t.Fatalf("enabled=%v collector allocations = %d, want 300", enabled, stats.Allocations)
		}
		instance.Close()
		compiled.Close()
	}
}

func BenchmarkFixedGCArrayLenExecuteARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
				WithBoundsChecks(BoundsChecksExplicit).
				WithOptimization("gc-fixed-array-len", enabled)
			compiled, err := Compile(cfg, fixedGCArrayLenProductARM64())
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
				if err != nil || len(got) != 1 || got[0] != 2 {
					b.Fatalf("run = %v, %v; want [2]", got, err)
				}
			}
		})
	}
}

func BenchmarkKnownDynamicGCArrayLenExecuteARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
				WithBoundsChecks(BoundsChecksExplicit).
				WithOptimization("gc-fixed-array-len", enabled)
			compiled, err := Compile(cfg, knownDynamicGCArrayLenProductARM64())
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
				if err != nil || len(got) != 1 || got[0] != 9 {
					b.Fatalf("run = %v, %v; want [9]", got, err)
				}
			}
		})
	}
}
