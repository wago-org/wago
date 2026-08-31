//go:build amd64

package dragline

import (
	"bytes"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestAMD64ShuffleMasksSelectExactlyOneInput(t *testing.T) {
	lanes := [16]byte{0, 16, 1, 17, 2, 18, 3, 19, 4, 20, 5, 21, 6, 22, 15, 31}
	left, right := amd64ShuffleMasks(lanes)
	wantLeft := [16]byte{0, 0x80, 1, 0x80, 2, 0x80, 3, 0x80, 4, 0x80, 5, 0x80, 6, 0x80, 15, 0x80}
	wantRight := [16]byte{0x80, 0, 0x80, 1, 0x80, 2, 0x80, 3, 0x80, 4, 0x80, 5, 0x80, 6, 0x80, 15}
	if !bytes.Equal(left[:], wantLeft[:]) || !bytes.Equal(right[:], wantRight[:]) {
		t.Fatalf("shuffle masks = %x / %x, want %x / %x", left, right, wantLeft, wantRight)
	}
}

func TestAMD64RailMachAdmissionKeepsUnprovedModuleShapesStructured(t *testing.T) {
	stack := &railssa.StackFunc{HasReferences: true}
	if !amd64RailMachCandidate(stack, false, false) {
		t.Fatal("ordinary scalar candidate was rejected")
	}
	if amd64RailMachCandidate(stack, false, true) {
		t.Fatal("dense-global module was admitted")
	}
	stack.Instrs = make([]railssa.StackInstr, 1025)
	if amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large parameterless function was admitted")
	}
	stack.Params = []wasm.ValType{wasm.I32}
	if !amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large parameterized scalar candidate was rejected")
	}
}

func TestAMD64ProductionConsumesProvedBoundsElision(t *testing.T) {
	fn, plan := constantMemoryEmissionTestFunc(t)
	optimized, _, _, _, err := emitAMD64(fn, plan, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, _, _, err := emitAMD64(fn, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized) >= len(checked) {
		t.Fatalf("optimized bytes=%d checked bytes=%d", len(optimized), len(checked))
	}
}

func TestAMD64ProductionConsumesMaskedRangeBoundsElision(t *testing.T) {
	fn, plan := maskedMemoryEmissionTestFunc(t)
	optimized, _, _, _, err := emitAMD64(fn, plan, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, _, _, err := emitAMD64(fn, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized) >= len(checked) {
		t.Fatalf("optimized bytes=%d checked bytes=%d", len(optimized), len(checked))
	}
}

func TestAMD64ProductionConsumesMaskedInductionBoundsElision(t *testing.T) {
	fn, plan := maskedLoopMemoryEmissionTestFunc(t)
	optimized, _, _, _, err := emitAMD64(fn, plan, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, _, _, err := emitAMD64(fn, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized) >= len(checked) {
		t.Fatalf("optimized bytes=%d checked bytes=%d", len(optimized), len(checked))
	}
}

func TestAMD64RailMachConsumesMaskedInductionBoundsElision(t *testing.T) {
	fn, _ := maskedLoopMemoryEmissionTestFunc(t)
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	optimizedPlan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	if optimizedPlan.Emission == nil || optimizedPlan.Emission.ElidedBoundsChecks() != 1 {
		t.Fatalf("RailMach emission plan = %#v", optimizedPlan.Emission)
	}
	checkedPlan := *optimizedPlan
	checkedPlan.Emission = nil
	var checkedMetadata, optimizedMetadata functionEmissionMetadata
	checked, _, used, err := emitAMD64RailMach(fn, &checkedPlan, nil, nil, &checkedMetadata)
	if err != nil || !used {
		t.Fatalf("checked RailMach emission: used=%v err=%v", used, err)
	}
	optimized, _, used, err := emitAMD64RailMach(fn, optimizedPlan, nil, nil, &optimizedMetadata)
	if err != nil || !used {
		t.Fatalf("optimized RailMach emission: used=%v err=%v", used, err)
	}
	if len(optimized) >= len(checked) {
		t.Fatalf("optimized bytes=%d checked bytes=%d", len(optimized), len(checked))
	}
	if len(optimizedMetadata.Traps) >= len(checkedMetadata.Traps) {
		t.Fatalf("optimized traps=%d checked traps=%d", len(optimizedMetadata.Traps), len(checkedMetadata.Traps))
	}
}
