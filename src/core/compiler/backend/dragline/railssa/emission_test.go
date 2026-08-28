package railssa

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func emissionMemoryModule(t *testing.T, body []byte) *wasm.Module {
	t.Helper()
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, memorySec, codeSec))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestEmissionPlannerPublishesVerifiedConstantBounds(t *testing.T) {
	m := emissionMemoryModule(t, []byte{0x41, 0x00, 0x28, 0x02, 0x00, 0x0b})
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !NeedsEmissionPlan(f) {
		t.Fatal("memory function did not request emission planning")
	}
	var planner EmissionPlanner
	plan, err := planner.Plan(f)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ElidesBoundsCheck(1) || plan.ElidesBoundsCheck(0) || plan.SemanticInsts != 2 {
		t.Fatalf("plan = %#v", plan)
	}
	if planner.CapacityBytes() == 0 {
		t.Fatal("planner retained scratch is not measured")
	}
	plan.boundsChecksElided[1] = false
	if err := VerifyEmissionPlan(f, &planner.semantic, &planner.metadata, &planner.simplified, plan); err != nil {
		t.Fatalf("retaining a proved check must remain valid: %v", err)
	}
}

func TestEmissionPlannerUsesCompactMaskedLoopProof(t *testing.T) {
	f, err := BuildStackFunc(maskedInductionModule(t, 8, 65535), 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner EmissionPlanner
	plan, err := planner.Plan(f)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ElidedBoundsChecks() != 1 || plan.SemanticInsts != 0 || len(planner.cfg.Blocks) != 0 {
		t.Fatalf("compact masked-loop plan=%#v cfg-blocks=%d", plan, len(planner.cfg.Blocks))
	}
	var planErr error
	allocs := testing.AllocsPerRun(10, func() {
		_, planErr = planner.Plan(f)
	})
	if planErr != nil {
		t.Fatal(planErr)
	}
	if allocs != 0 {
		t.Fatalf("warm compact loop planning allocations = %g, want 0", allocs)
	}
}

func TestEmissionPlannerRejectsPreinitializedMaskedLoopLocal(t *testing.T) {
	prefix := []byte{0x41, 0x07, 0x21, 0x01}
	f, err := BuildStackFunc(maskedInductionModuleWithPrefix(t, 8, 65535, prefix), 0)
	if err != nil {
		t.Fatal(err)
	}
	var planner EmissionPlanner
	plan, err := planner.Plan(f)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ElidedBoundsChecks() != 0 {
		t.Fatalf("preinitialized masked loop elisions = %d", plan.ElidedBoundsChecks())
	}
}

func TestEmissionPlannerDoesNotElideUnknownAddress(t *testing.T) {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b})))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, memorySec, codeSec))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if NeedsEmissionPlan(f) {
		t.Fatal("dynamic address requested an unusable production emission plan")
	}
	var planner EmissionPlanner
	plan, err := planner.Plan(f)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ElidesBoundsCheck(1) {
		t.Fatal("unknown address bounds check was elided")
	}
	plan.boundsChecksElided[1] = true
	if err := VerifyEmissionPlan(f, &planner.semantic, &planner.metadata, &planner.simplified, plan); err == nil {
		t.Fatal("unproved bounds elision was accepted")
	}
}

func TestEmissionPlannerSkipsNonMemoryFunction(t *testing.T) {
	m := scalarModule(nil, []wasm.ValType{wasm.I32}, []byte{0x41, 0x01, 0x0b})
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if NeedsEmissionPlan(f) {
		t.Fatal("non-memory function requested emission planning")
	}
}

func TestEmissionPlannerPublishesVerifiedConstantStoreBounds(t *testing.T) {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil)))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x41, 0x07, 0x36, 0x02, 0x00, 0x0b})))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, memorySec, codeSec))
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !NeedsEmissionPlan(f) {
		t.Fatal("directly constant store did not request emission planning")
	}
	var planner EmissionPlanner
	plan, err := planner.Plan(f)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.ElidesBoundsCheck(2) || plan.ElidedBoundsChecks() != 1 || plan.ProofQueries != 1 {
		t.Fatalf("plan = %#v", plan)
	}
}
