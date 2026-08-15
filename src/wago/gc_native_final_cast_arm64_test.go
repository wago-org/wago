//go:build arm64

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeFinalCastBytesARM64() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	initBody := []byte{0xfb, 0x01, 0x00, 0x24, 0x00, 0x0b}
	runBody := []byte{0x01, 0x01, 0x6e, 0x23, 0x00, 0x21, 0x00}
	for range 8 {
		runBody = append(runBody, 0x20, 0x00, 0xfb, 0x16, 0x00, 0x1a)
	}
	runBody = append(runBody, 0x41, 0x2a, 0x0b)
	nullableBody := []byte{0xd0, 0x00, 0xfb, 0x17, 0x00, 0xd1, 0x0b}
	nullFailBody := []byte{0xd0, 0x00, 0xfb, 0x16, 0x00, 0xd1, 0x0b}
	i31FailBody := []byte{0x41, 0x00, 0xfb, 0x1c, 0xfb, 0x16, 0x00, 0xd1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.RefVal(wasm.AbsRef(wasm.HeapAny)), true, []byte{0xd0, 0x6e, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("nullable", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("null_fail", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("i31_fail", byte(wasm.ExternFunc), 4),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(initBody), append(wasmtest.ULEB(uint32(len(runBody))), runBody...), wasmtest.Code(nullableBody),
			wasmtest.Code(nullFailBody), wasmtest.Code(i31FailBody),
		)),
	)
}

func TestGCNativeFinalCastARM64(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-cast", enabled), gcNativeFinalCastBytesARM64())
		if err != nil {
			t.Fatal(err)
		}
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}})
		if err != nil {
			compiled.Close()
			t.Fatal(err)
		}
		if _, err := instance.Invoke("init"); err != nil {
			t.Fatalf("enabled=%v: init: %v", enabled, err)
		}
		if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("enabled=%v: run = %v, %v; want [42]", enabled, got, err)
		}
		if got, err := instance.Invoke("nullable"); err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("enabled=%v: nullable = %v, %v; want [1]", enabled, got, err)
		}
		for _, name := range []string{"null_fail", "i31_fail"} {
			if _, err := instance.Invoke(name); err == nil {
				t.Fatalf("enabled=%v: %s succeeded, want cast trap", enabled, name)
			} else {
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
					t.Fatalf("enabled=%v: %s error = %v, want TrapCastFailure", enabled, name, err)
				}
			}
		}
		instance.Close()
		compiled.Close()
	}
}

func BenchmarkGCNativeFinalCastARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-cast", enabled), gcNativeFinalCastBytesARM64())
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 20}})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			if _, err := instance.Invoke("init"); err != nil {
				b.Fatal(err)
			}
			fn, err := instance.PrepareFunction("run")
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, err := fn.Invoke()
				if err != nil || len(got) != 1 || got[0] != 42 {
					b.Fatalf("run = %v, %v; want [42]", got, err)
				}
			}
		})
	}
}
