//go:build (linux || darwin) && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcFinalCastStructScalarBodyModuleARM64(structType []byte, result wasm.ValType, body []byte) []byte {
	types := wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, []wasm.ValType{result})))
	funcs := wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1)))
	globals := wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.RefVal(wasm.AbsRef(wasm.HeapAny)), true, []byte{0xd0, 0x6e, 0x0b})))
	exports := wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0)))
	return wasmtest.Module(types, funcs, globals, exports, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func gcFinalCastStructScalarGetModuleARM64(structType []byte, result wasm.ValType, initializer []byte, sub byte) []byte {
	body := append([]byte(nil), initializer...)
	body = append(body,
		0xfb, 0x00, 0x00, // struct.new type 0
		0x24, 0x00, // global.set 0
		0x23, 0x00, // global.get 0
		0xfb, 0x16, 0x00, // ref.cast (ref type 0)
		0xfb, sub, 0x00, 0x00, // struct.get[_s/_u] type 0 field 0
		0x0b,
	)
	return gcFinalCastStructScalarBodyModuleARM64(structType, result, body)
}

func gcFinalCastStructI32GetBytesARM64() []byte {
	structType := append([]byte{0x5f}, wasmtest.Vec([]byte{0x7f, 0x01})...)
	return gcFinalCastStructScalarGetModuleARM64(structType, wasm.I32, []byte{0x41, 0x2a}, 2)
}

func TestGCFinalCastStructScalarGetARM64(t *testing.T) {
	f32 := make([]byte, 5)
	f32[0] = 0x43
	binary.LittleEndian.PutUint32(f32[1:], math.Float32bits(1.5))
	f64 := make([]byte, 9)
	f64[0] = 0x44
	binary.LittleEndian.PutUint64(f64[1:], math.Float64bits(-2.25))
	tests := []struct {
		name        string
		storage     byte
		result      wasm.ValType
		initializer []byte
		sub         byte
		want        uint64
	}{
		{name: "i32", storage: 0x7f, result: wasm.I32, initializer: []byte{0x41, 0x2a}, sub: 2, want: 42},
		{name: "i64", storage: 0x7e, result: wasm.I64, initializer: []byte{0x42, 0x2a}, sub: 2, want: 42},
		{name: "f32", storage: 0x7d, result: wasm.F32, initializer: f32, sub: 2, want: uint64(math.Float32bits(1.5))},
		{name: "f64", storage: 0x7c, result: wasm.F64, initializer: f64, sub: 2, want: math.Float64bits(-2.25)},
		{name: "i8 signed", storage: 0x78, result: wasm.I32, initializer: []byte{0x41, 0x7f}, sub: 3, want: uint64(^uint32(0))},
		{name: "i16 unsigned", storage: 0x77, result: wasm.I32, initializer: []byte{0x41, 0x7f}, sub: 4, want: 0xffff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			structType := append([]byte{0x5f}, wasmtest.Vec([]byte{tc.storage, 0x01})...)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructScalarGetModuleARM64(structType, tc.result, tc.initializer, tc.sub))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("run = %v, %v; want [%d]", got, err, tc.want)
			}
		})
	}
}

func TestGCFinalCastStructScalarGetTrapOrderARM64(t *testing.T) {
	structType := append([]byte{0x5f}, wasmtest.Vec([]byte{0x7f, 0x01})...)
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{name: "nullable null reaches get", body: []byte{0xd0, 0x00, 0xfb, 0x17, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}, want: "null reference"},
		{name: "nonnullable null fails cast", body: []byte{0xd0, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}, want: "cast failure"},
		{name: "i31 fails cast", body: []byte{0x41, 0x00, 0xfb, 0x1c, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}, want: "cast failure"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructScalarBodyModuleARM64(structType, wasm.I32, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if _, err := instance.Invoke("run"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("run error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestGCFinalCastStructScalarGetAcrossCollectionARM64(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructI32GetBytesARM64())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
		Profile:              GCProfileThroughput,
		StressNurseryBytes:   64,
		CollectEveryAlloc:    true,
		ForceMajorEveryMinor: true,
		VerifyAfterCollect:   true,
		ThroughputHeapBytes:  4096,
		ThroughputPageBytes:  4096,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 100 {
		if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("run = %v, %v; want [42]", got, err)
		}
	}
}

func BenchmarkGCFinalCastStructScalarGetARM64(b *testing.B) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastStructI32GetBytesARM64())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 20}})
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
}

func gcFinalCastArrayLenModuleARM64(body []byte) []byte {
	structType := []byte{0x5f, 0x00}
	arrayType := []byte{0x5e, 0x7f, 0x01}
	types := wasmtest.Section(1, wasmtest.Vec(structType, arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))
	funcs := wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2)))
	globals := wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.RefVal(wasm.AbsRef(wasm.HeapAny)), true, []byte{0xd0, 0x6e, 0x0b})))
	exports := wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0)))
	return wasmtest.Module(types, funcs, globals, exports, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func gcFinalCastArrayLenBytesARM64() []byte {
	return gcFinalCastArrayLenModuleARM64([]byte{
		0x41, 0x03,
		0xfb, 0x07, 0x01, // array.new_default type 1
		0x24, 0x00, // global.set 0: erase constructor provenance
		0x23, 0x00, // global.get 0
		0xfb, 0x16, 0x01, // ref.cast (ref type 1)
		0xfb, 0x0f, // array.len
		0x0b,
	})
}

func TestGCFinalCastArrayLenARM64(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastArrayLenBytesARM64())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || len(got) != 1 || got[0] != 3 {
		t.Fatalf("run = %v, %v; want [3]", got, err)
	}
}

func TestGCFinalCastArrayLenTrapOrderARM64(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want string
	}{
		{name: "nullable null reaches length", body: []byte{0xd0, 0x01, 0xfb, 0x17, 0x01, 0xfb, 0x0f, 0x0b}, want: "null reference"},
		{name: "nonnullable null fails cast", body: []byte{0xd0, 0x01, 0xfb, 0x16, 0x01, 0xfb, 0x0f, 0x0b}, want: "cast failure"},
		{name: "i31 fails cast", body: []byte{0x41, 0x00, 0xfb, 0x1c, 0xfb, 0x16, 0x01, 0xfb, 0x0f, 0x0b}, want: "cast failure"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastArrayLenModuleARM64(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if _, err := instance.Invoke("run"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("run error = %v, want %q", err, tc.want)
			}
		})
	}
}

func BenchmarkGCFinalCastArrayLenARM64(b *testing.B) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFinalCastArrayLenBytesARM64())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 20}})
	if err != nil {
		b.Fatal(err)
	}
	defer instance.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 3 {
			b.Fatalf("run = %v, %v; want [3]", got, err)
		}
	}
}
