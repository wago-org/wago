//go:build arm64

package wago

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeFinalArrayScalarSetCaseBytesARM64(storage byte, valueType wasm.ValType, get []byte) []byte {
	arrayType := []byte{0x5e, storage, 0x01}
	initBody := []byte{0x00, 0x41, 0x01, 0xfb, 0x07, 0x00, 0x24, 0x00, 0x0b}
	setBody := []byte{0x00, 0x23, 0x00, 0x41, 0x00, 0x20, 0x00, 0xfb, 0x0e, 0x00, 0x0b}
	getBody := []byte{0x00, 0x23, 0x00, 0x41, 0x00}
	getBody = append(getBody, get...)
	getBody = append(getBody, 0x00, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			arrayType,
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

func gcNativeFinalArrayScalarSetBytesARM64() []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01}
	initBody := []byte{0x00, 0x41, 0x08, 0xfb, 0x07, 0x00, 0x24, 0x00, 0x0b}
	runBody := []byte{0x01, 0x01, 0x63, 0x00, 0x23, 0x00, 0x21, 0x00}
	for i := 0; i < 8; i++ {
		runBody = append(runBody, 0x20, 0x00, 0x41, byte(i), 0x41, byte(10+i), 0xfb, 0x0e, 0x00)
	}
	runBody = append(runBody, 0x20, 0x00, 0x41, 0x07, 0xfb, 0x0b, 0x00, 0x0b)
	setBody := []byte{0x00, 0x23, 0x00, 0x20, 0x00, 0x20, 0x01, 0xfb, 0x0e, 0x00, 0x0b}
	getBody := []byte{0x00, 0x23, 0x00, 0x20, 0x00, 0xfb, 0x0b, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			arrayType,
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
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

func TestGCNativeFinalArrayScalarSetARM64(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeFinalArrayScalarSetBytesARM64())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("set", 0, 1); err == nil || !strings.Contains(err.Error(), "null reference") {
		t.Fatalf("pre-init set error = %v, want null reference", err)
	}
	if _, err := instance.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Invoke("set", 8, 1); err == nil {
		t.Fatal("set(8) succeeded, want builtin bounds trap")
	} else {
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapBuiltin {
			t.Fatalf("set(8) error = %v, want TrapBuiltin", err)
		}
	}
	if _, err := instance.Invoke("set", 3, 99); err != nil {
		t.Fatal(err)
	}
	if got, err := instance.Invoke("get", 3); err != nil || len(got) != 1 || got[0] != 99 {
		t.Fatalf("get(3) = %v, %v; want [99]", got, err)
	}
	if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
		t.Fatalf("run = %v, %v; want [17]", got, err)
	}
}

func TestGCNativeFinalArrayScalarSetStorageClassesARM64(t *testing.T) {
	tests := []struct {
		name      string
		storage   byte
		valueType wasm.ValType
		get       []byte
		value     uint64
		want      uint64
	}{
		{"i8 signed", 0x78, wasm.I32, []byte{0xfb, 0x0c}, uint64(uint32(0xffffffff)), uint64(uint32(0xffffffff))},
		{"i8 unsigned", 0x78, wasm.I32, []byte{0xfb, 0x0d}, 0xff, 0xff},
		{"i16 signed", 0x77, wasm.I32, []byte{0xfb, 0x0c}, uint64(uint32(0xfffffffe)), uint64(uint32(0xfffffffe))},
		{"i16 unsigned", 0x77, wasm.I32, []byte{0xfb, 0x0d}, 0xfffe, 0xfffe},
		{"i64", 0x7e, wasm.I64, []byte{0xfb, 0x0b}, 42, 42},
		{"f32", 0x7d, wasm.F32, []byte{0xfb, 0x0b}, uint64(math.Float32bits(1.5)), uint64(math.Float32bits(1.5))},
		{"f64", 0x7c, wasm.F64, []byte{0xfb, 0x0b}, math.Float64bits(1.5), math.Float64bits(1.5)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeFinalArrayScalarSetCaseBytesARM64(tc.storage, tc.valueType, tc.get))
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

func BenchmarkGCNativeFinalArrayScalarSetARM64(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "off"
		if enabled {
			name = "on"
		}
		b.Run(name, func(b *testing.B) {
			config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-final-array-scalar-set", enabled)
			compiled, err := Compile(config, gcNativeFinalArrayScalarSetBytesARM64())
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
