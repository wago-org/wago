//go:build wago_guardpage && linux && amd64 && !tinygo

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcGuardPageFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7b, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128})
	vec := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	body := append([]byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x7f, 0xfd, 0x0c}, vec...)
	body = append(body,
		0xfb, 0x00, 0x00, 0x21, 0x01,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x02, 0x20, 0x00, 0x4f, 0x0d, 0x01,
		0xfd, 0x0c,
	)
	body = append(body, vec...)
	body = append(body,
		0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x02, 0x41, 0x01, 0x6a, 0x21, 0x02, 0x0c, 0x00,
		0x0b, 0x0b,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCGuardPageNativeFrameRoots(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGuardPageFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.boundsMode != BoundsChecksSignalsBased || compiled.genericGCFrameRoots() == nil {
		t.Fatalf("guard GC admission = mode %v roots %+v", compiled.boundsMode, compiled.genericGCFrameRoots())
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("run", 1000)
	want := []uint64{0x0706050403020100, 0x0f0e0d0c0b0a0908}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("run = %v, %v; want %v", got, err, want)
	}
}
