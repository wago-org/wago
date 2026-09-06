//go:build linux && amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeOldArrayReferenceStoreBytes() []byte {
	return gcNativeOldArrayReferenceStoreFixture(3, 1)
}

func gcNativeOldArrayReferenceStoreFixture(length, secondIndex uint32) []byte {
	structType := []byte{0x5f}
	structType = append(structType, wasmtest.Vec([]byte{0x6d, 0x01})...) // (struct (field (mut eqref)))
	arrayType := []byte{0x5e, 0x6d, 0x01}                                // (array (mut eqref))
	voidType := wasmtest.FuncType(nil, nil)
	arrayGlobal := []byte{0x63, 0x01, 0x01, 0xd0, 0x01, 0x0b} // (mut (ref null 1)) = null
	childGlobal := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b} // (mut (ref null 0)) = null

	initBody := []byte{0x00, 0x41}
	initBody = append(initBody, wasmtest.SLEB32(int32(length))...)
	initBody = append(initBody,
		0xfb, 0x07, 0x01, 0x24, 0x00,
		0xd0, 0x6d, 0xfb, 0x00, 0x00, 0x24, 0x01,
		0xd0, 0x6d, 0xfb, 0x00, 0x00, 0x24, 0x02,
		0x0b,
	)
	setBothBody := []byte{0x00,
		0x23, 0x00, 0x41, 0x00, 0x23, 0x01, 0xfb, 0x0e, 0x01,
		0x23, 0x00, 0x41,
	}
	setBothBody = append(setBothBody, wasmtest.SLEB32(int32(secondIndex))...)
	setBothBody = append(setBothBody, 0x23, 0x02, 0xfb, 0x0e, 0x01, 0x0b)
	clearChildrenBody := []byte{0x00,
		0xd0, 0x00, 0x24, 0x01,
		0xd0, 0x00, 0x24, 0x02,
		0x0b,
	}
	setFirstBody := []byte{0x00,
		0x23, 0x00, 0x41, 0x00, 0x23, 0x01, 0xfb, 0x0e, 0x01,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, arrayType, voidType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(arrayGlobal, childGlobal, childGlobal)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("set_both", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("clear_children", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("set_first", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(initBody))), initBody...),
			append(wasmtest.ULEB(uint32(len(setBothBody))), setBothBody...),
			append(wasmtest.ULEB(uint32(len(clearChildrenBody))), clearChildrenBody...),
			append(wasmtest.ULEB(uint32(len(setFirstBody))), setFirstBody...),
		)),
	)
}

func TestGCNativeOldArrayDistantStoresRetainSeparateCards(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldArrayReferenceStoreFixture(130, 129))
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
	array := corergc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	if err := in.gc.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("set_both"); err != nil {
		t.Fatal(err)
	}
	// Three checked global roots remain dirty from init; the two distant array
	// stores add two independent object-card ranges.
	if got := in.gc.CardCount(); got != 5 {
		t.Fatalf("distant native stores total cards=%d, want 3 roots + 2 object cards", got)
	}
	if err := in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	value, err := in.gc.ArrayGet(array, 129)
	if err != nil || !value.Ref.IsObj() {
		t.Fatalf("distant stored child after minor collection = %+v, %v", value, err)
	}
}

func TestGCNativeOldArrayReferenceStorePreservesBarrier(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldArrayReferenceStoreBytes())
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
	array := corergc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	if err := in.gc.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("set_both"); err != nil {
		t.Fatal(err)
	}
	if err := in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	value, err := in.gc.ArrayGet(array, 1)
	if err != nil || !value.Ref.IsObj() {
		t.Fatalf("stored array child after minor collection = %+v, %v", value, err)
	}
}
