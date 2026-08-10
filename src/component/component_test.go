package component_test

import (
	"context"
	_ "embed"
	"testing"

	"github.com/wago-org/wago/src/component"
	"github.com/wago-org/wago/src/wago"
)

// This fixture is a genuine Component Model binary, not a core Wasm module.
// It exercises nested instance exports and Canonical ABI lift/lower on Wago.
//
//go:embed testdata/adder.wasm
var adderWasm []byte

func TestInstantiateAdder(t *testing.T) {
	ctx := context.Background()
	r := wago.NewRuntime()
	defer r.Close()

	inst, err := component.Instantiate(ctx, r, adderWasm)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close(ctx)

	got, err := inst.CallExport(ctx, "component:adder/calc", "add", uint32(2), uint32(3))
	if err != nil {
		t.Fatalf("CallExport add: %v", err)
	}
	if len(got) != 1 || got[0] != uint32(5) {
		t.Fatalf("add(2, 3) = %#v, want [5]", got)
	}
}

func TestCompileCache(t *testing.T) {
	ctx := context.Background()
	r := wago.NewRuntime()
	defer r.Close()
	cache := component.NewCompileCache()
	defer cache.Close(ctx)

	for i := 0; i < 2; i++ {
		inst, err := component.Instantiate(ctx, r, adderWasm, component.WithCompileCache(cache))
		if err != nil {
			t.Fatalf("Instantiate #%d: %v", i, err)
		}
		got, err := inst.CallExport(ctx, "component:adder/calc", "add", uint32(10), uint32(20))
		if err != nil {
			t.Fatalf("Call #%d: %v", i, err)
		}
		if len(got) != 1 || got[0] != uint32(30) {
			t.Fatalf("add(10, 20) = %#v, want [30]", got)
		}
		if err := inst.Close(ctx); err != nil {
			t.Fatalf("Close #%d: %v", i, err)
		}
	}
}
