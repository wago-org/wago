//go:build amd64

package dragline

import (
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestAMD64RealizesEFLAGSPhysicalRename(t *testing.T) {
	locals := append(wasmtest.ULEB(2), byte(0x7f))
	body := append(wasmtest.Vec(locals), []byte{
		0x20, 0x00, 0x20, 0x01, 0x48, // compare
		0x21, 0x02,
		0x41, 0x07, 0x21, 0x03, // retained MOV constant
		0x20, 0x02, 0x04, 0x7f,
		0x41, 0x01, 0x05, 0x41, 0x00, 0x0b,
		0x20, 0x03, 0x6a, 0x0b,
	}...)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var stackScratch railssa.StackFunc
	fn, err := buildCompilerFunc(m, 0, &stackScratch)
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	schedule := *plan.Schedule
	schedule.Order = append([]uint32(nil), plan.Schedule.Order...)
	if len(schedule.Order) < 3 || schedule.Order[0] != 1 || schedule.Order[1] != 0 || schedule.Order[2] != 2 {
		t.Fatalf("unexpected source-stable compare schedule: %#v", schedule.Order)
	}
	schedule.Order[0], schedule.Order[1] = schedule.Order[1], schedule.Order[0]
	allocation, err := railmach.AllocateGreedyPForSchedule(plan.Machine, &schedule, railmach.DefaultGreedyConfig(railmach.TargetAMD64), nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := railmach.LateSSAExit(plan.Machine, &allocation.Allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	postRA, err := railmach.PlanPostRA(railmach.TargetAMD64, plan.Machine, plan.Selection, &schedule, allocation, exit, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	physical := postRA.Rewrites[:0]
	for _, rewrite := range postRA.Rewrites {
		if rewrite.Kind == railmach.RewritePhysicalRename && rewrite.First == 0 && rewrite.Second == 2 {
			found = true
			physical = append(physical, rewrite)
		}
	}
	if !found {
		t.Fatalf("forced post-RA rewrites = %#v", postRA.Rewrites)
	}
	postRA.Rewrites = physical
	if err := railmach.VerifyPostRAPlan(railmach.TargetAMD64, plan.Machine, plan.Selection, &schedule, postRA); err != nil {
		t.Fatal(err)
	}
	forced := *plan
	forced.Schedule, forced.Allocation, forced.Exit, forced.PostRA = &schedule, allocation, exit, postRA
	forced.PostRAFusionWith = make([]uint32, len(plan.Machine.Insts))
	forced.PostRAFusionWith[0], forced.PostRAFusionWith[2] = 3, 1
	var relocs []amd64CallReloc
	var metrics FunctionMetrics
	optimized, _, ok, err := emitAMD64RailMach(fn, &forced, &relocs, &metrics, nil)
	if err != nil || !ok {
		t.Fatalf("optimized EFLAGS finalization = ok %t, err %v", ok, err)
	}
	baseline := forced
	clearPostRAEmissionRewrites(&baseline)
	relocs = relocs[:0]
	checked, _, ok, err := emitAMD64RailMach(fn, &baseline, &relocs, nil, nil)
	if err != nil || !ok {
		t.Fatalf("baseline EFLAGS finalization = ok %t, err %v", ok, err)
	}
	if metrics.PostRARewrites != 1 || len(optimized) >= len(checked) {
		t.Fatalf("EFLAGS realization = rewrites %d optimized %d baseline %d", metrics.PostRARewrites, len(optimized), len(checked))
	}
}
