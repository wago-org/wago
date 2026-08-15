//go:build (linux || darwin) && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

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
