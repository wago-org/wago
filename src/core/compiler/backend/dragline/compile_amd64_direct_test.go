//go:build amd64

package dragline

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/codegen/amd64"
	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestAMD64RailMachDerivesMixedBoundsFromCachedLimit(t *testing.T) {
	plan := &nativeBackendPlan{
		Stack:               &railssa.StackFunc{MemoryMinBytes: 1 << 16, Instrs: []railssa.StackInstr{{Offset: 7}}},
		Machine:             &railmach.Func{Insts: []railmach.Inst{{Source: 0}}},
		AMD64MemoryBoundEnd: 8,
	}
	var got amd64.Asm
	var patches []nativeBranchPatch
	emitAMD64RailMachBoundsCheck(&got, plan, amd64.R10, 64, 0, &patches, false)
	var want amd64.Asm
	want.LeaDisp(amd64.RSI, amd64.R12, -56)
	want.Cmp64(amd64.R10, amd64.RSI)
	if !bytes.HasPrefix(got.B, want.B) {
		t.Fatalf("mixed cached bound = %x, want prefix %x", got.B, want.B)
	}
	if len(patches) != 1 || patches[0].Target != 7 || patches[0].Code != 3 {
		t.Fatalf("bounds trap patches = %#v", patches)
	}
}

func TestAMD64RegionalReloadSurvivesFoldedInstruction(t *testing.T) {
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
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	var load uint32
	found := false
	for id, inst := range plan.Machine.Insts {
		if railmach.SemanticOpcode(inst.Op) == wasm.InstrI32Load && plan.PostRASkip.has(uint32(id)) {
			load, found = uint32(id), true
		}
	}
	if !found {
		t.Fatal("fixture must fold its load into the add")
	}
	value := plan.Machine.InstructionOperands(load)[0].Reg
	position := plan.Allocation.InstructionPositions[load]*6 + 2
	// Force the allocator product that exposed the corpus failure: a spilled
	// address becomes resident exactly at the eliminated load. Reserve a
	// separate spill home so the emitted reload has an unambiguous encoding.
	slot := plan.Allocation.SpillSlots
	plan.Allocation.SpillSlots++
	plan.Allocation.Locations[value] = railmach.Location{Kind: railmach.LocationSpill, Bank: railmach.BankGPR, Index: slot}
	plan.Allocation.Fragments = []railmach.AllocationFragment{{
		Reg: value, Start: position, End: position + 6,
		Location: railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 3},
	}}
	plan.Frame.SpillBytes += 16
	plan.Frame.TotalBytes += 16
	code, _, ok, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !ok {
		t.Fatalf("emit: admitted=%v err=%v", ok, err)
	}
	var reload amd64.Asm
	reload.LoadRsp32(amd64.R8, int32(slot)*8)
	if !bytes.Contains(code, reload.B) {
		t.Fatalf("folded load lost regional entry reload %x in %x", reload.B, code)
	}
}

func TestAMD64VariableShiftUsesColdRematerializedLHS(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x01, 0x20, 0x00, 0x74, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	target.FeatureBits[0] &^= uint64(1) << uint16(corecompiler.TargetFeatureAMD64BMI2)
	fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
	if err != nil {
		t.Fatal(err)
	}
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	var shift uint32
	found := false
	for id, inst := range plan.Machine.Insts {
		if railmach.SemanticOpcode(inst.Op) == wasm.InstrI32Shl {
			shift, found = uint32(id), true
		}
	}
	if !found {
		t.Fatal("missing variable shift")
	}
	operands := plan.Machine.InstructionOperands(shift)
	lhs := operands[0].Reg
	// A cold use is rematerialized into RSI even when its original allocation
	// was RCX. RCX may already contain the count by this scheduled position.
	operands[0].Flags |= railmach.OperandColdRemat
	plan.Machine.VRegs[lhs].Flags |= railmach.VRegElided
	plan.Allocation.Locations[lhs] = railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 1}
	code, _, ok, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !ok {
		t.Fatalf("emit: admitted=%v err=%v", ok, err)
	}
	pos := plan.Allocation.InstructionPositions[shift]*6 + 2
	dst := amd64RailMachPhysical(plan, plan.Allocation.LocationAt(plan.Machine.Insts[shift].Result, pos))
	if dst == amd64.RCX {
		dst = amd64.R10
	}
	var want amd64.Asm
	want.MovReg64(dst, amd64.RSI)
	want.ShiftCL(4, dst, false)
	if !bytes.Contains(code, want.B) {
		t.Fatalf("shift must consume rematerialized lhs: missing %x in %x", want.B, code)
	}
}

func TestAMD64RailMachUsesRelativeJumpTableForDenseBrTable(t *testing.T) {
	body := make([]byte, 0, 64)
	for range 9 {
		body = append(body, 0x02, 0x40) // block
	}
	body = append(body, 0x20, 0x00, 0x0e, 0x08)
	for label := byte(0); label < 8; label++ {
		body = append(body, label)
	}
	body = append(body, 0x08) // default
	for result := byte(0); result < 9; result++ {
		body = append(body, 0x0b, 0x41, result, 0x0f) // end; i32.const; return
	}
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
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
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	code, _, ok, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !ok {
		t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
	}
	entries := 0
	for _, patch := range plan.BranchPatches {
		if patch.Base != 0 {
			entries++
		}
	}
	if entries != 8 {
		t.Fatalf("relative jump-table entries = %d, want 8", entries)
	}
	var indirect amd64.Asm
	indirect.JmpReg(amd64.R10)
	if !bytes.Contains(code, indirect.B) {
		t.Fatalf("dense br_table has no indirect dispatch: %x", code)
	}
}

func TestAMD64RailMachUsesJumpTableThunksForBrTableEdgeMoves(t *testing.T) {
	body := make([]byte, 0, 96)
	for range 9 {
		body = append(body, 0x02, 0x7f) // block (result i32)
	}
	body = append(body, 0x20, 0x01, 0x20, 0x00, 0x0e, 0x08)
	for label := byte(0); label < 8; label++ {
		body = append(body, label)
	}
	body = append(body, 0x08) // default
	for range 9 {
		body = append(body, 0x0b, 0x0f) // end; return carried result
	}
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
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
	var planner nativeBackendPlanner
	plan, err := planner.Plan(fn.Structured, target)
	if err != nil {
		t.Fatal(err)
	}
	tableEdge := ^uint32(0)
	for blockID, block := range plan.CFG.Blocks {
		terminator := plan.Stack.Instrs[block.InstStart+block.InstCount-1]
		if terminator.Kind != wasm.InstrBrTable {
			continue
		}
		var ok bool
		tableEdge, ok = nativeBranchTableEdge(plan, uint32(blockID), terminator.Labels(plan.Stack)[0])
		if !ok {
			t.Fatal("br_table edge is unavailable")
		}
		break
	}
	if tableEdge == ^uint32(0) {
		t.Fatal("br_table block is unavailable")
	}
	reg := railmach.VReg(1)
	for int(reg) < len(plan.Machine.VRegs) && plan.Machine.VRegs[reg].Bank != railmach.BankGPR {
		reg++
	}
	if int(reg) == len(plan.Machine.VRegs) {
		t.Fatal("br_table plan has no GPR value")
	}
	plan.Exit.EdgeMoves[tableEdge] = railmach.MoveRange{Start: uint32(len(plan.Exit.Moves)), Count: 1}
	plan.Exit.Moves = append(plan.Exit.Moves, railmach.PhysicalMove{
		Src: railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 0},
		Dst: railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 1},
		Reg: reg, Edge: tableEdge, Kind: railmach.MoveCopy, Placement: railmach.PlacePredecessorEnd, Bank: railmach.BankGPR,
	})
	code, _, ok, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
	if err != nil || !ok {
		t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
	}
	var indirect amd64.Asm
	indirect.JmpReg(amd64.R10)
	if !bytes.Contains(code, indirect.B) {
		t.Fatalf("dense moving br_table has no indirect dispatch: %x", code)
	}
}

func TestAMD64RailMachReloadsCachedMemoryBoundOnlyAfterGrowingDirectCall(t *testing.T) {
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}
	caller := []byte{
		0x41, 0, 0x41, 0, 0x41, 0, 0x41, 0,
		0x41, 0, 0x41, 0, 0x41, 0, 0x41, 0,
		0x10, 0, 0x1a,
	}
	for range 16 {
		caller = append(caller, 0x20, 0, 0x28, 2, 0, 0x1a)
	}
	caller = append(caller, 0x20, 0, 0x28, 2, 0, 0x0b)
	callerBytes := 0
	for _, tc := range []struct {
		name       string
		callee     []byte
		wantReload int
	}{
		{name: "non-growing", callee: []byte{0x20, 7, 0x0b}, wantReload: 1},
		{name: "growing", callee: []byte{0x20, 7, 0x40, 0x00, 0x0b}, wantReload: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					wasmtest.FuncType(params, []wasm.ValType{wasm.I32}),
					wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
				)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code(nonInlinableLeafBody(tc.callee)),
					wasmtest.Code(caller),
				)),
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
			output, err := (Compiler{}).Compile(corecompiler.Input{
				Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
				Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
			})
			if err != nil {
				t.Fatal(err)
			}
			body := output.Code[output.InternalEntry[1]:]
			var reload amd64.Asm
			reload.Load64(amd64.R12, amd64.RBX, -int32(runtimeabi.ActualLinMemByteSize64Offset))
			reload.AluRI(5, amd64.R12, 4, true)
			if got := bytes.Count(body, reload.B); got != tc.wantReload {
				t.Fatalf("cached memory-bound reloads = %d, want %d in %x", got, tc.wantReload, body)
			}
			if callerBytes == 0 {
				callerBytes = len(body)
			} else if len(body) != callerBytes {
				t.Fatalf("caller bytes = %d, want layout-preserving %d", len(body), callerBytes)
			}
		})
	}
}

func TestAMD64ImmutableInlineIndirectAvoidsCallAreaMarshalling(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x02, 0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(
			// Nops and encoding details must not affect the decoded semantic proof.
			wasmtest.Code([]byte{0x01, 0x20, 0, 0x20, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6b, 0x0b}),
			wasmtest.Code([]byte{0x20, 1, 0x20, 2, 0x20, 0, 0x11, 0, 0, 0x0b}),
		)),
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
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsSignals,
		ConfigurationFingerprint: [32]byte{4},
	}
	for _, workers := range []int{1, 2} {
		t.Run(map[int]string{1: "sequential", 2: "parallel"}[workers], func(t *testing.T) {
			input.FunctionWorkers = workers
			output, err := (Compiler{}).Compile(input)
			if err != nil {
				t.Fatal(err)
			}
			if len(output.InternalEntry) != 3 {
				t.Fatalf("internal entries = %v", output.InternalEntry)
			}
			if !output.PreparedIsolatedTables {
				t.Fatal("immutable local table proof was not published")
			}
			body := output.Code[output.InternalEntry[2]:]
			var callArea amd64.Asm
			callArea.StoreRsp64(0, amd64.RCX)
			callArea.StoreRsp64(8, amd64.RDX)
			callArea.StoreRsp64(16, amd64.RAX)
			if bytes.Contains(body, callArea.B) {
				t.Fatalf("immutable inline indirect retained generic call-area marshalling: %x", body)
			}
		})
	}
}

func TestAMD64PublishesDirectPreparedLeafAcrossCompilerPaths(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x6a, 0x0b}))),
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
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsSignals,
		ConfigurationFingerprint: [32]byte{1},
	}
	assertDirect := func(t *testing.T, output corecompiler.Output) {
		t.Helper()
		if len(output.DirectPrepared) == 0 || output.DirectPrepared[0]&1 == 0 {
			t.Fatal("AMD64 output omitted direct prepared metadata")
		}
		if len(output.DirectLeafPrepared) == 0 || output.DirectLeafPrepared[0]&1 == 0 {
			t.Fatal("AMD64 output omitted direct leaf metadata")
		}
		if len(output.ContextFreeLoopPrepared) != 0 && output.ContextFreeLoopPrepared[0]&1 != 0 {
			t.Fatal("AMD64 leaf redundantly published signal-guard-free call metadata")
		}
	}

	for _, workers := range []int{1, 2} {
		t.Run(map[int]string{1: "sequential", 2: "parallel"}[workers], func(t *testing.T) {
			input.FunctionWorkers = workers
			output, err := (Compiler{}).Compile(input)
			if err != nil {
				t.Fatal(err)
			}
			assertDirect(t, output)
			if len(output.DirectPreparedBounded) == 0 || output.DirectPreparedBounded[0]&1 == 0 {
				t.Fatal("AMD64 output omitted bounded prepared-entry metadata")
			}
		})
	}

	input.FunctionWorkers = 1
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	compiler := Compiler{FunctionCache: cache}
	for _, name := range []string{"cold-cache", "warm-cache"} {
		t.Run(name, func(t *testing.T) {
			output, err := compiler.Compile(input)
			if err != nil {
				t.Fatal(err)
			}
			assertDirect(t, output)
		})
	}
}

func TestAMD64PublishesPreparedCallWithoutLeafMetadata(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("tick")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x20, 0x00, 0x0b}))),
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
	input := corecompiler.Input{Module: module, Source: source, Target: target, ConfigurationFingerprint: [32]byte{2}}
	assertDirectCall := func(t *testing.T, output corecompiler.Output) {
		t.Helper()
		if len(output.DirectPrepared) == 0 || output.DirectPrepared[0]&1 == 0 {
			t.Fatal("AMD64 output omitted prepared-call metadata")
		}
		if len(output.DirectLeafPrepared) != 0 && output.DirectLeafPrepared[0]&1 != 0 {
			t.Fatal("AMD64 call-bearing function was published as a direct leaf")
		}
		if len(output.ContextFreeLoopPrepared) != 0 && output.ContextFreeLoopPrepared[0]&1 != 0 {
			t.Fatal("AMD64 imported-call function was published as signal-guard-free")
		}
	}
	for _, workers := range []int{1, 2} {
		input.FunctionWorkers = workers
		output, err := (Compiler{}).Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		assertDirectCall(t, output)
	}
	input.FunctionWorkers = 1
	compiler := Compiler{FunctionCache: corecompiler.NewFunctionArtifactCache(1 << 20)}
	for range 2 {
		output, err := compiler.Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		assertDirectCall(t, output)
	}
}

func TestAMD64PublishesTransitiveSignalGuardFreeClosure(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x6a, 0x0b}),
		)),
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
	input := corecompiler.Input{Module: module, Source: source, Target: target, Bounds: corecompiler.BoundsSignals, ConfigurationFingerprint: [32]byte{3}}
	assertClosure := func(t *testing.T, output corecompiler.Output) {
		t.Helper()
		if len(output.ContextFreeLoopPrepared) == 0 || output.ContextFreeLoopPrepared[0]&0x3 != 0x1 {
			t.Fatalf("AMD64 signal-guard-free metadata = %x, want only call-bearing root", output.ContextFreeLoopPrepared)
		}
	}
	for _, workers := range []int{1, 2} {
		input.FunctionWorkers = workers
		output, err := (Compiler{}).Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		assertClosure(t, output)
	}
	input.FunctionWorkers = 1
	compiler := Compiler{FunctionCache: corecompiler.NewFunctionArtifactCache(1 << 20)}
	for range 2 {
		output, err := compiler.Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		assertClosure(t, output)
	}
}

func TestAMD64BMI2VariableShiftsReleaseCountRegister(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  wasm.ValType
		op   byte
	}{
		{"i32.shl", wasm.I32, 0x74}, {"i32.shr_s", wasm.I32, 0x75}, {"i32.shr_u", wasm.I32, 0x76},
		{"i64.shl", wasm.I64, 0x86}, {"i64.shr_s", wasm.I64, 0x87}, {"i64.shr_u", wasm.I64, 0x88},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{tc.typ, tc.typ}, []wasm.ValType{tc.typ}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, tc.op, 0x0b}))),
			)
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			target, err := corecompiler.HostTarget(corecompiler.TargetNative)
			if err != nil {
				t.Fatal(err)
			}
			for _, bmi2 := range []bool{false, true} {
				target.FeatureBits[0] &^= uint64(1) << uint16(corecompiler.TargetFeatureAMD64BMI2)
				if bmi2 {
					target.FeatureBits[0] |= uint64(1) << uint16(corecompiler.TargetFeatureAMD64BMI2)
				}
				fn, err := buildCompilerFunc(m, 0, &railssa.StackFunc{})
				if err != nil {
					t.Fatal(err)
				}
				var planner nativeBackendPlanner
				plan, err := planner.Plan(fn.Structured, target)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for id, inst := range plan.Machine.Insts {
					if !amd64BMI2ShiftKind(railmach.SemanticOpcode(inst.Op)) {
						continue
					}
					found = true
					operands := plan.Machine.InstructionOperands(uint32(id))
					fixed := operands[1].Flags&railmach.OperandFixed != 0
					if fixed == bmi2 {
						t.Fatalf("BMI2=%v: count fixed=%v", bmi2, fixed)
					}
				}
				if !found {
					t.Fatal("missing shift")
				}
				if got := amd64RailMachMayUseBMI2(plan); got != bmi2 {
					t.Fatalf("BMI2=%v: artifact requirement=%v", bmi2, got)
				}
				code, _, ok, err := emitAMD64RailMach(fn, plan, nil, nil, nil)
				if err != nil || !ok {
					t.Fatalf("emit: %v %v", ok, err)
				}
				hasBMI2 := false
				for i := 0; i+4 < len(code); i++ {
					if code[i] == 0xc4 && code[i+1]&0x1f == 2 && code[i+3] == 0xf7 {
						hasBMI2 = true
					}
				}
				if hasBMI2 != bmi2 {
					t.Fatalf("BMI2=%v: shift encoding=%x", bmi2, code)
				}
			}
		})
	}
}

func TestAMD64PublishesFoldedRotateBMI2Requirement(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x78, 0x0b}))),
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
	if !target.HasFeature(corecompiler.TargetFeatureAMD64BMI2) {
		t.Skip("host does not support BMI2")
	}
	input := corecompiler.Input{Module: module, Source: source, Target: target, ConfigurationFingerprint: [32]byte{4}}
	for _, workers := range []int{1, 2} {
		input.FunctionWorkers = workers
		output, err := (Compiler{}).Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		if !output.RequiresBMI2 {
			t.Fatalf("workers %d: folded rotate did not publish BMI2 requirement", workers)
		}
	}
	input.FunctionWorkers = 1
	compiler := Compiler{FunctionCache: corecompiler.NewFunctionArtifactCache(1 << 20)}
	for pass := range 2 {
		output, err := compiler.Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		if !output.RequiresBMI2 {
			t.Fatalf("cache pass %d: folded rotate lost BMI2 requirement", pass)
		}
	}
	target.FeatureBits[uint16(corecompiler.TargetFeatureAMD64BMI2)/64] &^= uint64(1) << (uint16(corecompiler.TargetFeatureAMD64BMI2) % 64)
	input.Target = target
	input.ConfigurationFingerprint = [32]byte{5}
	output, err := (Compiler{}).Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	if output.RequiresBMI2 {
		t.Fatal("baseline target published a BMI2 requirement")
	}
}

func TestAMD64StructuredWritesSIMDBinaryDirectlyToTeeLocal(t *testing.T) {
	source := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x07, 0x01, 0x60, 0x02, 0x7b, 0x7b, 0x01, 0x7b,
		0x03, 0x02, 0x01, 0x00,
		0x0a, 0x12, 0x01, 0x10, 0x01, 0x01, 0x7b,
		0x20, 0x00, 0x20, 0x01, 0xfd, 0x51,
		0x20, 0x00, 0xfd, 0x51, 0x22, 0x02, 0x0b,
	}
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
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: module, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if got := metrics.Functions[0].NativeBytes; got > 200 {
		t.Fatalf("direct SIMD tee emitted %d bytes, want at most 200", got)
	}
}

func TestAMD64LateFloatMemoryFoldWindow(t *testing.T) {
	for _, tc := range []struct {
		name               string
		middle             []byte
		op                 byte
		live, shared, want bool
	}{
		{"add", nil, 0xa0, true, false, true},
		{"multiply", nil, 0xa2, true, false, true},
		{"subtract left", nil, 0xa1, true, false, false},
		{"address dies", nil, 0xa0, false, false, false},
		{"store barrier", []byte{0x41, 0, 0x20, 2, 0x39, 3, 0}, 0xa0, true, false, false},
		{"division barrier", []byte{0x41, 1, 0x41, 0, 0x6e, 0x1a}, 0xa0, true, false, false},
		{"shared memory", nil, 0xa0, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0x20, 0, 0x2b, 3, 0}
			body = append(body, tc.middle...)
			body = append(body, 0x20, 2, 0x20, 1, 0x2b, 3, 0, 0xa2, tc.op)
			if tc.live {
				body = append(body, 0x20, 0, 0x41, 63, 0x71, 0x41, 42, 0x3a, 0, 0)
			}
			body = append(body, 0x0b)
			fn, plan := simdAddressTestPlan(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.F64}, []wasm.ValType{wasm.F64}, body)
			if tc.shared {
				fn.Structured.Module.Memories[0].Shared = true
				fn.Structured.Module.Memories[0].Limits.HasMax = true
				fn.Structured.Module.Memories[0].Limits.Max = 1
				target, err := corecompiler.HostTarget(corecompiler.TargetNative)
				if err != nil {
					t.Fatal(err)
				}
				plan, err = (&nativeBackendPlanner{}).Plan(fn.Structured, target)
				if err != nil {
					t.Fatal(err)
				}
			}
			first := ^uint32(0)
			for id, in := range plan.Machine.Insts {
				if railmach.SemanticOpcode(in.Op) == wasm.InstrF64Load {
					first = uint32(id)
					break
				}
			}
			if first == ^uint32(0) {
				t.Fatal("missing fixture load")
			}
			folded := false
			for id := range plan.Machine.Insts {
				if source, ok := plan.PostRAMemoryFrom.get(uint32(id)); ok && source == first {
					folded = true
				}
			}
			if folded != tc.want {
				t.Fatalf("first load folded=%v, want %v", folded, tc.want)
			}
		})
	}
}
