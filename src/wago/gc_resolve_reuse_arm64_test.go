//go:build arm64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcResolveReuseBytesARM64() []byte {
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x7f, 0x01}, []byte{0x7f, 0x01})...)
	initBody := []byte{
		0x00,
		0x41, 0x28,
		0x41, 0x02,
		0xfb, 0x00, 0x00,
		0x24, 0x00,
		0x0b,
	}
	runBody := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x23, 0x00,
		0x21, 0x00,
		0x20, 0x00,
		0xfb, 0x02, 0x00, 0x00,
	}
	for i := 1; i < 8; i++ {
		runBody = append(runBody, 0x20, 0x00, 0xfb, 0x02, 0x00, byte(i&1))
	}
	for i := 1; i < 8; i++ {
		runBody = append(runBody, 0x6a)
	}
	runBody = append(runBody, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			append(wasmtest.ULEB(uint32(len(runBody))), runBody...),
		)),
	)
}

func gcArrayResolveReuseBytesARM64() []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01}
	initBody := []byte{0x00, 0x41, 0x07, 0xfb, 0x07, 0x00, 0x24, 0x00, 0x0b}
	runBody := []byte{0x01, 0x01, 0x63, 0x00, 0x23, 0x00, 0x21, 0x00}
	for range 8 {
		runBody = append(runBody, 0x20, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x0f)
	}
	for i := 1; i < 8; i++ {
		runBody = append(runBody, 0x6a)
	}
	runBody = append(runBody, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			append(wasmtest.ULEB(uint32(len(runBody))), runBody...),
		)),
	)
}

func TestGCResolveReuseARM64(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcResolveReuseBytesARM64())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 168 {
		t.Fatalf("run = %v, %v; want [168]", got, err)
	}
}

func TestGCArrayResolveReuseARM64(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayResolveReuseBytesARM64())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 56 {
		t.Fatalf("run = %v, %v; want [56]", got, err)
	}
}

func BenchmarkGCResolveReuseARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-resolve-reuse", enabled)
			compiled, err := Compile(config, gcResolveReuseBytesARM64())
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
				if err != nil || len(got) != 1 || got[0] != 168 {
					b.Fatalf("run = %v, %v; want [168]", got, err)
				}
			}
		})
	}
}

func BenchmarkGCArrayResolveReuseARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-resolve-reuse", enabled)
			compiled, err := Compile(config, gcArrayResolveReuseBytesARM64())
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
				if err != nil || len(got) != 1 || got[0] != 56 {
					b.Fatalf("run = %v, %v; want [56]", got, err)
				}
			}
		})
	}
}
