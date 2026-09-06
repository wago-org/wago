//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func v128StructModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7b, 0x01} // (struct (field (mut v128)))
	refLocal := []byte{0x01, 0x01, 0x63, 0x00}
	newGet := []byte{
		0x20, 0x00, 0xfb, 0x00, 0x00, // struct.new 0
		0xfb, 0x02, 0x00, 0x00, 0x0b, // struct.get 0 0
	}
	setGet := []byte{
		0xfb, 0x01, 0x00, 0x21, 0x01, // struct.new_default 0 -> local 1
		0x20, 0x01, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x00,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b,
	}
	getDefault := []byte{
		0xfb, 0x01, 0x00,
		0xfb, 0x02, 0x00, 0x00, 0x0b,
	}
	vecBytes := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	vecGlobal := append([]byte{0x7b, 0x00, 0xfd, 0x0c}, vecBytes...)
	vecGlobal = append(vecGlobal, 0x0b)
	structGlobal := []byte{0x64, 0x00, 0x00, 0x23, 0x00, 0xfb, 0x00, 0x00, 0x0b}
	getGlobal := []byte{0x23, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(vecGlobal, structGlobal)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("new_get", 0, 0),
			wasmtest.ExportEntry("set_get", 0, 1),
			wasmtest.ExportEntry("default", 0, 2),
			wasmtest.ExportEntry("global", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(newGet),
			v128ArrayCodeWithLocals(refLocal, setGet),
			wasmtest.Code(getDefault),
			wasmtest.Code(getGlobal),
		)),
	)
}

func TestGCStructV128HelpersPreserveBothSlots(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	c, err := Compile(cfg, v128StructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.GCTypeDescs) == 0 || len(c.GCTypeDescs[0].Fields) != 1 || c.GCTypeDescs[0].Fields[0].Kind != gc.StorageV128 || c.GCTypeDescs[0].Align != 16 {
		t.Fatalf("v128 struct descriptor = %+v", c.GCTypeDescs)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	lo, hi := uint64(0x0706050403020100), uint64(0x0f0e0d0c0b0a0908)
	for _, name := range []string{"new_get", "set_get"} {
		got, err := in.Invoke(name, lo, hi)
		if err != nil || !reflect.DeepEqual(got, []uint64{lo, hi}) {
			t.Fatalf("%s = %#x, %v", name, got, err)
		}
	}
	if got, err := in.Invoke("default"); err != nil || !reflect.DeepEqual(got, []uint64{0, 0}) {
		t.Fatalf("default = %#x, %v", got, err)
	}
	if got, err := in.Invoke("global"); err != nil || !reflect.DeepEqual(got, []uint64{lo, hi}) {
		t.Fatalf("global = %#x, %v", got, err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		got, err := in.Invoke("set_get", lo, hi)
		if err != nil || len(got) != 2 || got[0] != lo || got[1] != hi {
			panic("v128 struct helper failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("v128 struct helper allocations = %v, want 0", allocs)
	}
}
