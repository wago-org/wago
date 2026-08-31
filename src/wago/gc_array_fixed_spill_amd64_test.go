//go:build linux && amd64 && !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcWideFixedArrayModule(count uint32) []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01} // array (mut i32)
	body := make([]byte, 0, int(count)*3+16)
	for i := uint32(0); i < count; i++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(i))...)
	}
	body = append(body, 0xfb, 0x08, 0x00)
	body = append(body, wasmtest.ULEB(count)...)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(int32(count-1))...)
	body = append(body, 0xfb, 0x0b, 0x00, 0x0b) // array.get 0; end
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("last", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCArrayNewFixedSpillsBeyondOldCapacity(t *testing.T) {
	const count = 100
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit), gcWideFixedArrayModule(count))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got := invokeOne(t, in, "last"); got != count-1 {
		t.Fatalf("array.new_fixed last element = %d, want %d", got, count-1)
	}
}
