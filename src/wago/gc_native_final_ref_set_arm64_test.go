//go:build arm64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeFinalRefSetBytesARM64(array bool) []byte {
	childType := []byte{0x5f, 0x01, 0x7f, 0x01}
	containerType := []byte{0x5f, 0x01, 0x63, 0x00, 0x01}
	body := []byte{0x01, 0x01, 0x63, 0x01} // one nullable container local
	if array {
		containerType = []byte{0x5e, 0x63, 0x00, 0x01}
		body = append(body,
			0x41, 0x01, 0xfb, 0x00, 0x00, // struct.new child
			0x41, 0x08, 0xfb, 0x06, 0x01, // array.new container
			0x21, 0x00,
		)
		for i := 0; i < 8; i++ {
			body = append(body,
				0x20, 0x00, 0x41, byte(i), 0xd0, 0x00,
				0xfb, 0x0e, 0x01,
			)
		}
		body = append(body, 0x20, 0x00, 0x41, 0x07, 0xfb, 0x0b, 0x01, 0xd1, 0x0b)
	} else {
		body = append(body,
			0x41, 0x01, 0xfb, 0x00, 0x00, // struct.new child
			0xfb, 0x00, 0x01, 0x21, 0x00, // struct.new container; local.set
		)
		for range 8 {
			body = append(body,
				0x20, 0x00, 0xd0, 0x00,
				0xfb, 0x05, 0x01, 0x00,
			)
		}
		body = append(body, 0x20, 0x00, 0xfb, 0x02, 0x01, 0x00, 0xd1, 0x0b)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(childType, containerType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcNativeFinalI31RefSetBytesARM64(array bool) []byte {
	containerType := []byte{0x5f, 0x01, 0x6e, 0x01}
	body := []byte{0x01, 0x01, 0x63, 0x00} // one nullable container local
	if array {
		containerType = []byte{0x5e, 0x6e, 0x01}
		body = append(body,
			0x41, 0x01, 0xfb, 0x1c,
			0x41, 0x08, 0xfb, 0x06, 0x00,
			0x21, 0x00,
		)
		for i := 0; i < 8; i++ {
			body = append(body,
				0x20, 0x00, 0x41, byte(i), 0x41, byte(i), 0xfb, 0x1c,
				0xfb, 0x0e, 0x00,
			)
		}
		body = append(body, 0x20, 0x00, 0x41, 0x07, 0xfb, 0x0b, 0x00, 0xfb, 0x16, 0x6c, 0xfb, 0x1e, 0x0b)
	} else {
		body = append(body,
			0x41, 0x01, 0xfb, 0x1c,
			0xfb, 0x00, 0x00, 0x21, 0x00,
		)
		for i := 0; i < 8; i++ {
			body = append(body,
				0x20, 0x00, 0x41, byte(i), 0xfb, 0x1c,
				0xfb, 0x05, 0x00, 0x00,
			)
		}
		body = append(body, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x16, 0x6c, 0xfb, 0x1e, 0x0b)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(containerType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCNativeFinalRefSetNullARM64(t *testing.T) {
	for _, array := range []bool{false, true} {
		kind := "struct"
		if array {
			kind = "array"
		}
		for _, enabled := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/off", true: "/on"}[enabled], func(t *testing.T) {
				config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-ref-set", enabled)
				compiled, err := Compile(config, gcNativeFinalRefSetBytesARM64(array))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
					StressNurseryBytes:   32,
					CollectEveryAlloc:    true,
					ForceMajorEveryMinor: true,
					VerifyAfterCollect:   true,
				}})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				for i := 0; i < 100; i++ {
					got, invokeErr := instance.Invoke("run")
					if invokeErr != nil || len(got) != 1 || got[0] != 1 {
						t.Fatalf("iteration %d: run = %v, %v; want [1]", i, got, invokeErr)
					}
				}
			})
		}
	}
}

func TestGCNativeFinalI31RefSetARM64(t *testing.T) {
	for _, array := range []bool{false, true} {
		kind := "struct"
		if array {
			kind = "array"
		}
		for _, enabled := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/off", true: "/on"}[enabled], func(t *testing.T) {
				config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-ref-set", enabled)
				compiled, err := Compile(config, gcNativeFinalI31RefSetBytesARM64(array))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
					StressNurseryBytes:   32,
					CollectEveryAlloc:    true,
					ForceMajorEveryMinor: true,
					VerifyAfterCollect:   true,
				}})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				for i := 0; i < 100; i++ {
					got, invokeErr := instance.Invoke("run")
					if invokeErr != nil || len(got) != 1 || got[0] != 7 {
						t.Fatalf("iteration %d: run = %v, %v; want [7]", i, got, invokeErr)
					}
				}
			})
		}
	}
}

func BenchmarkGCNativeFinalRefSetARM64(b *testing.B) {
	for _, array := range []bool{false, true} {
		kind := "struct"
		if array {
			kind = "array"
		}
		for _, enabled := range []bool{false, true} {
			name := kind + "/off"
			if enabled {
				name = kind + "/on"
			}
			b.Run(name, func(b *testing.B) {
				config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-ref-set", enabled)
				compiled, err := Compile(config, gcNativeFinalRefSetBytesARM64(array))
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
					if err != nil || len(got) != 1 || got[0] != 1 {
						b.Fatalf("run = %v, %v; want [1]", got, err)
					}
				}
			})
		}
	}
}

func BenchmarkGCNativeFinalI31RefSetARM64(b *testing.B) {
	for _, array := range []bool{false, true} {
		kind := "struct"
		if array {
			kind = "array"
		}
		for _, enabled := range []bool{false, true} {
			name := kind + "/off"
			if enabled {
				name = kind + "/on"
			}
			b.Run(name, func(b *testing.B) {
				config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-ref-set", enabled)
				compiled, err := Compile(config, gcNativeFinalI31RefSetBytesARM64(array))
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
					if err != nil || len(got) != 1 || got[0] != 7 {
						b.Fatalf("run = %v, %v; want [7]", got, err)
					}
				}
			})
		}
	}
}
