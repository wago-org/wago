//go:build linux && amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeOldStructReferenceStoreBytes() []byte {
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x6d, 0x01})...) // (struct (field (mut eqref)))
	voidType := wasmtest.FuncType(nil, nil)
	i32Type := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	global := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b} // (mut (ref null 0)) = null

	initBody := []byte{0x00,
		0xd0, 0x6d, 0xfb, 0x00, 0x00, 0x24, 0x00, // global = struct.new 0 (ref.null eq)
		0x0b,
	}
	setI31Body := []byte{0x00,
		0x23, 0x00, 0x41, 0x07, 0xfb, 0x1c, 0xfb, 0x05, 0x00, 0x00,
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x41, 0x07, 0xfb, 0x1c, 0xd3,
		0x0b,
	}
	setChildBody := []byte{0x00,
		0x23, 0x00, 0xd0, 0x6d, 0xfb, 0x00, 0x00, 0xfb, 0x05, 0x00, 0x00,
		0x0b,
	}
	setSelfBody := []byte{0x00,
		0x23, 0x00, 0x23, 0x00, 0xfb, 0x05, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, voidType, i32Type)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("set_i31", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("set_child", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("set_self", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			append(wasmtest.ULEB(uint32(len(setI31Body))), setI31Body...),
			append(wasmtest.ULEB(uint32(len(setChildBody))), setChildBody...),
			append(wasmtest.ULEB(uint32(len(setSelfBody))), setSelfBody...),
		)),
	)
}

func TestGCNativeOldStructReferenceStorePreservesBarrier(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldStructReferenceStoreBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressBarriers: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	parent := corergc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	if err := in.gc.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("set_self"); err != nil {
		t.Fatal(err)
	}
	self, err := in.gc.StructGet(parent, 0)
	if err != nil || self.Ref != parent {
		t.Fatalf("old-to-old self store = %+v, %v; want %v", self, err, parent)
	}
	if got, err := in.Invoke("set_i31"); err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("set_i31 = %v, %v; want [1]", got, err)
	}
	if got := in.gc.RememberedCount(); got != 0 {
		t.Fatalf("immediate old-parent store remembered count = %d, want 0", got)
	}
	if _, err := in.Invoke("set_child"); err != nil {
		t.Fatal(err)
	}
	if got := in.gc.RememberedCount(); got != 1 {
		t.Fatalf("first old-to-young store remembered count = %d, want 1", got)
	}
	if _, err := in.Invoke("set_child"); err != nil {
		t.Fatal(err)
	}
	if err := in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	value, err := in.gc.StructGet(parent, 0)
	if err != nil || !value.Ref.IsObj() {
		t.Fatalf("stored child after minor collection = %+v, %v", value, err)
	}
}
