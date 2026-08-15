//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func knownGCStructGetProductARM64() []byte {
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec(
		[]byte{0x7f, 0x00}, // immutable i32
		[]byte{0x78, 0x00}, // immutable i8
	)...)
	body := []byte{
		0x41, 0x2a, 0x41, 0x7f,
		0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x41, 0x07, 0x41, 0x7f,
		0xfb, 0x00, 0x00, 0xfb, 0x03, 0x00, 0x01, 0x6a,
		0xfb, 0x01, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	trapBody := []byte{
		0x41, 0x2a, // selected field 0 = 42
		0x41, 0x07, 0x20, 0x00, 0x6d, // unselected field 1 = 7 / divisor
		0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 0),
			wasmtest.ExportEntry("trap", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(trapBody))),
	)
}

func TestKnownGCStructGetExecuteARM64(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
			WithBoundsChecks(BoundsChecksExplicit).
			WithOptimization("gc-const-struct-get", enabled)
		compiled, err := Compile(cfg, knownGCStructGetProductARM64())
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
			if invokeErr != nil || len(got) != 1 || got[0] != 41 {
				t.Fatalf("enabled=%v iteration %d: run = %v, %v; want [41]", enabled, i, got, invokeErr)
			}
		}
		if _, err := instance.Invoke("trap", I32(0)); err == nil {
			t.Fatalf("enabled=%v unselected initializer did not trap", enabled)
		}
		got, err := instance.Invoke("trap", I32(1))
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("enabled=%v nontrapping initializer = %v, %v; want [42]", enabled, got, err)
		}
		if stats := instance.gc.Stats(); stats.Allocations != 301 {
			t.Fatalf("enabled=%v collector allocations = %d, want 301", enabled, stats.Allocations)
		}
		instance.Close()
		compiled.Close()
	}
}

func BenchmarkKnownGCStructGetExecuteARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).
				WithBoundsChecks(BoundsChecksExplicit).
				WithOptimization("gc-const-struct-get", enabled)
			compiled, err := Compile(cfg, knownGCStructGetProductARM64())
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
				if err != nil || len(got) != 1 || got[0] != 41 {
					b.Fatalf("run = %v, %v; want [41]", got, err)
				}
			}
		})
	}
}
