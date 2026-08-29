//go:build arm64

package dragline

import (
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
)

func TestARM64ProductionConsumesProvedBoundsElision(t *testing.T) {
	fn, plan := constantMemoryEmissionTestFunc(t)
	optimized, _, _, err := emitARM64(fn, plan, nil, corecompiler.Target{}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, _, err := emitARM64(fn, nil, nil, corecompiler.Target{}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized) >= len(checked) {
		t.Fatalf("optimized bytes=%d checked bytes=%d", len(optimized), len(checked))
	}
}

func TestARM64ProductionConsumesMaskedRangeBoundsElision(t *testing.T) {
	fn, plan := maskedMemoryEmissionTestFunc(t)
	optimized, _, _, err := emitARM64(fn, plan, nil, corecompiler.Target{}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, _, err := emitARM64(fn, nil, nil, corecompiler.Target{}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized) >= len(checked) {
		t.Fatalf("optimized bytes=%d checked bytes=%d", len(optimized), len(checked))
	}
}

func TestARM64ProductionConsumesMaskedInductionBoundsElision(t *testing.T) {
	fn, plan := maskedLoopMemoryEmissionTestFunc(t)
	optimized, _, _, err := emitARM64(fn, plan, nil, corecompiler.Target{}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, _, err := emitARM64(fn, nil, nil, corecompiler.Target{}, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized) >= len(checked) {
		t.Fatalf("optimized bytes=%d checked bytes=%d", len(optimized), len(checked))
	}
}

func TestARM64RailMachConsumesMaskedInductionBoundsElision(t *testing.T) {
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
	checked, _, used, err := emitARM64RailMach(fn, &checkedPlan, false, nil, nil, nil, &checkedMetadata)
	if err != nil || !used {
		t.Fatalf("checked RailMach emission: used=%v err=%v", used, err)
	}
	optimized, _, used, err := emitARM64RailMach(fn, optimizedPlan, false, nil, nil, nil, &optimizedMetadata)
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
