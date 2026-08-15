//go:build arm64

package wago

import (
	"math"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeFinalStructScalarSetCaseBytesARM64(storage byte, valueType wasm.ValType, get byte) []byte {
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{storage, 0x01})...)
	initBody := []byte{0x00, 0xfb, 0x01, 0x00, 0x24, 0x00, 0x0b}
	setBody := []byte{0x00, 0x23, 0x00, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x00, 0x0b}
	getBody := []byte{0x00, 0x23, 0x00, 0xfb, get, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{valueType}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{valueType}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 0),
			wasmtest.ExportEntry("set", 0, 1),
			wasmtest.ExportEntry("get", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			append(wasmtest.ULEB(uint32(len(setBody))), setBody...),
			append(wasmtest.ULEB(uint32(len(getBody))), getBody...),
		)),
	)
}

func gcNativeFinalStructScalarSetBytesARM64() []byte {
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x7f, 0x01}, []byte{0x7f, 0x01})...)
	initBody := []byte{0x00, 0xfb, 0x01, 0x00, 0x24, 0x00, 0x0b}
	runBody := []byte{0x01, 0x01, 0x63, 0x00, 0x23, 0x00, 0x21, 0x00}
	for i := 0; i < 8; i++ {
		runBody = append(runBody, 0x20, 0x00, 0x41, byte(10+i), 0xfb, 0x05, 0x00, byte(i&1))
	}
	runBody = append(runBody, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x01, 0x0b)
	setBody := []byte{0x00, 0x23, 0x00, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x00, 0x0b}
	getBody := []byte{0x00, 0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 0),
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("set", 0, 2),
			wasmtest.ExportEntry("get", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			append(wasmtest.ULEB(uint32(len(runBody))), runBody...),
			append(wasmtest.ULEB(uint32(len(setBody))), setBody...),
			append(wasmtest.ULEB(uint32(len(getBody))), getBody...),
		)),
	)
}

func TestGCNativeFinalStructScalarSetARM64(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeFinalStructScalarSetBytesARM64())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("set", 1); err == nil || !strings.Contains(err.Error(), "null reference") {
		t.Fatalf("pre-init set error = %v, want null reference", err)
	}
	if _, err := instance.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Invoke("set", 99); err != nil {
		t.Fatal(err)
	}
	if got, err := instance.Invoke("get"); err != nil || len(got) != 1 || got[0] != 99 {
		t.Fatalf("get = %v, %v; want [99]", got, err)
	}
	if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
		t.Fatalf("run = %v, %v; want [17]", got, err)
	}
}

func TestGCNativeFinalStructScalarSetStorageClassesARM64(t *testing.T) {
	tests := []struct {
		name      string
		storage   byte
		valueType wasm.ValType
		get       byte
		value     uint64
		want      uint64
	}{
		{"i8 signed", 0x78, wasm.I32, 0x03, uint64(uint32(0xffffffff)), uint64(uint32(0xffffffff))},
		{"i8 unsigned", 0x78, wasm.I32, 0x04, 0xff, 0xff},
		{"i16 signed", 0x77, wasm.I32, 0x03, uint64(uint32(0xfffffffe)), uint64(uint32(0xfffffffe))},
		{"i16 unsigned", 0x77, wasm.I32, 0x04, 0xfffe, 0xfffe},
		{"i64", 0x7e, wasm.I64, 0x02, 42, 42},
		{"f32", 0x7d, wasm.F32, 0x02, uint64(math.Float32bits(1.5)), uint64(math.Float32bits(1.5))},
		{"f64", 0x7c, wasm.F64, 0x02, math.Float64bits(1.5), math.Float64bits(1.5)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeFinalStructScalarSetCaseBytesARM64(tc.storage, tc.valueType, tc.get))
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
			if _, err := instance.Invoke("set", tc.value); err != nil {
				t.Fatal(err)
			}
			if got, err := instance.Invoke("get"); err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("get = %v, %v; want [%#x]", got, err, tc.want)
			}
		})
	}
}

func BenchmarkGCNativeFinalStructScalarSetARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-scalar-set", enabled)
			compiled, err := Compile(config, gcNativeFinalStructScalarSetBytesARM64())
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
				if err != nil || len(got) != 1 || got[0] != 17 {
					b.Fatalf("run = %v, %v; want [17]", got, err)
				}
			}
		})
	}
}
