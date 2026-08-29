//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeMixedReservationCastModule() []byte {
	packedArrayType := []byte{0x5e, 0x78, 0x01} // type 0: (array (mut i8))
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x7f, 0x00})...) // type 1: (struct (field i32))
	nativeArrayType := []byte{0x5e, 0x7f, 0x01}                          // type 2: (array (mut i32))
	funcType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{
		0x03,
		0x01, 0x63, 0x02, // local 0: (ref null type 2)
		0x01, 0x63, 0x01, // local 1: (ref null type 1)
		0x01, 0x63, 0x00, // local 2: (ref null type 0)
	}
	for range 9 {
		body = append(body, 0x41, 0x01, 0xfb, 0x07, 0x02, 0x21, 0x00)
	}
	body = append(body,
		0x41, 0x07, 0xfb, 0x00, 0x01, 0x21, 0x01, // native struct above the array chunk
		0x41, 0x01, 0xfb, 0x07, 0x02, 0x21, 0x00, // native array inside the chunk
		0x41,
	)
	body = append(body, wasmtest.SLEB32(5000)...)
	body = append(body,
		0xfb, 0x07, 0x00, 0x21, 0x02, // helper-only packed array cancels the native batch
		0x20, 0x01, 0xfb, 0x16, 0x01, 0xd1, 0x0b, // live struct must still cast as type 1
	)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(packedArrayType, structType, nativeArrayType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
}

func TestGCNativeMixedReservationPreservesStructAcrossFallbackCollection(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "helper", true: "native"}[native], func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-alloc", native), gcNativeMixedReservationCastModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 8192, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("run"); err != nil || !reflect.DeepEqual(got, []uint64{0}) {
				t.Fatalf("run = %v, %v; want [0]", got, err)
			}
			if native && in.gc.Stats().MinorCollections == 0 {
				t.Fatal("native fallback did not collect after preserving the high live extent")
			}
		})
	}
}
