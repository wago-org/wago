//go:build amd64

package dragline

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestAMD64CarriesMemoryChecksAcrossMemoryFreeLayoutSibling(t *testing.T) {
	plan := &nativeBackendPlan{Machine: &railmach.Func{Blocks: make([]railmach.Block, 4)}, CFG: &railssa.CFG{
		Blocks: []railssa.Block{
			{SuccStart: 0, SuccCount: 2},
			{PredStart: 0, PredCount: 1},
			{PredStart: 1, PredCount: 1},
			{PredStart: 2, PredCount: 2},
		},
		Preds: []railssa.BlockID{0, 0, 1, 2},
	}}
	if !amd64RailMachCarriesMemoryChecks(plan, 0, 1) || !amd64RailMachCarriesMemoryChecks(plan, 1, 2) {
		t.Fatal("common-predecessor bounds facts were not carried")
	}
	if amd64RailMachCarriesMemoryChecks(plan, 2, 3) {
		t.Fatal("join block carried path-local checks")
	}
	plan.Machine.Blocks[1] = railmach.Block{InstCount: 1}
	plan.Machine.Memory = []railmach.MemoryAccess{{Instruction: 0}}
	if amd64RailMachCarriesMemoryChecks(plan, 1, 2) {
		t.Fatal("memory-producing sibling carried path-local checks")
	}
}

func TestAMD64PairsAdjacentSignedConstantDivisionAndRemainder(t *testing.T) {
	machine := &railmach.Func{
		Target: railmach.TargetAMD64,
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Aux: 10, Result: 2},
			{Op: wasm.InstrI32DivS, OperandStart: 0, OperandCount: 2, Result: 3},
			{Op: wasm.InstrI32Add},
			{Op: wasm.InstrI32RemS, OperandStart: 2, OperandCount: 2, Result: 4},
			{Op: wasm.InstrI32DivU},
		},
		Operands: []railmach.Operand{{Reg: 1}, {Reg: 2}, {Reg: 1}, {Reg: 2}},
		VRegs:    make([]railmach.VRegData, 5),
	}
	machine.VRegs[2] = railmach.VRegData{Def: 3, Type: railmach.TypeI32, Bank: railmach.BankGPR}
	plan := &nativeBackendPlan{Machine: machine, AMD64SignedImmediateRemainders: true}
	paired := []uint32{1, 2, 3}
	if !amd64RailMachPairedSignedI32Division(plan, paired, 0) || !amd64RailMachPairedSignedI32Division(plan, paired, 2) {
		t.Fatal("matching div/rem pair was not recognized across an ordinary instruction")
	}
	interveningDivision := []uint32{1, 2, 4, 3}
	if amd64RailMachPairedSignedI32Division(plan, interveningDivision, 0) || amd64RailMachPairedSignedI32Division(plan, interveningDivision, 3) {
		t.Fatal("pair crossed an intervening integer division")
	}
	machine.Operands[2].Reg = 3
	if amd64RailMachPairedSignedI32Division(plan, paired, 0) || amd64RailMachPairedSignedI32Division(plan, paired, 2) {
		t.Fatal("pair crossed unequal dividends")
	}
}

func TestAMD64UnsignedVectorComparePreservesAliasedRHS(t *testing.T) {
	var got amd64.Asm
	var patches []amd64SIMDConstantPatch
	emitAMD64UnsignedVectorCompare(&got, railmach.OpAMD64I16x8GeU, 2, 1, 2, &patches)

	var want amd64.Asm
	want.MovdquRipPlaceholder(5)
	want.VPxor(2, 2, 5)
	want.VPxor(5, 1, 5)
	want.VPcmpgtw(2, 2, 5)
	if !bytes.Equal(got.B, want.B) {
		t.Fatalf("aliased unsigned comparison = %x, want %x", got.B, want.B)
	}
	if len(patches) != 1 || patches[0].bytes != amd64UnsignedVectorSignMask(railmach.OpAMD64I16x8GeU) {
		t.Fatalf("aliased unsigned comparison patches = %#v", patches)
	}
}

func TestAMD64RailMachRetainsGlobalDescriptorAcrossScalarUpdate(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x23, 0x00, 0x41, 0x01, 0x6a, 0x24, 0x00,
			0x23, 0x01, 0x41, 0x01, 0x6a, 0x24, 0x01,
			0x23, 0x02, 0x41, 0x01, 0x6a, 0x24, 0x02,
			0x23, 0x03, 0x41, 0x01, 0x6a, 0x24, 0x03,
			0x0b,
		}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	fn, err := buildCompilerFunc(module, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&nativeBackendPlanner{}).Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	native, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !used {
		t.Fatalf("global update finalization = used %t, err %v", used, err)
	}
	var loadGlobals amd64.Asm
	loadGlobals.Load64(amd64.R10, amd64.RBX, -int32(abi.GlobalsPtrOffset))
	if got := bytes.Count(native, loadGlobals.B); got != 4 {
		t.Fatalf("globals table loads = %d, want one per update; code = %x", got, native)
	}
}

func TestAMD64RailMachRenamesReductionResultToBackedgeDestination(t *testing.T) {
	body := []byte{
		0x42, 0x00, 0x21, 0x02, // accumulator = i64.const 0
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // break when count == 0
		0x20, 0x02, 0x20, 0x01, 0x29, 0x03, 0x00, 0x7c, 0x21, 0x02, // accumulator += load64(address)
		0x20, 0x01, 0x41, 0x08, 0x6a, 0x21, 0x01, // address += 8
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // count -= 1
		0x0c, 0x00, 0x0b, 0x0b, // continue; end loop; end block
		0x20, 0x02, 0x0b,
	}
	function := append([]byte{0x02, 0x01, 0x7f, 0x01, 0x7e}, body...)
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	fn, err := buildCompilerFunc(module, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&nativeBackendPlanner{}).Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	plan.SignalsBounds = true
	rename := amd64EdgeResultRename{}
	for block := range plan.Machine.Blocks {
		if candidate := amd64RailMachEdgeResultRename(plan, uint32(block)); candidate.valid {
			rename = candidate
			break
		}
	}
	if !rename.valid {
		t.Fatal("reduction result was not eligible for edge renaming")
	}
	move := plan.Exit.Moves[rename.move]
	instruction := plan.Machine.Insts[rename.instruction]
	operands := plan.Machine.InstructionOperands(rename.instruction)
	if len(operands) != 2 || instruction.Result != move.Reg {
		t.Fatalf("renamed instruction = %#v, operands = %#v, move = %#v", instruction, operands, move)
	}
	position := plan.Allocation.InstructionPositions[rename.instruction]*6 + 2
	lhs := plan.Allocation.LocationAt(operands[0].Reg, position)
	if lhs.Kind != railmach.LocationRegister || move.Src.Kind != railmach.LocationRegister || move.Dst.Kind != railmach.LocationRegister {
		t.Fatalf("renamed locations = lhs %#v, move %#v", lhs, move)
	}
	native, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !used {
		t.Fatalf("reduction emission = used %t, err %v", used, err)
	}
	var inputCopy, edgeCopy amd64.Asm
	inputCopy.MovReg64(amd64RailMachPhysical(move.Src), amd64RailMachPhysical(lhs))
	edgeCopy.MovReg64(amd64RailMachPhysical(move.Dst), amd64RailMachPhysical(move.Src))
	if bytes.Contains(native, inputCopy.B) || bytes.Contains(native, edgeCopy.B) {
		t.Fatalf("renamed reduction retained register copies: input=%x edge=%x code=%x", inputCopy.B, edgeCopy.B, native)
	}
}

func TestAMD64RailMachRetainsGlobalDescriptorsAcrossLocalCall(t *testing.T) {
	globalUpdates := []byte{
		0x23, 0x00, 0x41, 0x01, 0x6a, 0x24, 0x00,
		0x23, 0x01, 0x41, 0x01, 0x6a, 0x24, 0x01,
		0x23, 0x02, 0x41, 0x01, 0x6a, 0x24, 0x02,
		0x23, 0x03, 0x41, 0x01, 0x6a, 0x24, 0x03,
	}
	calleeUpdate := []byte{0x23, 0x00, 0x41, 0x01, 0x6a, 0x24, 0x00}
	calleeBody := make([]byte, 0, len(calleeUpdate)*16+1)
	for range 16 {
		calleeBody = append(calleeBody, calleeUpdate...)
	}
	calleeBody = append(calleeBody, 0x0b)
	callerBody := append([]byte(nil), globalUpdates...)
	callerBody = append(callerBody, 0x10, 0x00)
	callerBody = append(callerBody, 0x10, 0x01)
	callerBody = append(callerBody, globalUpdates...)
	callerBody = append(callerBody, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(calleeBody), wasmtest.Code([]byte{0x0b}), wasmtest.Code(callerBody))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	callee, err := buildCompilerFunc(module, 0, new(railssa.StackFunc))
	if err != nil {
		t.Fatal(err)
	}
	calleePlan, err := new(nativeBackendPlanner).Plan(callee.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	if calleePlan.LocalABI.CalleeGPRs&(uint64(1)<<nativeAMD64GlobalsRegister) == 0 {
		t.Fatalf("global-descriptor register is not preserved by the local-call ABI: ABI=%#v caches=%t globals=%t instructions=%d", calleePlan.LocalABI, nativeAMD64CachesGlobalDescriptors(calleePlan.Machine), nativeAMD64CachesGlobals(calleePlan.Machine), len(calleePlan.Machine.Insts))
	}
	readOnlyCallee, err := buildCompilerFunc(module, 1, new(railssa.StackFunc))
	if err != nil {
		t.Fatal(err)
	}
	readOnlyPlan, err := new(nativeBackendPlanner).Plan(readOnlyCallee.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	contracts := []railmach.ABIContract{calleePlan.ABI, readOnlyPlan.ABI}
	caller, err := buildCompilerFunc(module, 2, new(railssa.StackFunc))
	if err != nil {
		t.Fatal(err)
	}
	callerPlan, err := new(nativeBackendPlanner).PlanProfileIPRA(caller.Structured, target, corecompiler.ObjectiveSpeed, caller.Index, nil, nil, contracts, nil, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	var relocs []amd64CallReloc
	native, _, used, err := emitAMD64RailMach(caller, callerPlan, &relocs, nil, nil)
	if err != nil || !used {
		t.Fatalf("local-call finalization = used %t, err %v", used, err)
	}
	var loadGlobals amd64.Asm
	loadGlobals.Load64(amd64RailMachGPRRegisters[nativeAMD64GlobalsRegister], amd64.RBX, -int32(abi.GlobalsPtrOffset))
	if got := bytes.Count(native, loadGlobals.B); got != 2 {
		t.Fatalf("globals table loads = %d, want entry and read-only-call loads only; code = %x", got, native)
	}
}

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
	plan.SignalsBounds = true
	unrolled, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !used {
		t.Fatalf("unrolled countdown finalization = used %t, err %v", used, err)
	}
	if len(unrolled) <= len(native)+16 {
		t.Fatalf("signals countdown code = %d bytes, checked code = %d; loop was not unrolled", len(unrolled), len(native))
	}
	oldCount := plan.Schedule.BlockRanges[rotatedBlock].Count
	plan.Schedule.BlockRanges[rotatedBlock].Count = 9
	if _, _, ok := amd64RailMachRotatedZeroTestLatch(plan, rotatedBlock, rotatedEdge); ok {
		t.Fatal("large countdown body was rotated")
	}
	plan.Schedule.BlockRanges[rotatedBlock].Count = oldCount
}

func TestAMD64RailMachSelfLoopUnrollCostModel(t *testing.T) {
	tests := []struct {
		name                              string
		weight, instructions, bytes, debt uint64
		want                              int
	}{
		{name: "compact", weight: 64, instructions: 200, bytes: 128, debt: 1 << 16, want: 3},
		{name: "hot-spill-budget", weight: 512, instructions: 200, bytes: 192, debt: 1 << 17, want: 2},
		{name: "cold", weight: 63, instructions: 200, bytes: 128, debt: 0},
		{name: "large-function", weight: 64, instructions: 257, bytes: 128, debt: 0},
		{name: "tiny-loop", weight: 64, instructions: 200, bytes: 63, debt: 0},
		{name: "large-loop", weight: 64, instructions: 200, bytes: 257, debt: 0},
		{name: "spill-heavy", weight: 64, instructions: 200, bytes: 128, debt: 1<<16 + 1},
		{name: "hot-spill-heavy", weight: 512, instructions: 200, bytes: 128, debt: 1<<17 + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := amd64RailMachSelfLoopUnrollCopies(uint32(test.weight), int(test.instructions), test.debt, int(test.bytes))
			if got != test.want {
				t.Fatalf("copies = %d, want %d", got, test.want)
			}
		})
	}
}

func TestAMD64RailMachUnrollsHotConditionalSelfLoop(t *testing.T) {
	body := []byte{0x03, 0x40} // loop
	for index := range 20 {
		body = append(body, 0x20, 0x00, 0x41, byte(index), 0x36, 0x02)
		body = append(body, wasmtest.ULEB(uint32(index*4))...) // memory[counter + offset] = index
	}
	body = append(body,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // ++counter
		0x20, 0x00, 0x41, 0xc0, 0x00, 0x49, 0x0d, 0x00, // continue while counter < 64
		0x0b, 0x20, 0x00, 0x0b,
	)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
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
	selfLoopBlock := railssa.BlockID(0)
	foundSelfLoop := false
	for _, edge := range plan.Machine.Edges {
		if edge.From == edge.To {
			selfLoopBlock = edge.From
			foundSelfLoop = true
		}
	}
	if !foundSelfLoop {
		t.Fatal("conditional self-loop was not preserved")
	}
	plan.SignalsBounds = true
	plan.Machine.Blocks[selfLoopBlock].Weight = 1
	rolled, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !used {
		t.Fatalf("rolled self-loop finalization = used %t, err %v", used, err)
	}
	plan.Machine.Blocks[selfLoopBlock].Weight = 64
	unrolled, _, used, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !used {
		t.Fatalf("unrolled self-loop finalization = used %t, err %v", used, err)
	}
	if len(unrolled) <= len(rolled)+128 {
		t.Fatalf("unrolled self-loop code = %d bytes, rolled code = %d; loop was not unrolled", len(unrolled), len(rolled))
	}
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

func TestAMD64RailMachRecognizesPreparedSingleArgumentCall(t *testing.T) {
	plan := &nativeBackendPlan{
		Stack: &railssa.StackFunc{FunctionIndex: 2, ImportedFuncs: 1},
		Calls: []railmach.CallContract{{Instruction: 7, Callee: 3, Class: railmach.ABIPreparedCall}},
	}
	instruction := railmach.Inst{Op: wasm.InstrCall, Aux: uint64(1)<<32 | 3, OperandCount: 1, Result: 1}
	if !amd64RailMachFastSingleArgumentCall(plan, 7, instruction) {
		t.Fatal("prepared single-argument call did not select the register path")
	}
	plan.Calls[0].Conservative = true
	if amd64RailMachFastSingleArgumentCall(plan, 7, instruction) {
		t.Fatal("conservative call selected the register-only path")
	}
}

func TestAMD64RailMachTracksBMI2OnlyForFoldedRotates(t *testing.T) {
	plan := &nativeBackendPlan{
		AMD64BMI2: true,
		Machine:   &railmach.Func{Insts: []railmach.Inst{{Op: wasm.InstrI32Rotr}}},
	}
	plan.ImmediateProducer.prepare(1, true)
	plan.ImmediateProducer.set(0, 0)
	if !amd64RailMachMayUseBMI2(plan) {
		t.Fatal("BMI2 folded rotate was not tracked")
	}
	plan.ImmediateProducer.prepare(1, true)
	if amd64RailMachMayUseBMI2(plan) {
		t.Fatal("non-folded rotate was marked as requiring BMI2")
	}
	plan.ImmediateProducer.set(0, 0)
	plan.AMD64BMI2 = false
	if amd64RailMachMayUseBMI2(plan) {
		t.Fatal("baseline target was marked as requiring BMI2")
	}
}

func TestAMD64RailMachCallArgumentsBreakRegisterCycle(t *testing.T) {
	var got amd64.Asm
	amd64EmitRailMachCallArguments(&got, []amd64RailMachCallArgument{
		{src: amd64.RCX, dst: amd64.RAX, i32: true},
		{src: amd64.RAX, dst: amd64.RCX, i32: true},
	})
	var want amd64.Asm
	want.MovReg64(amd64.RSI, amd64.RAX)
	want.MovReg32(amd64.RAX, amd64.RCX)
	want.MovReg32(amd64.RCX, amd64.RSI)
	if !bytes.Equal(got.B, want.B) {
		t.Fatalf("cycle assignment = %x, want %x", got.B, want.B)
	}
}

func TestAMD64RailMachUsesRegisterTupleOnlyWhenEveryArgumentIsResident(t *testing.T) {
	plan := &nativeBackendPlan{
		Stack: &railssa.StackFunc{FunctionIndex: 2, ImportedFuncs: 1},
		Calls: []railmach.CallContract{{Instruction: 7, Callee: 3, Class: railmach.ABIPreparedCall}},
		Machine: &railmach.Func{VRegs: []railmach.VRegData{
			{}, {Bank: railmach.BankGPR, Type: railmach.TypeI32}, {Bank: railmach.BankGPR, Type: railmach.TypeI32},
		}},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: []railmach.Location{
			{}, {Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 1}, {Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 0},
		}}},
	}
	instruction := railmach.Inst{Op: wasm.InstrCall, Aux: uint64(1)<<32 | 3, OperandCount: 2, Result: 3}
	operands := []railmach.Operand{{Reg: 1}, {Reg: 2}}
	if !amd64RailMachPrivateRegisterCall(plan, 7, instruction, operands, 0) {
		t.Fatal("resident prepared tuple did not select register arguments")
	}
	plan.Allocation.Locations[2].Kind = railmach.LocationSpill
	if amd64RailMachPrivateRegisterCall(plan, 7, instruction, operands, 0) {
		t.Fatal("spilled tuple bypassed the canonical call vector")
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

func TestAMD64ShuffleAlignrOffset(t *testing.T) {
	for offset := byte(0); offset <= 16; offset++ {
		var lanes [16]byte
		for i := range lanes {
			lanes[i] = offset + byte(i)
		}
		got, ok := amd64ShuffleAlignrOffset(lanes)
		if !ok || got != offset {
			t.Fatalf("offset %d: got (%d, %v)", offset, got, ok)
		}
	}
	for _, lanes := range [][16]byte{
		{17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 0},
		{14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 30},
	} {
		if offset, ok := amd64ShuffleAlignrOffset(lanes); ok {
			t.Fatalf("non-contiguous lanes %v selected offset %d", lanes, offset)
		}
	}
}

func TestAMD64StructuredScalarResidencySelectsHotIntegerLocals(t *testing.T) {
	locals := []wasm.ValType{wasm.I32, wasm.V128, wasm.I64, wasm.F32, wasm.I32, wasm.I64, wasm.I32, wasm.I32}
	uses := []uint32{2, 100, 9, 50, 7, 6, 5, 4}
	assigned := make([]amd64.Reg, len(locals))
	pinned := make([]bool, len(locals))
	amd64PinHotStructuredScalarLocals(amd64StackLocalRegisters[:], locals, uses, assigned, pinned)
	for local, register := range map[int]amd64.Reg{0: amd64.R9, 2: amd64.R12, 4: amd64.R13, 5: amd64.R14, 6: amd64.R15, 7: amd64.R8} {
		if !pinned[local] || assigned[local] != register {
			t.Fatalf("local %d: assigned=%v pinned=%v", local, assigned, pinned)
		}
	}
	if pinned[1] || pinned[3] {
		t.Fatalf("non-integer local pinned: %v", pinned)
	}
	clear(assigned)
	clear(pinned)
	amd64PinHotStructuredScalarLocals(amd64StackLocalRegisters[:4], locals, uses, assigned, pinned)
	for local, register := range map[int]amd64.Reg{2: amd64.R12, 4: amd64.R13, 5: amd64.R14, 6: amd64.R15} {
		if !pinned[local] || assigned[local] != register {
			t.Fatalf("nonvolatile local %d: assigned=%v pinned=%v", local, assigned, pinned)
		}
	}
	if pinned[0] || pinned[7] {
		t.Fatalf("call-bearing allocation used argument registers: %v", pinned)
	}
}

func TestAMD64StructuredScalarResidencyPinsHotSubsetWithoutSIMD(t *testing.T) {
	body := []byte{
		0x01, 0x06, 0x7f, // six i32 locals plus the i32 parameter
		0x03, 0x40, 0x20, 0x00, 0x1a, 0x0b, // structured loop using parameter 0
		0x41, 0x07, 0x21, 0x00, // replace parameter 0 outside the loop
		0x20, 0x00, 0x0b,
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
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
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	var planner railssa.EmissionPlanner
	plan, err := planCompilerFunc(fn, &planner)
	if err != nil {
		t.Fatal(err)
	}
	native, _, _, err := emitAMD64Stack(fn, plan, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var pinned amd64.Asm
	pinned.MovReg64(amd64.R12, amd64.RAX)
	if !bytes.Contains(native, pinned.B) {
		t.Fatalf("structured scalar function did not pin its hot parameter: %x", native)
	}
	var directSet, directGet amd64.Asm
	directSet.MovReg64(amd64.R12, amd64.RDI)
	directGet.MovReg64(amd64.RDI, amd64.R12)
	if !bytes.Contains(native, directSet.B) || !bytes.Contains(native, directGet.B) {
		t.Fatalf("structured pinned local still round-trips through scratch: %x", native)
	}
}

func TestAMD64StructuredLoadsSIMDDirectlyFromPinnedI32Address(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0xfd, 0x00, 0x04, 0x10, // v128.load offset=16
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
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	var planner railssa.EmissionPlanner
	plan, err := planCompilerFunc(fn, &planner)
	if err != nil {
		t.Fatal(err)
	}
	native, _, _, err := emitAMD64Stack(fn, plan, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var direct, redundant amd64.Asm
	direct.VMovdquLoadIdx(4, amd64.RBX, amd64.R12, 16)
	redundant.MovReg32(amd64.R10, amd64.R12)
	if !bytes.Contains(native, direct.B) {
		t.Fatalf("structured pinned-address SIMD load was not direct: %x", native)
	}
	if bytes.Contains(native, redundant.B) {
		t.Fatalf("structured pinned-address SIMD load copied through scratch: %x", native)
	}
}

func TestAMD64StructuredLoadsSIMDDirectlyFromCachedI32Address(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x41, 0x04, // i32.const 4
			0x6a,                   // i32.add
			0xfd, 0x00, 0x04, 0x10, // v128.load offset=16
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
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	plan := new(railssa.EmissionPlan)
	plan.ElideAllMemoryBounds()
	native, _, _, err := emitAMD64Stack(fn, plan, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var direct, redundant amd64.Asm
	direct.VMovdquLoadIdx(4, amd64.RBX, amd64.RDI, 16)
	redundant.LeaScaled(amd64.R10, amd64.RBX, amd64.RDI, 0, 16)
	if !bytes.Contains(native, direct.B) {
		t.Fatalf("structured cached-address SIMD load was not direct: %x", native)
	}
	if bytes.Contains(native, redundant.B) {
		t.Fatalf("structured cached-address SIMD load copied through scratch: %x", native)
	}
}

func TestAMD64StructuredLoadsScalarDirectlyFromCachedI32Address(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x41, 0x04, // i32.const 4
			0x6a,             // i32.add
			0x28, 0x02, 0x10, // i32.load offset=16
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
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	plan := new(railssa.EmissionPlan)
	plan.ElideAllMemoryBounds()
	native, _, _, err := emitAMD64Stack(fn, plan, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var direct, redundant amd64.Asm
	direct.LoadIdx(amd64.R10, amd64.RBX, amd64.RDI, 16, 4, false, false)
	redundant.MovReg64(amd64.RAX, amd64.RDI)
	if !bytes.Contains(native, direct.B) {
		t.Fatalf("structured cached-address scalar load was not direct: %x", native)
	}
	if bytes.Contains(native, redundant.B) {
		t.Fatalf("structured cached-address scalar load copied through scratch: %x", native)
	}
}

func TestAMD64StructuredSignalsDoNotReserveMemorySizeCache(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x28, 0x02, 0x00, // i32.load(local 0)
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
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	var planner railssa.EmissionPlanner
	plan, err := planCompilerFunc(fn, &planner)
	if err != nil {
		t.Fatal(err)
	}
	explicit, _, _, err := emitAMD64Stack(fn, plan, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	signalsPlan := new(railssa.EmissionPlan)
	signalsPlan.ElideAllMemoryBounds()
	signals, _, _, err := emitAMD64Stack(fn, signalsPlan, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var boundLoad amd64.Asm
	boundLoad.Load64(amd64.RBP, amd64.RBX, -int32(abi.ActualLinMemByteSize64Offset))
	if !bytes.Contains(explicit, boundLoad.B) {
		t.Fatalf("explicit structured code has no memory-size cache load: %x", explicit)
	}
	if bytes.Contains(signals, boundLoad.B) {
		t.Fatalf("signal-bounded structured code reserved the memory-size cache: %x", signals)
	}
}

func TestAMD64StructuredSIMDHighRegistersRespectStackPressure(t *testing.T) {
	if !amd64StructuredSIMDHighRegisterWorthwhile(5, 0, 10) {
		t.Fatal("the base six resident registers must remain available")
	}
	if amd64StructuredSIMDHighRegisterWorthwhile(6, 47, 6) {
		t.Fatal("a low-reuse value displaced a register needed by the vector stack")
	}
	if !amd64StructuredSIMDHighRegisterWorthwhile(6, 80, 10) {
		t.Fatal("a high-reuse value did not earn an additional resident register")
	}
}

func TestAMD64RailMachAvoidsUnneededPinnedLocalSaveAcrossExactCall(t *testing.T) {
	callee := wasmtest.Code([]byte{0x20, 0x00, 0x0b})
	callerBody := []byte{
		0x01, 0x01, 0x7b, // one v128 local exercises mixed-bank planning
		0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x1a, // v128.const 0; drop
		0x20, 0x00, 0x10, 0x00, 0x1a, // call 0(local.get 0); drop
		0x20, 0x00, 0x0b, // return the pinned parameter
	}
	caller := append(wasmtest.ULEB(uint32(len(callerBody))), callerBody...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(callee, caller)),
	)
	output := compileAMD64EmissionTest(t, source)
	var save, restore amd64.Asm
	save.StoreRsp64(0, amd64.R12)
	restore.LoadRsp32(amd64.R12, 0)
	if bytes.Contains(output.Code, save.B) || bytes.Contains(output.Code, restore.B) {
		t.Fatalf("exact local call retained an unnecessary pinned-local save: %x", output.Code)
	}
}

func TestAMD64StructuredVectorTeeDoesNotCopyBack(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x7b, // one v128 local
		0x20, 0x00, 0x22, 0x01, 0x20, 0x01, 0xfd, 0x51, 0x0b, // tee local 1, then xor the stack and local copies
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	output := compileAMD64EmissionTest(t, source)
	var inverse amd64.Asm
	inverse.VMovdqu(9, 4)
	inverse.VMovdqu(4, 9)
	if bytes.Contains(output.Code, inverse.B) {
		t.Fatalf("vector local.tee copied its value back into the source register: %x", output.Code)
	}
}

func TestAMD64StructuredVectorSelectUsesExistingRegisters(t *testing.T) {
	var got, want amd64.Asm
	emitAMD64StructuredVectorSelect(&got, amd64.R11, 4, 5)
	want.TestSelf(amd64.R11, false)
	keepLHS := want.JccPlaceholder(amd64.CondNE)
	want.VMovdqu(4, 5)
	want.PatchRel32(keepLHS, want.Len())
	if !bytes.Equal(got.B, want.B) {
		t.Fatalf("vector select = %x, want %x", got.B, want.B)
	}
}

func TestAMD64RailMachSpillForwardingRetainsLiveHomes(t *testing.T) {
	allocation := railmach.GreedyAllocation{Allocation: railmach.Allocation{Intervals: []railmach.LiveInterval{
		{Reg: 1, Start: 3, End: 14, Bank: railmach.BankFPR},
		{Reg: 2, Start: 9, End: 30, Bank: railmach.BankFPR},
	}}}
	machine := railmach.Func{
		Insts:    []railmach.Inst{{Op: wasm.InstrF64Add, OperandCount: 2}},
		Operands: []railmach.Operand{{Reg: 1}, {Reg: 2}},
	}
	plan := nativeBackendPlan{Machine: &machine, Allocation: &allocation}
	if forward, elideStore := amd64RailMachForwardPendingSpill(&plan, 0, 1, 14); !forward || !elideStore {
		t.Fatalf("last-use spill forwarding = (%v, %v), want (true, true)", forward, elideStore)
	}
	if forward, elideStore := amd64RailMachForwardPendingSpill(&plan, 0, 2, 14); !forward || elideStore {
		t.Fatalf("live spill forwarding = (%v, %v), want (true, false)", forward, elideStore)
	}
	if forward, elideStore := amd64RailMachForwardPendingSpill(&plan, 0, 3, 14); forward || elideStore {
		t.Fatalf("unused spill forwarding = (%v, %v), want (false, false)", forward, elideStore)
	}
}

func TestAMD64RailMachUsesAllocatedMemoryAddressesDirectly(t *testing.T) {
	allocation := railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: []railmach.Location{
		{},
		{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 2},
		{Kind: railmach.LocationSpill, Bank: railmach.BankGPR, Index: 0},
	}}}
	plan := nativeBackendPlan{Allocation: &allocation}
	if !amd64RailMachCanUseMemoryAddressDirectly(&plan, 1, 0, math.MaxInt32, false) {
		t.Fatal("allocated address with encodable offset required a scratch copy")
	}
	allocation.Locations[1].Index = 5
	if amd64RailMachCanUseMemoryAddressDirectly(&plan, 1, 0, 0, false) {
		t.Fatal("live callee-saved address bypassed its scratch copy")
	}
	plan.SignalsBounds = true
	if !amd64RailMachCanUseMemoryAddressDirectly(&plan, 1, 0, 0, false) {
		t.Fatal("signal-bounded callee-saved address required a scratch copy")
	}
	plan.SignalsBounds = false
	allocation.Intervals = append(allocation.Intervals, railmach.LiveInterval{Reg: 1, Start: 0, End: 6, Bank: railmach.BankGPR})
	if !amd64RailMachCanUseMemoryAddressDirectly(&plan, 1, 0, 0, false) {
		t.Fatal("dead callee-saved address required a scratch copy")
	}
	if !amd64RailMachCanUseMemoryAddressDirectly(&plan, 1, 0, 0, true) {
		t.Fatal("dead address aliased by its load result required a scratch copy")
	}
	allocation.Locations[1].Index = 2
	allocation.Intervals[0].End = 12
	if amd64RailMachCanUseMemoryAddressDirectly(&plan, 2, 0, 0, false) || amd64RailMachCanUseMemoryAddressDirectly(&plan, 1, 0, math.MaxInt32+1, false) || amd64RailMachCanUseMemoryAddressDirectly(&plan, 1, 0, 0, true) {
		t.Fatal("unsafe memory address bypassed its scratch copy")
	}
}

func TestAMD64FloatZeroMaterializationUsesZeroIdiom(t *testing.T) {
	var zero, nonzero amd64.Asm
	emitAMD64FloatBits(&zero, 12, 0, true)
	emitAMD64FloatBits(&nonzero, 12, 1, true)
	if zero.Len() >= nonzero.Len() {
		t.Fatalf("zero materialization = %d bytes, nonzero = %d", zero.Len(), nonzero.Len())
	}
}

func TestAMD64RailMachFloatRematerializationUsesConstantPool(t *testing.T) {
	const bits = uint64(0x3ff8000000000000)
	machine := &railmach.Func{
		Insts: []railmach.Inst{{Op: wasm.InstrF64Const, Aux: bits, Result: 1}},
		VRegs: []railmach.VRegData{{}, {Def: 0, Type: railmach.TypeF64, Bank: railmach.BankFPR}},
	}
	plan := &nativeBackendPlan{Machine: machine}
	var a amd64.Asm
	called := false
	reg, err := amd64RailMachReadLocationWithFloatConstant(&a, plan, 1, railmach.Location{Kind: railmach.LocationRematerialize, Bank: railmach.BankFPR}, 12, 0, func(dst amd64.Reg, got uint64, f64 bool) {
		called = true
		if dst != 12 || got != bits || !f64 {
			t.Fatalf("constant pool materialization = (dst %d, bits %#x, f64 %v)", dst, got, f64)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if reg != 12 || !called {
		t.Fatalf("rematerialized register = %d, callback called = %v", reg, called)
	}
}

func TestAMD64RailMachRematerializesWrappingAddressWithLEA(t *testing.T) {
	machine := &railmach.Func{
		Target: railmach.TargetAMD64,
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Aux: 16, Result: 2},
			{Op: wasm.InstrI32Add, OperandStart: 0, OperandCount: 2, Result: 3},
		},
		Operands: []railmach.Operand{{Reg: 1, Bank: railmach.BankGPR}, {Reg: 2, Bank: railmach.BankGPR}},
		VRegs: []railmach.VRegData{
			{},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Flags: railmach.VRegInitial},
			{Def: 3, Type: railmach.TypeI32, Bank: railmach.BankGPR, Flags: railmach.VRegRematerializable},
			{Def: 9, Type: railmach.TypeI32, Bank: railmach.BankGPR},
		},
	}
	allocation := &railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: []railmach.Location{
		{},
		{Kind: railmach.LocationRegister, Bank: railmach.BankGPR},
		{Kind: railmach.LocationRematerialize, Bank: railmach.BankGPR},
		{Kind: railmach.LocationSpill, Bank: railmach.BankGPR},
	}}}
	plan := &nativeBackendPlan{Machine: machine, Allocation: allocation}
	plan.AMD64AddressRematerialize.prepare(len(machine.VRegs), true)
	plan.AMD64AddressRematerialize.set(3, true)
	var got, want amd64.Asm
	reg, err := amd64RailMachReadLocation(&got, plan, 3, allocation.Locations[3], amd64.R10, 0)
	if err != nil {
		t.Fatal(err)
	}
	want.LeaDispW(amd64.R10, amd64.RAX, 16, false)
	if reg != amd64.R10 || !bytes.Equal(got.B, want.B) {
		t.Fatalf("wrapping address rematerialization = reg %d code %x, want %x", reg, got.B, want.B)
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

func TestAMD64StructuredBitmaskReadsPinnedLocalDirectly(t *testing.T) {
	body := bytes.Repeat([]byte{0x01}, 510) // force the large-bulk structured path
	body = append(body,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
		0x20, 0x00, 0xfd, 0x64, 0x0b, // local.get 0; i8x16.bitmask; end
	)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var direct, copied amd64.Asm
	direct.VPmovmskb(amd64.RAX, 8)
	copied.VMovdqu(4, 8)
	copied.VPmovmskb(amd64.RAX, 4)
	if !bytes.Contains(output.Code, direct.B) || bytes.Contains(output.Code, copied.B) {
		t.Fatalf("structured pinned-local bitmask was not direct: %x", output.Code)
	}
}

func TestAMD64StructuredBitmaskComparisonReadsLocalDirectly(t *testing.T) {
	body := bytes.Repeat([]byte{0x01}, 510) // force the large-bulk structured path
	body = append(body,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
		0x20, 0x00, 0xfd, 0x64, 0x41, 0x00, 0x47, 0x0b, // local.get 0; i8x16.bitmask; i32.const 0; i32.ne; end
	)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var direct amd64.Asm
	direct.VPmovmskb(amd64.RAX, 8)
	direct.TestSelf(amd64.RAX, false)
	direct.SetccReg(amd64.CondNE, amd64.RAX)
	if !bytes.Contains(output.Code, direct.B) {
		t.Fatalf("structured bitmask comparison was not direct: %x", output.Code)
	}
}

func TestAMD64StructuredShuffleLocalTeeFeedsShiftDirectly(t *testing.T) {
	body := bytes.Repeat([]byte{0x01}, 510) // force the large-bulk structured path
	body = append(body,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
		0x20, 0x00, 0x20, 0x01, 0xfd, 0x0d, // local.get 0; local.get 1; i8x16.shuffle
	)
	for lane := byte(1); lane <= 16; lane++ {
		body = append(body, lane)
	}
	body = append(body,
		0x22, 0x00, 0x41, 0x04, 0xfd, 0x8d, 0x01, // local.tee 0; i32.const 4; i16x8.shr_u
		0x0b,
	)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var direct, copied amd64.Asm
	direct.VPalignr(8, 8, 9, 1)
	direct.VPsrlwImm(4, 8, 4)
	copied.VPalignr(4, 8, 9, 1)
	copied.VMovdqu(8, 4)
	copied.VPsrlwImm(4, 4, 4)
	if !bytes.Contains(output.Code, direct.B) || bytes.Contains(output.Code, copied.B) {
		t.Fatalf("structured shuffle/local.tee/shift was not direct: %x", output.Code)
	}
}

func TestAMD64StructuredCallUsesWriteThroughPinnedLocalHomes(t *testing.T) {
	body := bytes.Repeat([]byte{0x01}, 510) // force the large-bulk structured path
	body = append(body,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // local.get 0; i32.const 1; i32.add; local.set 0
		0x10, 0x00, // call imported function 0
		0x20, 0x01, 0x1a, // local.get 1; drop
		0x20, 0x00, 0x0b, // local.get 0; end
	)
	functionImport := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	functionImport = append(functionImport, 0x00)
	functionImport = append(functionImport, wasmtest.ULEB(0)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(functionImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var scalarHome, vectorHome amd64.Asm
	scalarHome.StoreRsp64(0, amd64.R12)
	vectorHome.VMovdquStoreDisp(amd64.RSP, 8, 8)
	if got := bytes.Count(output.Code, scalarHome.B); got != 2 {
		t.Fatalf("pinned scalar home stores = %d, want entry plus assignment: %x", got, output.Code)
	}
	if got := bytes.Count(output.Code, vectorHome.B); got != 1 {
		t.Fatalf("pinned vector home stores = %d, want entry only: %x", got, output.Code)
	}
}

func TestAMD64StructuredCallReloadsOnlyCalleeClobbers(t *testing.T) {
	body := bytes.Repeat([]byte{0x01}, 510) // force the large-bulk structured path
	body = append(body,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, // local.get 0; i32.const 1; i32.add; local.set 0
		0x10, 0x00, // call local function 0
		0x20, 0x01, 0x1a, // local.get 1; drop
		0x20, 0x00, 0x0b, // local.get 0; end
	)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code(body),
		)),
	)
	output := compileAMD64EmissionTest(t, source)
	var scalarReload, vectorReload amd64.Asm
	scalarReload.LoadRsp32(amd64.R12, 0)
	vectorReload.VMovdquLoadDisp(8, amd64.RSP, 8)
	if bytes.Contains(output.Code, scalarReload.B) || bytes.Contains(output.Code, vectorReload.B) {
		t.Fatalf("structured caller reloaded registers preserved by exact callee contract: %x", output.Code)
	}
}

func TestAMD64StructuredScalarProducersWriteDirectlyToCache(t *testing.T) {
	body := bytes.Repeat([]byte{0x01}, 510) // force the large-bulk structured path
	body = append(body,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
		0x41, 0x07, 0x41, 0x05, 0x6a, 0x0b, // i32.const 7; i32.const 5; i32.add; end
	)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var direct, copied amd64.Asm
	direct.MovImm32(amd64.RDI, 7)
	copied.MovImm32(amd64.R10, 7)
	copied.MovReg64(amd64.RDI, amd64.R10)
	if !bytes.Contains(output.Code, direct.B) || bytes.Contains(output.Code, copied.B) {
		t.Fatalf("structured scalar producers did not write directly to cache: %x", output.Code)
	}
}

func TestAMD64StructuredControlConsumesCachedScalarsDirectly(t *testing.T) {
	body := bytes.Repeat([]byte{0x01}, 510) // force the large-bulk structured path
	body = append(body,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
		0x41, 0x07, 0x41, 0x05, 0x49, // i32.const 7; i32.const 5; i32.lt_u
		0x04, 0x40, 0x01, 0x0b, // if; nop; end
		0x0b, // end
	)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var direct, copied amd64.Asm
	direct.MovImm32(amd64.RDI, 7)
	direct.MovImm32(amd64.RSI, 5)
	direct.Cmp32(amd64.RDI, amd64.RSI)
	copied.MovImm32(amd64.RDI, 7)
	copied.MovImm32(amd64.RSI, 5)
	copied.MovReg64(amd64.R10, amd64.RSI)
	copied.MovReg64(amd64.RAX, amd64.RDI)
	copied.Cmp32(amd64.RAX, amd64.R10)
	if !bytes.Contains(output.Code, direct.B) || bytes.Contains(output.Code, copied.B) {
		t.Fatalf("structured control did not consume cached scalar operands directly: %x", output.Code)
	}
}

func TestAMD64StructuredBinaryReadsResidentConstantDirectly(t *testing.T) {
	constant := [16]byte{0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f, 0x0f}
	body := bytes.Repeat([]byte{0x01}, 510)                                         // force the large-bulk structured path
	body = append(body, 0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00) // memory.copy 0, 0
	for occurrence := 0; occurrence < 2; occurrence++ {
		body = append(body, 0x20, 0x00, 0x41, 0x04, 0xfd, 0x8d, 0x01, 0xfd, 0x0c) // local.get 0; i16x8.shr_u 4; v128.const
		body = append(body, constant[:]...)
		body = append(body, 0xfd, 0x4e) // v128.and
		if occurrence == 0 {
			body = append(body, 0x1a) // drop
		}
	}
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var direct, copied amd64.Asm
	direct.VPand(4, 4, 9)
	copied.VMovdqu(5, 9)
	copied.VPand(4, 4, 5)
	if !bytes.Contains(output.Code, direct.B) || bytes.Contains(output.Code, copied.B) {
		t.Fatalf("structured resident-constant binary was not direct: %x", output.Code)
	}
}

func TestAMD64StructuredSupportsI32x4Mul(t *testing.T) {
	body := []byte{0x01, 0x01, 0x7b, 0xfd, 0x0c} // one v128 local; v128.const
	body = append(body, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, 4, 0, 0, 0)
	body = append(body, 0xfd, 0x0c)
	body = append(body, 5, 0, 0, 0, 6, 0, 0, 0, 7, 0, 0, 0, 8, 0, 0, 0)
	body = append(body, 0xfd, 0xb5, 0x01, 0x0b) // i32x4.mul; end
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	compileAMD64EmissionTest(t, source)
}

func TestAMD64StructuredDoesNotMoveBranchResultWithinSameStackSlot(t *testing.T) {
	body := []byte{
		0x02, 0x7b, // block (result v128)
		0x20, 0x00, // local.get 0
		0x0c, 0x00, // br 0
		0x0b,                                                       // end block
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
	}
	body = append(body, bytes.Repeat([]byte{0x01}, 510)...) // force the large-bulk structured path
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var redundant amd64.Asm
	redundant.VMovdquLoadDisp(0, amd64.RSP, 16)
	redundant.VMovdquStoreDisp(amd64.RSP, 16, 0)
	if bytes.Contains(output.Code, redundant.B) {
		t.Fatalf("structured branch result moved within one canonical stack slot: %x", output.Code)
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

func TestAMD64RailMachShuffleUsesSelectedRegisterForms(t *testing.T) {
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
	if got := countAMD64VPshufbRIP(output.Code); got != 0 || !containsAMD64VEXOpcode(output.Code, 0x00) {
		t.Fatalf("selected shuffle code has RIP forms=%d or no register vpshufb: %x", got, output.Code)
	}
}

func TestAMD64RailMachFusesIntegerComparisonIntoSelect(t *testing.T) {
	body := []byte{
		0x20, 0x02, // local.get 2: true value
		0x20, 0x03, // local.get 3: false value
		0x20, 0x00, // local.get 0
		0x20, 0x01, // local.get 1
		0x48, // i32.lt_s
		0x1b, // select
		0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	if bytes.Contains(output.Code, []byte{0x0f, 0x9c}) || !bytes.Contains(output.Code, []byte{0x0f, 0x4d}) {
		t.Fatalf("comparison-fed select did not use flags directly: %x", output.Code)
	}
}

func TestAMD64StructuredContiguousShuffleUsesAlignr(t *testing.T) {
	body := []byte{
		0x20, 0x00, // local.get 0
		0x20, 0x01, // local.get 1
		0xfd, 0x0d, // i8x16.shuffle
	}
	for lane := byte(14); lane < 30; lane++ {
		body = append(body, lane)
	}
	body = append(body, 0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00) // memory.copy 0, 0, 0
	body = append(body, bytes.Repeat([]byte{0x01}, 510)...)                         // force the large-bulk structured path
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	if !containsAMD64VEXOpcode(output.Code, 0x0f) {
		t.Fatalf("contiguous structured shuffle emitted no vpalignr: %x", output.Code)
	}
}

func TestAMD64StructuredFusesAnyTrueIntoControl(t *testing.T) {
	body := []byte{
		0x20, 0x00, // local.get 0
		0xfd, 0x53, // v128.any_true
		0x04, 0x40, 0x01, 0x0b, // if; nop; end
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
	}
	body = append(body, bytes.Repeat([]byte{0x01}, 510)...)
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	if !bytes.Contains(output.Code, []byte{0xc4, 0xe2, 0x79, 0x17}) || bytes.Contains(output.Code, []byte{0x0f, 0x95}) {
		t.Fatalf("structured any_true did not feed control flags directly: %x", output.Code)
	}
}

func TestAMD64StructuredFusesMaskedAnyTrueIntoPtest(t *testing.T) {
	body := []byte{0x20, 0x00, 0xfd, 0x0c} // local.get 0; v128.const
	body = append(body, bytes.Repeat([]byte{0x80}, 16)...)
	body = append(body,
		0xfd, 0x4e, // v128.and
		0xfd, 0x53, // v128.any_true
		0x04, 0x40, 0x01, 0x0b, // if; nop; end
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
	)
	body = append(body, bytes.Repeat([]byte{0x01}, 510)...)
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	if got := countAMD64VPtestRIP(output.Code); got != 1 {
		t.Fatalf("structured masked any_true emitted %d RIP-relative vptest instructions, want 1: %x", got, output.Code)
	}
}

func TestAMD64StructuredFoldsConstantShiftCount(t *testing.T) {
	body := []byte{
		0x20, 0x00, // local.get 0
		0x42, 0x7f, // i64.const -1
		0x88,                                                       // i64.shr_u
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
	}
	body = append(body, bytes.Repeat([]byte{0x01}, 510)...)
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var immediate amd64.Asm
	immediate.ShiftImm(5, amd64.RDI, 0xff, true)
	if !bytes.Contains(output.Code, immediate.B) {
		t.Fatalf("structured constant shift did not use an immediate: %x", output.Code)
	}
}

func TestAMD64StructuredFoldsConstantIntegerBinary(t *testing.T) {
	body := make([]byte, 0, 600)
	for _, operation := range []byte{0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85} { // i64 add/sub/mul/and/or/xor
		body = append(body,
			0x20, 0x00, // local.get 0
			0x42, 0x7f, // i64.const -1
			operation,
			0x1a, // drop
		)
	}
	body = append(body, 0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00) // memory.copy 0, 0
	body = append(body, bytes.Repeat([]byte{0x01}, 510)...)
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	for _, digit := range []byte{0, 5, 4, 1, 6} {
		var immediate amd64.Asm
		immediate.AluRI(digit, amd64.RDI, -1, true)
		if !bytes.Contains(output.Code, immediate.B) {
			t.Fatalf("structured constant binary digit %d did not use an immediate: %x", digit, output.Code)
		}
	}
	var multiply amd64.Asm
	multiply.ImulRRI(amd64.RDI, amd64.RDI, -1, true)
	if !bytes.Contains(output.Code, multiply.B) {
		t.Fatalf("structured constant multiply did not use an immediate: %x", output.Code)
	}
}

func TestAMD64StructuredCombinesVectorLocalWithConstantWithoutCopy(t *testing.T) {
	body := []byte{0x20, 0x00, 0xfd, 0x0c} // local.get 0; v128.const
	body = append(body, bytes.Repeat([]byte{0x7f}, 16)...)
	body = append(body, 0xfd, 0x4e, 0x0b) // v128.and; end
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var redundant amd64.Asm
	redundant.VMovdqu(4, 8)
	if bytes.Contains(output.Code, redundant.B) {
		t.Fatalf("structured local/constant operation copied its pinned local: %x", output.Code)
	}
}

func TestAMD64StructuredUsesPinnedVectorLocalAsBinaryOperand(t *testing.T) {
	body := []byte{0x20, 0x00, 0xfd, 0x0c} // local.get 0; v128.const
	body = append(body, bytes.Repeat([]byte{0x7f}, 16)...)
	body = append(body,
		0xfd, 0x51, // v128.xor
		0x20, 0x01, // local.get 1
		0xfd, 0x4e, // v128.and
		0x0b,
	)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	output := compileAMD64EmissionTest(t, source)
	var redundant amd64.Asm
	redundant.VMovdqu(5, 9)
	if bytes.Contains(output.Code, redundant.B) {
		t.Fatalf("structured binary operation copied its pinned right operand: %x", output.Code)
	}
}

func TestAMD64TernaryBooleanImmediate(t *testing.T) {
	tests := []struct {
		inner wasm.InstrKind
		outer wasm.InstrKind
		want  byte
	}{
		{wasm.InstrV128And, wasm.InstrV128Xor, 0x78},
		{wasm.InstrV128Or, wasm.InstrV128Or, 0xfe},
		{wasm.InstrV128And, wasm.InstrV128And, 0x80},
		{wasm.InstrV128Xor, wasm.InstrV128Xor, 0x96},
	}
	for _, test := range tests {
		got, ok := amd64TernaryBooleanImmediate(test.inner, test.outer)
		if !ok || got != test.want {
			t.Fatalf("inner %s outer %s = (%#x, %v), want (%#x, true)", test.inner, test.outer, got, ok, test.want)
		}
	}
	if _, ok := amd64TernaryBooleanImmediate(wasm.InstrI32x4Add, wasm.InstrV128Or); ok {
		t.Fatal("non-boolean inner operation selected ternary logic")
	}
}

func TestAMD64StructuredTernaryLogicRequiresAVX512VL(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x7b, // one v128 local
		0x20, 0x00, 0x20, 0x01, 0x20, 0x02,
		0x02, 0x40, 0x0b, // keep the three operands while ending local/local lookahead
		0xfd, 0x4e, // v128.and
		0xfd, 0x51, // v128.xor
		0x21, 0x03, // local.set 3
		0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0
	}
	body = append(body, bytes.Repeat([]byte{0x01}, 510)...)
	body = append(body, 0x20, 0x03, 0x0b)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	compile := func(features uint64) corecompiler.Output {
		target := corecompiler.Target{GOOS: "linux", GOARCH: "amd64", Mode: corecompiler.TargetExplicit, FeatureBits: [4]uint64{features}}
		output, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
		if err != nil {
			t.Fatal(err)
		}
		return output
	}
	containsTernary := func(code []byte) bool {
		for offset := 0; offset+6 < len(code); offset++ {
			if code[offset] == 0x62 && code[offset+4] == 0x25 && code[offset+6] == 0x78 {
				return true
			}
		}
		return false
	}
	compat := compile(0)
	if compat.RequiresAVX512VL || containsTernary(compat.Code) {
		t.Fatalf("compatibility lowering selected AVX-512VL: requires=%v code=%x", compat.RequiresAVX512VL, compat.Code)
	}
	native := compile(uint64(1) << corecompiler.TargetFeatureAMD64AVX512VL)
	if !native.RequiresAVX512VL || !containsTernary(native.Code) {
		t.Fatalf("native lowering omitted AVX-512VL requirement or ternary instruction: requires=%v code=%x", native.RequiresAVX512VL, native.Code)
	}
}

func TestAMD64StructuredLoadsUnpinnedVectorLocalIntoStackCache(t *testing.T) {
	body := []byte{0x01, 0x09, 0x7b} // nine v128 locals
	for local := byte(1); local < 9; local++ {
		body = append(body, 0x20, local, 0x1a) // local.get; drop keeps the first eight locals hotter
	}
	body = append(body, 0x20, 0x09, 0x0b) // return the unpinned ninth local
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	output := compileAMD64EmissionTest(t, source)
	var redundant amd64.Asm
	redundant.VMovdquLoadDisp(0, amd64.RSP, 9*16)
	redundant.VMovdqu(4, 0)
	if bytes.Contains(output.Code, redundant.B) {
		t.Fatalf("structured local load copied through scratch before the stack cache: %x", output.Code)
	}
}

func TestAMD64StructuredWritesReloadedSIMDBinaryIntoStackCache(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x20, 0x01, // local.get 1
			0x02, 0x40, 0x0b, // empty block canonicalizes the live stack prefix
			0xfd, 0x4e, // v128.and
			0x0b,
		}))),
	)
	output := compileAMD64EmissionTest(t, source)
	var redundant amd64.Asm
	redundant.VMovdqu(4, 0)
	if bytes.Contains(output.Code, redundant.B) {
		t.Fatalf("structured SIMD result copied from scratch into its stack cache: %x", output.Code)
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

func TestAMD64StructuredCoalescesLocalBoundsChecksAcrossPureArithmetic(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0xfd, 0x00, 0x04, 0x00, 0x1a,
			0x41, 0x01, 0x41, 0x02, 0x6a, 0x1a,
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
	if ends[1] != 32 || !elided[8] {
		t.Fatalf("coalesced checks across arithmetic: ends=%v elided=%v", ends, elided)
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

func countAMD64VPtestRIP(code []byte) int {
	count := 0
	for i := 0; i+4 < len(code); i++ {
		if code[i] == 0xc4 && code[i+1]&0x1f == 0x02 && code[i+3] == 0x17 && code[i+4]&0xc7 == 0x05 {
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
	if !amd64RailMachCandidate(stack, false, true) {
		t.Fatal("nested-loop dense-global call helper was rejected")
	}
	stack.MaxLoopDepth = 0
	stack.HasReferences = false
	stack.Instrs = make([]railssa.StackInstr, 1025)
	if !amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large acyclic parameterless function was rejected")
	}
	stack.MaxLoopDepth = 2
	if !amd64RailMachCandidate(stack, false, false) {
		t.Fatal("large nested-loop parameterless function was rejected")
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

func TestAMD64RailMachAdmitsDenseGlobalNestedLoops(t *testing.T) {
	withoutGlobals := &railssa.StackFunc{MaxLoopDepth: 2, Instrs: []railssa.StackInstr{{Kind: wasm.InstrI64Add}}}
	if !amd64RailMachCandidate(withoutGlobals, false, true) {
		t.Fatal("global-free nested loop was rejected in a dense-global module")
	}
	withGlobals := &railssa.StackFunc{MaxLoopDepth: 2, Instrs: []railssa.StackInstr{{Kind: wasm.InstrGlobalGet}}}
	if !amd64RailMachCandidate(withGlobals, false, true) {
		t.Fatal("global-backed nested loop was rejected in a dense-global module")
	}
}

func TestAMD64StructuredOverwrittenLocalsStopsAtControlBoundary(t *testing.T) {
	for _, test := range []struct {
		name   string
		instrs []railssa.StackInstr
		want   bool
	}{
		{name: "entry write", instrs: []railssa.StackInstr{{Kind: wasm.InstrLocalSet}}, want: true},
		{name: "entry read", instrs: []railssa.StackInstr{{Kind: wasm.InstrLocalGet}}},
		{name: "write after control", instrs: []railssa.StackInstr{{Kind: wasm.InstrLoop}, {Kind: wasm.InstrLocalSet}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stack := &railssa.StackFunc{Locals: []wasm.ValType{wasm.V128}, Instrs: test.instrs}
			overwritten := amd64StructuredOverwrittenLocals(stack)
			if len(overwritten) != 1 || overwritten[0] != test.want {
				t.Fatalf("overwritten locals = %v, want %t", overwritten, test.want)
			}
		})
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
	optimized, _, _, _, err := emitAMD64(fn, plan, nil, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, _, _, err := emitAMD64(fn, nil, nil, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized) >= len(checked) {
		t.Fatalf("optimized bytes=%d checked bytes=%d", len(optimized), len(checked))
	}
}

func TestAMD64ProductionConsumesMaskedRangeBoundsElision(t *testing.T) {
	fn, plan := maskedMemoryEmissionTestFunc(t)
	optimized, _, _, _, err := emitAMD64(fn, plan, nil, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, _, _, err := emitAMD64(fn, nil, nil, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized) >= len(checked) {
		t.Fatalf("optimized bytes=%d checked bytes=%d", len(optimized), len(checked))
	}
}

func TestAMD64ProductionConsumesMaskedInductionBoundsElision(t *testing.T) {
	fn, plan := maskedLoopMemoryEmissionTestFunc(t)
	optimized, _, _, _, err := emitAMD64(fn, plan, nil, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked, _, _, _, err := emitAMD64(fn, nil, nil, false, nil, nil, nil)
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
	checkedMachine := *optimizedPlan.Machine
	checkedMachine.Memory = append([]railmach.MemoryAccess(nil), optimizedPlan.Machine.Memory...)
	if err := railmach.BindBoundsProofs(&checkedMachine, nil); err != nil {
		t.Fatal(err)
	}
	checkedPlan.Machine = &checkedMachine
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
	for _, producer := range plan.PostRAMemoryFrom.narrow {
		folded = folded || producer != 0
	}
	for _, producer := range plan.PostRAMemoryFrom.wide {
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
		semanticOp := railmach.SemanticOpcode(instruction.Op)
		if semanticOp >= wasm.InstrI32TruncSatF32S && semanticOp <= wasm.InstrI64TruncSatF64U &&
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
