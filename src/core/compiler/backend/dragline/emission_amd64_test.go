//go:build amd64

package dragline

import (
	"bytes"
	"encoding/binary"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestAMD64RailMachRotatesCanonicalCountdownLoop(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x40, // block
			0x03, 0x40, // loop
			0x20, 0x00, 0x45, 0x0d, 0x01, // break when counter == 0
			0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // counter--
			0x0c, 0x00, 0x0b, 0x0b, // continue; end loop/block
			0x20, 0x00, 0x0b,
		}))),
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
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&nativeBackendPlanner{}).Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	rotated := false
	var rotatedBlock, rotatedEdge uint32
	for edge, candidate := range plan.Machine.Edges {
		_, _, ok := amd64RailMachRotatedZeroTestLatch(plan, uint32(candidate.From), uint32(edge))
		if ok {
			rotated = true
			rotatedBlock, rotatedEdge = uint32(candidate.From), uint32(edge)
		}
	}
	if !rotated {
		t.Fatal("canonical countdown loop was not rotated")
	}
	native, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !used {
		t.Fatalf("countdown finalization = used %t, err %v", used, err)
	}
	backwardJNE := false
	for offset := 0; offset+6 <= len(native); offset++ {
		if native[offset] == 0x0f && native[offset+1] == 0x85 && int32(binary.LittleEndian.Uint32(native[offset+2:])) < 0 {
			backwardJNE = true
			break
		}
	}
	if !backwardJNE {
		t.Fatal("rotated countdown has no backward JNE")
	}
	oldCount := plan.Schedule.BlockRanges[rotatedBlock].Count
	plan.Schedule.BlockRanges[rotatedBlock].Count = 9
	if _, _, ok := amd64RailMachRotatedZeroTestLatch(plan, rotatedBlock, rotatedEdge); ok {
		t.Fatal("large countdown body was rotated")
	}
	plan.Schedule.BlockRanges[rotatedBlock].Count = oldCount
}

func TestAMD64RailMachUsesDependencyBreakingVEXFloatConversionAndSqrt(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0xb8, // f64.convert_i32_u
			0x9f, // f64.sqrt
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
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&nativeBackendPlanner{}).Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	native, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !used {
		t.Fatalf("float finalization = used %t, err %v", used, err)
	}
	if !containsAMD64VEXOpcode(native, 0x2a) || !containsAMD64VEXOpcode(native, 0x51) {
		t.Fatalf("float code lacks VEX conversion/sqrt: %x", native)
	}
	if bytes.Contains(native, []byte{0xf2, 0x48, 0x0f, 0x2a}) || bytes.Contains(native, []byte{0xf2, 0x0f, 0x51}) {
		t.Fatalf("float code retained dependency-carrying legacy conversion/sqrt: %x", native)
	}
}

func containsAMD64VEXOpcode(code []byte, opcode byte) bool {
	for offset := 0; offset+4 < len(code); offset++ {
		if code[offset] == 0xc4 && code[offset+3] == opcode {
			return true
		}
	}
	return false
}

func TestAMD64ShuffleMasksSelectExactlyOneInput(t *testing.T) {
	lanes := [16]byte{0, 16, 1, 17, 2, 18, 3, 19, 4, 20, 5, 21, 6, 22, 15, 31}
	left, right := amd64ShuffleMasks(lanes)
	wantLeft := [16]byte{0, 0x80, 1, 0x80, 2, 0x80, 3, 0x80, 4, 0x80, 5, 0x80, 6, 0x80, 15, 0x80}
	wantRight := [16]byte{0x80, 0, 0x80, 1, 0x80, 2, 0x80, 3, 0x80, 4, 0x80, 5, 0x80, 6, 0x80, 15}
	if !bytes.Equal(left[:], wantLeft[:]) || !bytes.Equal(right[:], wantRight[:]) {
		t.Fatalf("shuffle masks = %x / %x, want %x / %x", left, right, wantLeft, wantRight)
	}
}

func TestAMD64StructuredScalarResidencySelectsHotIntegerLocals(t *testing.T) {
	locals := []wasm.ValType{wasm.I32, wasm.V128, wasm.I64, wasm.F32, wasm.I32, wasm.I64, wasm.I32, wasm.I32}
	uses := []uint32{2, 100, 9, 50, 7, 6, 5, 4}
	assigned := make([]amd64.Reg, len(locals))
	pinned := make([]bool, len(locals))
	amd64PinHotStructuredScalarLocals(locals, uses, assigned, pinned)
	for local, register := range map[int]amd64.Reg{0: amd64.R9, 2: amd64.R12, 4: amd64.R13, 5: amd64.R14, 6: amd64.R15, 7: amd64.R8} {
		if !pinned[local] || assigned[local] != register {
			t.Fatalf("local %d: assigned=%v pinned=%v", local, assigned, pinned)
		}
	}
	if pinned[1] || pinned[3] {
		t.Fatalf("non-integer local pinned: %v", pinned)
	}
}

func TestAMD64RailMachSpillForwardingRequiresLastUse(t *testing.T) {
	allocation := railmach.GreedyAllocation{Allocation: railmach.Allocation{Intervals: []railmach.LiveInterval{
		{Reg: 1, Start: 3, End: 14, Bank: railmach.BankFPR},
		{Reg: 2, Start: 9, End: 30, Bank: railmach.BankFPR},
	}}}
	plan := nativeBackendPlan{Allocation: &allocation}
	if !amd64RailMachValueDiesAt(&plan, 1, 14) || amd64RailMachValueDiesAt(&plan, 2, 14) || amd64RailMachValueDiesAt(&plan, 3, 99) {
		t.Fatal("spill forwarding accepted a value that remains live")
	}
}

func TestAMD64StructuredSIMDConstantsUseDeduplicatedRIPPool(t *testing.T) {
	constant := [16]byte{1, 3, 5, 7, 9, 11, 13, 15, 2, 4, 6, 8, 10, 12, 14, 16}
	body := []byte{0xfd, 0x0c}
	body = append(body, constant[:]...)
	body = append(body, 0xfd, 0x0c)
	body = append(body, constant[:]...)
	body = append(body, 0xfd, 0x51, 0x0b) // v128.xor; end
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	if got := bytes.Count(output.Code, constant[:]); got != 1 {
		t.Fatalf("SIMD constant pool copies = %d, want 1", got)
	}
}

func TestAMD64StructuredFusesIntegerComparisonIntoControl(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x7b, // one v128 local forces structured SIMD emission
		0x20, 0x00, 0x41, 0x0a, 0x49, // local.get 0; i32.const 10; i32.lt_u
		0x04, 0x40, 0x01, 0x0b, // if; nop; end
		0x20, 0x00, 0x0b, // local.get 0; end
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	output := compileAMD64EmissionTest(t, source)
	if bytes.Contains(output.Code, []byte{0x0f, 0x92}) {
		t.Fatalf("structured comparison materialized a boolean before control: %x", output.Code)
	}
}

func TestAMD64StructuredShuffleUsesRIPMaskOperands(t *testing.T) {
	body := []byte{0x20, 0x00, 0x20, 0x01, 0xfd, 0x0d}
	for lane := byte(0); lane < 16; lane++ {
		body = append(body, lane)
	}
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	if got := countAMD64VPshufbRIP(output.Code); got != 2 {
		t.Fatalf("RIP-relative vpshufb instructions = %d, want 2", got)
	}
}

func TestAMD64StructuredCoalescesStraightLineLocalBoundsChecks(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0xfd, 0x00, 0x04, 0x00, 0x1a,
			0x20, 0x00, 0xfd, 0x00, 0x04, 0x10, 0x1a,
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
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	ends, elided := planAMD64StructuredLocalMemoryChecks(stack)
	if ends[1] != 32 || !elided[4] {
		t.Fatalf("coalesced checks: ends=%v elided=%v", ends, elided)
	}
}

func compileAMD64EmissionTest(t *testing.T, source []byte) corecompiler.Output {
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
	output, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func countAMD64VPshufbRIP(code []byte) int {
	count := 0
	for i := 0; i+4 < len(code); i++ {
		if code[i] == 0xc4 && code[i+1] == 0xe2 && code[i+3] == 0x00 && code[i+4]&0xc7 == 0x05 {
			count++
		}
	}
	return count
}

func TestAMD64RailMachAdmissionKeepsUnprovedModuleShapesStructured(t *testing.T) {
	stack := &railssa.StackFunc{HasReferences: true}
	if !amd64RailMachCandidate(stack, false, false) {
		t.Fatal("ordinary scalar candidate was rejected")
	}
	if !amd64RailMachCandidate(stack, false, true) {
		t.Fatal("dense-global leaf was rejected")
	}
	stack.Instrs = []railssa.StackInstr{{Kind: wasm.InstrGlobalGet}, {Kind: wasm.InstrCall}}
	if !amd64RailMachCandidate(stack, false, true) {
		t.Fatal("acyclic dense-global call helper was rejected")
	}
	stack.MaxLoopDepth = 1
	if !amd64RailMachCandidate(stack, false, true) {
		t.Fatal("single-loop dense-global call helper was rejected")
	}
	stack.MaxLoopDepth = 2
	if amd64RailMachCandidate(stack, false, true) {
		t.Fatal("nested-loop dense-global call helper was admitted")
	}
	stack.MaxLoopDepth = 0
	stack.HasReferences = false
	stack.Instrs = make([]railssa.StackInstr, 1025)
	if !amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large acyclic parameterless function was rejected")
	}
	stack.MaxLoopDepth = 2
	if amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large nested-loop parameterless function was admitted")
	}
	stack.Params = []wasm.ValType{wasm.I32}
	stack.MaxLoopDepth = 0
	if !amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large parameterized scalar candidate was rejected")
	}
	stack.Instrs[0].Kind = wasm.InstrMemoryCopy
	if amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large memory.copy function was admitted")
	}
}

func TestAMD64RailMachAdmissionAcceptsRecursiveI64Loop(t *testing.T) {
	stack := &railssa.StackFunc{
		MaxLoopDepth: 1,
		Results:      []wasm.ValType{wasm.I64},
		Instrs:       []railssa.StackInstr{{Kind: wasm.InstrCall}, {Kind: wasm.InstrI64Add}},
	}
	if !amd64RailMachCandidate(stack, false, false) {
		t.Fatal("recursive i64 loop was rejected")
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

func TestAMD64RailMachSignalsElideFoldedMemoryBoundsCheck(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x01, 0x20, 0x00, 0x28, 0x02, 0x00, 0x6a, 0x0b,
		}))),
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
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&nativeBackendPlanner{}).Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	folded := false
	for _, producer := range plan.PostRAMemoryFrom {
		folded = folded || producer != 0
	}
	if !folded {
		t.Fatal("fixture produced no folded memory operand")
	}
	var explicitMetadata, signalsMetadata functionEmissionMetadata
	explicit, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, &explicitMetadata)
	if err != nil || !used {
		t.Fatalf("explicit RailMach emission: used=%v err=%v", used, err)
	}
	signalsPlan := *plan
	signalsPlan.SignalsBounds = true
	signals, _, used, err := emitAMD64RailMach(fn, &signalsPlan, nil, nil, &signalsMetadata)
	if err != nil || !used {
		t.Fatalf("signals RailMach emission: used=%v err=%v", used, err)
	}
	if len(explicitMetadata.Traps) == 0 || len(signalsMetadata.Traps) != 0 || len(signals) >= len(explicit) {
		t.Fatalf("folded bounds emission: explicit bytes/traps=%d/%d signals=%d/%d", len(explicit), len(explicitMetadata.Traps), len(signals), len(signalsMetadata.Traps))
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
