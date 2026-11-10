//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCRepeatedHelperTransitions(t *testing.T) {
	data := gcDeadNewModule([][]byte{
		{0x5f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}, 1, []byte{
		0xfb, 0x01, 0x00, // struct.new_default 0
		0xd1, // ref.is_null
		0x0b,
	})
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	for want := uint64(1); want <= 2; want++ {
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("run %d = %v, %v", want, got, err)
		}
	}
}

func TestGCHelperWorkloadBatchedNativeStructAllocation(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), v128StructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	fn, err := instance.WasmFunc("new_get")
	if err != nil {
		t.Fatal(err)
	}

	for i := uint64(0); i < 65; i++ {
		got, err := fn.Invoke(i, ^i)
		if err != nil || len(got) != 2 || got[0] != i || got[1] != ^i {
			t.Fatalf("constructor %d = %#x, %v", i, got, err)
		}
	}
}

func TestGCHelperWorkloadOldStructBarrierFallback(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldStructReferenceStoreBytes())
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
	parent := corergc.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	if err := instance.gc.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}

	if _, err := instance.Invoke("set_self"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Invoke("set_i31"); err != nil {
		t.Fatal(err)
	}

	if _, err := instance.Invoke("set_child"); err != nil {
		t.Fatal(err)
	}

	if _, err := instance.Invoke("set_child"); err != nil {
		t.Fatal(err)
	}
}

func TestGCHelperWorkloadDistantArrayCardFallbacks(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldArrayReferenceStoreFixture(130, 129))
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
	array := corergc.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	if err := instance.gc.ForcePromote(array); err != nil {
		t.Fatal(err)
	}

	if _, err := instance.Invoke("set_both"); err != nil {
		t.Fatal(err)
	}

	if _, err := instance.Invoke("set_first"); err != nil {
		t.Fatal(err)
	}

	if _, err := instance.Invoke("set_first"); err != nil {
		t.Fatal(err)
	}
}

func TestGCHelperWorkloadOldArrayCardFallback(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldArrayReferenceStoreBytes())
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
	array := corergc.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	if err := instance.gc.ForcePromote(array); err != nil {
		t.Fatal(err)
	}

	if _, err := instance.Invoke("set_both"); err != nil {
		t.Fatal(err)
	}
}
