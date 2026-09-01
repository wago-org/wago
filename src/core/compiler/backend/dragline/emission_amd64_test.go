//go:build amd64

package dragline

import (
	"bytes"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
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
		t.Fatal("dense-global leaf was admitted")
	}
	stack.Instrs = []railssa.StackInstr{{Kind: wasm.InstrGlobalGet}, {Kind: wasm.InstrCall}}
	if amd64RailMachCandidate(stack, false, true) {
		t.Fatal("acyclic dense-global call helper was admitted")
	}
	stack.MaxLoopDepth = 1
	if amd64RailMachCandidate(stack, false, true) {
		t.Fatal("cyclic dense-global call helper was admitted")
	}
	stack.MaxLoopDepth = 0
	stack.Instrs = make([]railssa.StackInstr, 1025)
	if amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large parameterless function was admitted")
	}
	stack.Params = []wasm.ValType{wasm.I32}
	if !amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large parameterized scalar candidate was rejected")
	}
	stack.Instrs[0].Kind = wasm.InstrMemoryCopy
	if amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large memory.copy function was admitted")
	}
}

func TestAMD64RailMachAdmissionKeepsRecursiveI64LoopStructured(t *testing.T) {
	stack := &railssa.StackFunc{
		MaxLoopDepth: 1,
		Results:      []wasm.ValType{wasm.I64},
		Instrs:       []railssa.StackInstr{{Kind: wasm.InstrCall}},
	}
	if amd64RailMachCandidate(stack, false, false) {
		t.Fatal("recursive i64 loop was admitted")
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

func TestAMD64RailMachFinalizesSaturatingConversion(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0xfc, 0x02, // i32.trunc_sat_f64_s
			0x0b,
		}))),
	)
	assertAMD64RailMachFinalized(t, source)
}

func TestAMD64RailMachFinalizesSaturatingConversionWithLiveScratch(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.F64, wasm.F64, wasm.F64}, []wasm.ValType{wasm.I32},
		))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // keep an i32 live across the float expression
			0x20, 0x01, // keep two f64 values live across the first conversion
			0x20, 0x02,
			0x20, 0x03,
			0xfc, 0x02, // i32.trunc_sat_f64_s
			0xb7,       // f64.convert_i32_s
			0xa0,       // f64.add
			0xa0,       // f64.add
			0xfc, 0x02, // i32.trunc_sat_f64_s
			0x6a, // i32.add
			0x0b,
		}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	fn, err := buildCompilerFunc(module, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&nativeBackendPlanner{}).Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	liveScratch := false
	for instructionID, instruction := range plan.Machine.Insts {
		if instruction.Op >= wasm.InstrI32TruncSatF32S && instruction.Op <= wasm.InstrI64TruncSatF64U &&
			(railMachPhysicalLiveAcross(plan, uint32(instructionID), railmach.BankGPR, 0) ||
				railMachPhysicalLiveAcross(plan, uint32(instructionID), railmach.BankFPR, 1)) {
			liveScratch = true
			break
		}
	}
	if !liveScratch {
		t.Fatal("test did not keep a saturating-conversion scratch register live")
	}
	assertAMD64RailMachFinalized(t, source)
}

func TestAMD64RailMachFinalizesBulkMemory(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(append([]byte{0x00}, wasmtest.ULEB(1)...))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0: destination
			0x20, 0x01, // local.get 1: source
			0x20, 0x02, // local.get 2: length
			0xfc, 0x0a, 0x00, 0x00, // memory.copy 0 0
			0x0b,
		}))),
	)
	assertAMD64RailMachFinalized(t, source)
}

func assertAMD64RailMachFinalized(t *testing.T, source []byte) {
	t.Helper()
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
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
		t.Fatalf("RailMach metrics = %#v", metrics.Functions)
	}
}
