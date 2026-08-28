package railssa

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func collectorRootPlanModule(t *testing.T) *wasm.Module {
	t.Helper()
	tickImport := append(wasmtest.Name("env"), wasmtest.Name("tick")...)
	tickImport = append(tickImport, 0)
	tickImport = append(tickImport, wasmtest.ULEB(0)...)
	consumeImport := append(wasmtest.Name("env"), wasmtest.Name("consume")...)
	consumeImport = append(consumeImport, 0)
	consumeImport = append(consumeImport, wasmtest.ULEB(2)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef, wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(tickImport, consumeImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x10, 0x00, // call tick: both parameters remain live.
			0x20, 0x01,
			0x10, 0x01, // consume local 1; local 0 remains live.
			0x10, 0x00, // call tick: only the selected identity remains live.
			0x20, 0x00,
			0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRootPlanTracksOnlyLiveCollectorValuesAndReusesSlots(t *testing.T) {
	m := collectorRootPlanModule(t)
	f, cfg, flow, semantic := buildSemanticTest(t, m)
	metadata, err := BuildMetadata(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildRootPlan(m, f, cfg, flow, semantic, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SlotCount != 2 || len(plan.Sites) != 3 || plan.Sites[0].Count != 2 || plan.Sites[1].Count != 2 || plan.Sites[2].Count != 1 {
		t.Fatalf("root plan = %#v roots=%#v", plan, plan.Roots)
	}
	last := plan.Roots[plan.Sites[2].Start]
	if last.Value == 0 || int(last.Value) >= len(flow.Values) || flow.Values[last.Value].Type != wasm.AnyRef {
		t.Fatalf("last safepoint root = %#v", last)
	}
	corrupt := *plan
	corrupt.Roots = append([]RootUse(nil), plan.Roots...)
	corrupt.Roots[0].Slot = 7
	if err := VerifyRootPlan(m, f, cfg, flow, semantic, metadata, &corrupt); err == nil {
		t.Fatal("corrupted root slot accepted")
	}
}

func TestRootPlanExcludesNonCollectorReferences(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}, []byte{0x20, 0x00, 0x0b})
	f, cfg, flow, semantic := buildSemanticTest(t, m)
	metadata, err := BuildMetadata(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildRootPlan(m, f, cfg, flow, semantic, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SlotCount != 0 || len(plan.Sites) != 0 || len(plan.Roots) != 0 {
		t.Fatalf("funcref root plan = %#v", plan)
	}
}
