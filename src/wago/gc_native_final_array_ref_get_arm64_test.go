//go:build arm64

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeFinalArrayRefGetBytesARM64() []byte {
	childType := []byte{0x5f, 0x01, 0x7f, 0x01}
	arrayType := []byte{0x5e, 0x6e, 0x01}
	body := []byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x63, 0x01,
		0xfb, 0x01, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x08, 0xfb, 0x06, 0x01, 0x21, 0x01,
	}
	for i := 0; i < 7; i++ {
		body = append(body, 0x20, 0x01, 0x41, byte(i), 0xfb, 0x0b, 0x01, 0x1a)
	}
	body = append(body,
		0x20, 0x01, 0x41, 0x07, 0xfb, 0x0b, 0x01,
		0xfb, 0x01, 0x00, 0x1a,
		0xfb, 0x16, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0x0b,
	)
	getBody := []byte{
		0xfb, 0x01, 0x00,
		0x41, 0x01, 0xfb, 0x06, 0x01,
		0x20, 0x00, 0xfb, 0x0b, 0x01,
		0xd1, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			childType,
			arrayType,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(body))), body...),
			wasmtest.Code(getBody),
		)),
	)
}

func TestGCNativeFinalArrayRefGetARM64(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-ref-get", enabled)
		compiled, err := Compile(config, gcNativeFinalArrayRefGetBytesARM64())
		if err != nil {
			t.Fatal(err)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
			StressNurseryBytes:   64,
			CollectEveryAlloc:    true,
			ForceMajorEveryMinor: true,
			VerifyAfterCollect:   true,
		}})
		if err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		for i := 0; i < 100; i++ {
			got, invokeErr := instance.Invoke("run")
			if invokeErr != nil || len(got) != 1 || got[0] != 0 {
				instance.Close()
				compiled.Close()
				t.Fatalf("enabled=%v iteration %d: run = %v, %v; want [0]", enabled, i, got, invokeErr)
			}
		}
		if got, invokeErr := instance.Invoke("get", 0); invokeErr != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("enabled=%v: get(0) = %v, %v; want [0]", enabled, got, invokeErr)
		}
		if _, invokeErr := instance.Invoke("get", 1); invokeErr == nil {
			t.Fatalf("enabled=%v: get(1) succeeded, want bounds trap", enabled)
		} else {
			var trap *TrapError
			if !errors.As(invokeErr, &trap) || trap.Code != TrapBuiltin {
				t.Fatalf("enabled=%v: get(1) error = %v, want TrapBuiltin", enabled, invokeErr)
			}
		}
		instance.Close()
		compiled.Close()
	}
}

func BenchmarkGCNativeFinalArrayRefGetARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-ref-get", enabled)
			compiled, err := Compile(config, gcNativeFinalArrayRefGetBytesARM64())
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 20}})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			fn, err := instance.PrepareFunction("run")
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, err := fn.Invoke()
				if err != nil || len(got) != 1 || got[0] != 0 {
					b.Fatalf("run = %v, %v; want [0]", got, err)
				}
			}
		})
	}
}
