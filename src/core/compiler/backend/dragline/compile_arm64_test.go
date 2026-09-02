//go:build arm64

package dragline

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"runtime"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestARM64BoundsImmediateHelpers(t *testing.T) {
	var a arm64.Asm
	emitARM64BoundsEnd(&a, arm64.X3, 8)
	if len(a.B) != 4 {
		t.Fatalf("small bounds end emitted %d bytes, want 4", len(a.B))
	}
	a.B = a.B[:0]
	if !emitARM64BoundsLimit(&a, arm64.X4, arm64.X5, 8, 64<<10) || len(a.B) != 4 {
		t.Fatalf("small bounds limit emitted %d bytes", len(a.B))
	}
	a.B = a.B[:0]
	if emitARM64BoundsLimit(&a, arm64.X4, arm64.X5, 64<<10, 32<<10) || len(a.B) != 0 {
		t.Fatalf("underflowing bounds limit was emitted: %x", a.B)
	}
}

func TestARM64ClosesUnsignedI64GlobalSumLoop(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0x00, 0x0b}))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x40, 0x03, 0x40, // block; loop
			0x20, 0x00, 0x45, 0x0d, 0x01, // local.get 0; i32.eqz; br_if 1
			0x23, 0x00, 0x20, 0x00, 0xad, 0x7c, 0x24, 0x00, // global += i64.extend_i32_u(n)
			0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, 0x0c, 0x00, // n--; br 0
			0x0b, 0x0b, 0x23, 0x00, 0x0b, // end; end; global.get 0; end
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
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	got := metrics.Functions[0]
	if !got.RailMachFinalized || got.PostRARewrites == 0 || got.NativeBytes > 192 {
		t.Fatalf("closed unsigned i64 global sum metrics = %#v", got)
	}
}

func TestARM64WhitespaceEndGuardRequiresMatchingLocals(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x40, // block
			0x20, 0x00, // local.get 0
			0x20, 0x01, // local.get 1
			0x4f,       // i32.ge_u
			0x0d, 0x00, // br_if 0
			0x0b, // end block
			0x0b, // end function
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
	label, next, ok := arm64WhitespaceEndGuard(stack.Instrs, 1, 0, 1)
	if !ok || label != 0 || next != 5 {
		t.Fatalf("guard = label %d, next %d, ok %t", label, next, ok)
	}
	if _, _, ok := arm64WhitespaceEndGuard(stack.Instrs, 1, 0, 0); ok {
		t.Fatal("guard with a different end local was accepted")
	}
	stack.Instrs[3].Kind = wasm.InstrI32GtU
	if _, _, ok := arm64WhitespaceEndGuard(stack.Instrs, 1, 0, 1); ok {
		t.Fatal("guard with a different comparison was accepted")
	}
}

func TestARM64RailMachTopLevelI32LTGuardRequiresWholeVoidBody(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x02, // local.get 2
			0x20, 0x03, // local.get 3
			0x48,       // i32.lt_s
			0x04, 0x40, // if
			0x20, 0x00, // local.get 0
			0x20, 0x01, // local.get 1
			0x20, 0x02, // local.get 2
			0x20, 0x03, // local.get 3
			0x10, 0x00, // call 0
			0x0b, // end if
			0x0b, // end function
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
	lhs, rhs, ok := arm64RailMachTopLevelI32LTGuard(plan)
	if !ok || lhs != arm64.X2 || rhs != arm64.X3 {
		t.Fatalf("top-level guard = %d, %d, %t; want X2, X3, true", lhs, rhs, ok)
	}
	region := plan.Stack.Regions[0]
	plan.Stack.Regions[0].ElseInstr = region.StartInstr + 1
	if _, _, ok := arm64RailMachTopLevelI32LTGuard(plan); ok {
		t.Fatal("guard with else was accepted")
	}
	plan.Stack.Regions[0] = region
	plan.Stack.Results = []wasm.ValType{wasm.I32}
	if _, _, ok := arm64RailMachTopLevelI32LTGuard(plan); ok {
		t.Fatal("result-bearing function guard was accepted")
	}
}

func TestARM64AddSubImmediateHelpersCanonicalizeShiftedAndNegativeConstants(t *testing.T) {
	var a arm64.Asm
	if !emitARM64I32AddSubImmediate(&a, arm64.X2, arm64.X3, 0xfff00000, true) || !bytes.Equal(a.B, []byte{0x62, 0x00, 0x44, 0x11}) {
		t.Fatalf("i32 subtract-negative immediate = %x", a.B)
	}
	a.B = a.B[:0]
	if !emitARM64I64AddSubImmediate(&a, arm64.X2, arm64.X3, 1048576, false) || !bytes.Equal(a.B, []byte{0x62, 0x00, 0x44, 0x91}) {
		t.Fatalf("i64 shifted add immediate = %x", a.B)
	}
	a.B = a.B[:0]
	if emitARM64I32AddSubImmediate(&a, arm64.X2, arm64.X3, 4097, false) || len(a.B) != 0 {
		t.Fatalf("unencodable i32 immediate emitted %d bytes", len(a.B))
	}
}

func TestARM64RailMachMulHighLoopRequiresCompletePortableIdiom(t *testing.T) {
	body := make([]byte, 0, 160)
	bytecode := func(op byte) { body = append(body, op) }
	local := func(op byte, index uint32) {
		body = append(body, op)
		body = append(body, wasmtest.ULEB(index)...)
	}
	i64const := func(value int64) {
		body = append(body, 0x42)
		body = append(body, wasmtest.SLEB64(value)...)
	}
	body = append(body, 0x03, 0x40)
	local(0x20, 0)
	local(0x20, 6)
	body = append(body, 0x4a, 0x04, 0x40)
	local(0x20, 6)
	bytecode(0xac)
	local(0x22, 3)
	i64const(7046029254386353131)
	bytecode(0x7d)
	local(0x22, 4)
	i64const(32)
	bytecode(0x88)
	local(0x22, 2)
	local(0x20, 3)
	i64const(-6884282663029611473)
	bytecode(0x7e)
	i64const(-2960836687051489901)
	bytecode(0x85)
	local(0x22, 3)
	i64const(0xffffffff)
	bytecode(0x83)
	local(0x22, 5)
	bytecode(0x7e)
	local(0x20, 4)
	i64const(0xffffffff)
	bytecode(0x83)
	local(0x22, 4)
	local(0x20, 5)
	bytecode(0x7e)
	i64const(32)
	body = append(body, 0x88, 0x7c)
	local(0x21, 5)
	local(0x20, 1)
	local(0x20, 3)
	i64const(32)
	bytecode(0x88)
	local(0x22, 1)
	local(0x20, 2)
	bytecode(0x7e)
	local(0x20, 5)
	i64const(32)
	body = append(body, 0x88, 0x7c)
	local(0x20, 1)
	local(0x20, 4)
	bytecode(0x7e)
	local(0x20, 5)
	i64const(0xffffffff)
	body = append(body, 0x83, 0x7c)
	i64const(32)
	body = append(body, 0x88, 0x7c, 0x85)
	local(0x21, 1)
	local(0x20, 6)
	body = append(body, 0x41)
	body = append(body, wasmtest.SLEB32(1)...)
	bytecode(0x6a)
	local(0x21, 6)
	body = append(body, 0x0c, 0x01, 0x0b, 0x0b)
	local(0x20, 1)
	bytecode(0x0b)

	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: body}}}
	plan := &nativeBackendPlan{
		Stack: &railssa.StackFunc{Module: m, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I64}},
		Machine: &railmach.Func{
			VRegs:   []railmach.VRegData{{}, {Type: railmach.TypeI32, Bank: railmach.BankGPR, Flags: railmach.VRegInitial}, {Type: railmach.TypeI64, Bank: railmach.BankGPR}},
			Results: []railmach.VReg{2},
		},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: []railmach.Location{{}, {Kind: railmach.LocationRegister, Bank: railmach.BankGPR}, {Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 1}}}},
	}
	n, result, subtract, multiply, xor, ok := arm64RailMachMulHighLoop(plan)
	multiplyWant, xorWant := int64(-6884282663029611473), int64(-2960836687051489901)
	if !ok || n != 1 || result != 2 || subtract != 7046029254386353131 || multiply != uint64(multiplyWant) || xor != uint64(xorWant) {
		t.Fatalf("mulhi loop = n%d result%d constants=%#x/%#x/%#x ok=%t", n, result, subtract, multiply, xor, ok)
	}
	body[0] = 0x02
	if _, _, _, _, _, ok := arm64RailMachMulHighLoop(plan); ok {
		t.Fatal("changed loop opcode was recognized as the complete mulhi idiom")
	}
}

func TestARM64StructuredSIMDImmediateShiftMasksLaneCount(t *testing.T) {
	var a arm64.Asm
	if !emitARM64StructuredSIMDImmediateShift(&a, wasm.InstrI16x8ShrU, arm64.X2, arm64.X3, 20) || len(a.B) != 4 {
		t.Fatalf("i16x8.shr_u immediate emitted %x", a.B)
	}
	a.B = a.B[:0]
	if !emitARM64StructuredSIMDImmediateShift(&a, wasm.InstrI16x8Shl, arm64.X3, arm64.X3, 16) || len(a.B) != 0 {
		t.Fatalf("masked-zero i16x8 shift emitted %x", a.B)
	}
	if emitARM64StructuredSIMDImmediateShift(&a, wasm.InstrI8x16Add, arm64.X2, arm64.X3, 1) {
		t.Fatal("unsupported immediate SIMD shift was accepted")
	}
}

func TestARM64MixedSIMDModuleRailMachAdmission(t *testing.T) {
	leaf := &railssa.StackFunc{Instrs: []railssa.StackInstr{{Kind: wasm.InstrI32Add}}}
	if !arm64RailMachCandidate(leaf, true, nil) {
		t.Fatal("scalar leaf in SIMD module was not admitted")
	}
	largeLeaf := &railssa.StackFunc{Instrs: make([]railssa.StackInstr, 193)}
	for i := range largeLeaf.Instrs {
		largeLeaf.Instrs[i].Kind = wasm.InstrI32Add
	}
	if !arm64RailMachCandidate(largeLeaf, true, nil) {
		t.Fatal("large scalar leaf in SIMD module was rejected")
	}
	caller := &railssa.StackFunc{Instrs: []railssa.StackInstr{{Kind: wasm.InstrCall}}}
	if arm64RailMachCandidate(caller, true, nil) {
		t.Fatal("mixed-module RailMach caller was admitted before its frame contract is shared")
	}
	if !arm64RailMachCandidate(caller, false, nil) {
		t.Fatal("scalar-only RailMach caller was rejected")
	}
	if !arm64RailMachCandidate(caller, true, []railmach.ABIContract{{Class: railmach.ABIPreparedLeaf}}) {
		t.Fatal("bounded scalar caller with a prepared callee was rejected in a SIMD module")
	}
	trapCaller := &railssa.StackFunc{ImportedFuncs: 1, Instrs: []railssa.StackInstr{{Kind: wasm.InstrCall}, {Kind: wasm.InstrUnreachable}}}
	if !arm64RailMachCandidate(trapCaller, true, nil) {
		t.Fatal("cold imported trap call was rejected in a SIMD module")
	}
}

func TestARM64WindowsRetainsRailMachWithCanonicalPrivateABI(t *testing.T) {
	windows := corecompiler.Target{GOOS: "windows", GOARCH: "arm64"}
	linux := corecompiler.Target{GOOS: "linux", GOARCH: "arm64"}
	plan := &nativeBackendPlan{
		ABI:      railmach.ABIContract{Class: railmach.ABIPreparedCall, GPRClobbers: 3, CalleeGPRs: 2},
		LocalABI: railmach.ABIContract{Class: railmach.ABIPreparedLeaf},
		Calls:    []railmach.CallContract{{Class: railmach.ABIPreparedInt}, {Class: railmach.ABILeafScalar}},
	}
	arm64ConstrainPrivateABI(plan, linux)
	if plan.ABI.Class != railmach.ABIPreparedCall || plan.Calls[0].Class != railmach.ABIPreparedInt {
		t.Fatal("Linux private register ABI was constrained")
	}
	arm64ConstrainPrivateABI(plan, windows)
	if plan.ABI.Class != railmach.ABIGeneral || plan.LocalABI.Class != railmach.ABIGeneral || plan.Calls[0].Class != railmach.ABIGeneral {
		t.Fatalf("Windows retained widened private ABI: plan=%v local=%v call=%v", plan.ABI.Class, plan.LocalABI.Class, plan.Calls[0].Class)
	}
	if plan.Calls[1].Class != railmach.ABILeafScalar || plan.ABI.GPRClobbers != 3 || plan.ABI.CalleeGPRs != 2 {
		t.Fatal("Windows constraint changed ordinary ABI metadata")
	}
	contract := arm64ConstrainPrivateContract(railmach.ABIContract{Class: railmach.ABIPreparedLeaf, GPRClobbers: 7}, windows)
	if contract.Class != railmach.ABIGeneral || contract.GPRClobbers != 7 {
		t.Fatalf("Windows published contract = %#v", contract)
	}
}

func TestARM64UnboundedLoopStaysOffGoStack(t *testing.T) {
	plan := &nativeBackendPlan{
		Stack:   &railssa.StackFunc{MaxLoopDepth: 1},
		Machine: &railmach.Func{},
		ABI:     railmach.ABIContract{Class: railmach.ABIPreparedInt},
	}
	if arm64DirectPreparedLeafPlan(plan) {
		t.Fatal("unbounded loop published the non-interruptible Go-stack leaf entry")
	}
	if !arm64ContextFreePreparedLoop(plan.Stack) {
		t.Fatal("context-free unbounded loop did not publish the private-wrapper proof")
	}
	plan.Stack.Instrs = []railssa.StackInstr{{Kind: wasm.InstrMemorySize}}
	if arm64ContextFreePreparedLoop(plan.Stack) {
		t.Fatal("context-dependent loop published the context-free private-wrapper proof")
	}
}

func TestARM64ContextFreeLoopMarkerSurvivesFunctionCache(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}))),
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
	input := corecompiler.Input{Module: module, Source: source, Target: target}
	compiler := Compiler{FunctionCache: corecompiler.NewFunctionArtifactCache(1 << 20)}
	for _, pass := range []string{"cold", "warm"} {
		output, err := compiler.Compile(input)
		if err != nil {
			t.Fatalf("%s compile: %v", pass, err)
		}
		if len(output.ContextFreeLoopPrepared) == 0 || output.ContextFreeLoopPrepared[0]&1 == 0 {
			t.Fatalf("%s compile lost the context-free loop marker", pass)
		}
	}
}

func TestARM64PinV128LocalsUsesHottestLocals(t *testing.T) {
	types := make([]wasm.ValType, len(arm64V128LocalRegisters)+3)
	uses := make([]uint32, len(types))
	pinned := make([]bool, len(types))
	registers := make([]arm64.Reg, len(types))
	for i := range types {
		types[i] = wasm.V128
		uses[i] = uint32(i)
	}

	arm64PinV128Locals(types, uses, pinned, registers, arm64V128LocalRegisters[:])

	for i := 0; i < 3; i++ {
		if pinned[i] {
			t.Fatalf("cold local %d was pinned", i)
		}
	}
	for i := 3; i < len(types); i++ {
		if !pinned[i] {
			t.Fatalf("hot local %d was not pinned", i)
		}
		want := arm64V128LocalRegisters[len(types)-1-i]
		if registers[i] != want {
			t.Fatalf("local %d register = %d, want %d", i, registers[i], want)
		}
	}
}

func TestARM64CallPinnedV128RegistersAvoidOperandStack(t *testing.T) {
	for _, pinned := range arm64V128CallPinnedRegisters {
		for _, stack := range arm64V128StackRegisters {
			if pinned == stack {
				t.Fatalf("call-pinned V%d overlaps the structured operand stack", pinned)
			}
		}
	}
}

func TestARM64StructuredDirectCallSpillsOnlyLivePrefix(t *testing.T) {
	if got := arm64StructuredCallSpillLimit(wasm.InstrCall, 1, 1, 6, 4, true); got != 4 {
		t.Fatalf("direct call spill limit = %d, want live prefix 4", got)
	}
	if got := arm64StructuredCallSpillLimit(wasm.InstrCall, 1, 1, 6, 4, false); got != 6 {
		t.Fatalf("canonical direct call spill limit = %d, want complete argument vector 6", got)
	}
	if got := arm64StructuredCallSpillLimit(wasm.InstrCall, 0, 1, 6, 4, false); got != 6 {
		t.Fatalf("imported wrapper spill limit = %d, want complete canonical vector 6", got)
	}
	if got := arm64StructuredCallSpillLimit(wasm.InstrCallIndirect, 1, 1, 7, 4, false); got != 7 {
		t.Fatalf("indirect wrapper spill limit = %d, want complete canonical vector 7", got)
	}
}

func TestARM64StructuredDefersPromotedGlobalReloadAcrossAdjacentCalls(t *testing.T) {
	instrs := []railssa.StackInstr{
		{Kind: wasm.InstrCall},
		{Kind: wasm.InstrCall},
		{Kind: wasm.InstrCallIndirect},
		{Kind: wasm.InstrGlobalGet},
	}
	if !arm64StructuredCanDeferPromotedGlobalReload(instrs, 0) {
		t.Fatal("adjacent direct call did not defer the redundant reload")
	}
	if !arm64StructuredCanDeferPromotedGlobalReload(instrs, 1) {
		t.Fatal("adjacent indirect call did not defer the redundant reload")
	}
	if arm64StructuredCanDeferPromotedGlobalReload(instrs, 2) {
		t.Fatal("global access incorrectly deferred the required reload")
	}
	if arm64StructuredCanDeferPromotedGlobalReload(instrs, len(instrs)-1) {
		t.Fatal("end of function incorrectly deferred the required reload")
	}
}

func TestARM64StructuredTradesOneVectorStackRegisterOnlyUnderLocalPressure(t *testing.T) {
	if got := arm64StructuredV128StackRegisterCount(22, 22); got != len(arm64V128StackRegisters) {
		t.Fatalf("unpressured vector stack registers = %d", got)
	}
	if got := arm64StructuredV128StackRegisterCount(23, 22); got != len(arm64V128StackRegisters)-1 {
		t.Fatalf("pressured vector stack registers = %d", got)
	}
	if arm64V128LocalUseWeight(wasm.InstrLocalGet) != 1 || arm64V128LocalUseWeight(wasm.InstrLocalSet) != 2 ||
		arm64V128LocalUseWeight(wasm.InstrLocalTee) != 2 {
		t.Fatal("vector local write costs are not weighted above reads")
	}
	if !arm64StructuredCachesMemoryEnd(true, 4, 3) || !arm64StructuredCachesMemoryEnd(false, 3, 4) ||
		arm64StructuredCachesMemoryEnd(false, 2, 1) || arm64StructuredCachesMemoryEnd(true, 0, 0) {
		t.Fatal("cached memory end policy did not retain its memory-density threshold")
	}
	if !arm64StructuredSIMDDirectLocalKind(wasm.InstrV128Load) || !arm64StructuredSIMDDirectLocalKind(wasm.InstrI32x4Splat) ||
		arm64StructuredSIMDDirectLocalKind(wasm.InstrI32x4Add) {
		t.Fatal("direct SIMD local-result policy accepted the wrong operation")
	}
}

func TestARM64StructuredReusesV128LocalZero(t *testing.T) {
	source := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x05, 0x01, 0x60, 0x00, 0x01, 0x7b,
		0x03, 0x02, 0x01, 0x00,
		0x0a, 0x0f, 0x01, 0x0d, 0x01, 0x02, 0x7b,
		0x20, 0x00, 0x20, 0x01, 0xfd, 0x51, 0x1a, 0x20, 0x00, 0x0b,
	}
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
	zeroV128 := []byte{0x00, 0x1c, 0x20, 0x6e} // eor v0.16b, v0.16b, v0.16b
	if got := bytes.Count(output.Code, zeroV128); got != 1 {
		t.Fatalf("v128 local zero materializations = %d, want 1", got)
	}
}

func TestARM64FoldsInlinedI32AddTree(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x00, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x96, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0xab, 0x02, 0x6a, 0x0b}),
			wasmtest.Code([]byte{
				0x20, 0x00, 0x10, 0x00,
				0x20, 0x00, 0x10, 0x01,
				0x20, 0x00, 0x10, 0x02,
				0x6a, 0x6a, 0x0b,
			}),
		)),
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
	var metrics Metrics
	output, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	got := metrics.Functions[3]
	if !got.RailMachFinalized || runtime.GOOS != "windows" && (got.PostRARewrites != 4 || got.NativeBytes > 64) {
		t.Fatalf("inlined i32 add tree metrics = %#v", got)
	}
	if runtime.GOOS != "windows" && (len(output.DirectLeafPrepared) == 0 || output.DirectLeafPrepared[0]&(uint64(1)<<3) == 0) {
		t.Fatal("fully inlined direct-call tree did not publish the call-free leaf entry")
	}
}

func TestARM64FramefulPreparedLeafStaysOffGoStack(t *testing.T) {
	const locals = 32
	body := make([]byte, 0, locals*16)
	for local := range locals {
		body = append(body, 0x20, 0x00, 0xb8, 0x44)
		body = append(body, make([]byte, 8)...)
		body[len(body)-8] = byte(local + 1)
		body = append(body, 0xa0)
		body = append(body, 0x21)
		body = append(body, wasmtest.ULEB(uint32(local+1))...)
	}
	for local := range locals {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(local+1))...)
	}
	for range locals - 1 {
		body = append(body, 0xa0)
	}
	body = append(body, 0xbd, 0xa7, 0x0b)
	function := append([]byte{0x01, locals, 0x7c}, body...)
	code := append(wasmtest.ULEB(uint32(len(function))), function...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var metrics Metrics
	output, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].FrameBytes == 0 {
		t.Fatalf("frameful prepared leaf metrics = %#v", metrics.Functions)
	}
	if runtime.GOOS != "windows" && (len(output.DirectPrepared) == 0 || output.DirectPrepared[0]&1 == 0) {
		t.Fatal("frameful prepared leaf lost its foreign-stack direct entry")
	}
	if len(output.DirectLeafPrepared) != 0 && output.DirectLeafPrepared[0]&1 != 0 {
		t.Fatal("frameful prepared leaf published the Go-stack entry")
	}
}

func TestARM64StructuredFusesTeeBackedI32x4Rotate(t *testing.T) {
	source := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x06, 0x01, 0x60, 0x01, 0x7b, 0x01, 0x7b,
		0x03, 0x02, 0x01, 0x00,
		0x0a, 0x18, 0x01, 0x16, 0x01, 0x01, 0x7b,
		0x20, 0x00, 0x22, 0x01,
		0x41, 0x0c, 0xfd, 0xad, 0x01,
		0x20, 0x01, 0x41, 0x14, 0xfd, 0xab, 0x01,
		0xfd, 0x50, 0x0b,
	}
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
	got := metrics.Functions[0]
	if got.NativeBytes > 96 {
		t.Fatalf("tee-backed i32x4 rotate emitted %d bytes, want at most 96", got.NativeBytes)
	}
}

func TestARM64StructuredBranchesDirectlyOnPinnedComparisons(t *testing.T) {
	source := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x09, 0x02, 0x60, 0x00, 0x00, 0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x03, 0x03, 0x02, 0x00, 0x01,
		0x06, 0x06, 0x01, 0x7f, 0x01, 0x41, 0x0a, 0x0b,
		0x0a, 0x33, 0x02, 0x02, 0x00, 0x0b, 0x2e, 0x00,
		0x10, 0x00,
		0xfd, 0x0c, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1a,
		0x02, 0x40, 0x23, 0x00, 0x41, 0x05, 0x4a, 0x0d, 0x00,
		0x20, 0x00, 0x41, 0x03, 0x48, 0x04, 0x40, 0x23, 0x00, 0x1a, 0x0b, 0x0b,
		0x20, 0x00, 0x0b,
	}
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
	got := metrics.Functions[1]
	if got.RailMachFinalized || got.NativeBytes > 160 {
		t.Fatalf("structured direct-branch metrics = %#v", got)
	}
}

func TestARM64StructuredBranchesDirectlyOnStackComparisons(t *testing.T) {
	body := []byte{
		0x00,                                                             // no locals
		0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x1a, // v128.const 0; drop
		0x20, 0x00, 0x29, 0x00, 0x00, // local.get 0; i64.load
		0x42, 0x00, 0x52, // i64.const 0; i64.ne
		0x04, 0x7f, 0x41, 0x01, // if (result i32); i32.const 1
		0x05, 0x41, 0x02, 0x0b, 0x0b, // else; i32.const 2; end; end
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(append([]byte{0x00}, wasmtest.ULEB(1)...))),
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
	var metrics Metrics
	compiled, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	got := metrics.Functions[0]
	if got.RailMachFinalized || got.NativeBytes > 108 {
		t.Fatalf("structured stack-comparison branch metrics = %#v", got)
	}
	if bytes.Contains(compiled.Code, []byte{0xe9, 0x03, 0x04, 0x2a}) { // mov w9, w4
		t.Fatal("pinned local address was copied to the operand stack before the load")
	}
}

func TestARM64StructuredWritesSIMDBinaryDirectlyToTeeLocal(t *testing.T) {
	source := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x07, 0x01, 0x60, 0x02, 0x7b, 0x7b, 0x01, 0x7b,
		0x03, 0x02, 0x01, 0x00,
		0x0a, 0x12, 0x01, 0x10, 0x01, 0x01, 0x7b,
		0x20, 0x00, 0x20, 0x01, 0xfd, 0x51,
		0x20, 0x00, 0xfd, 0x51, 0x22, 0x02, 0x0b,
	}
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
	got := metrics.Functions[0]
	if got.NativeBytes > 100 {
		t.Fatalf("direct SIMD tee emitted %d bytes, want at most 100", got.NativeBytes)
	}
}

func TestARM64StructuredLoadsSIMDDirectlyIntoTeeLocal(t *testing.T) {
	source := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x06, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7b,
		0x03, 0x02, 0x01, 0x00,
		0x05, 0x03, 0x01, 0x00, 0x01,
		0x0a, 0x0e, 0x01, 0x0c, 0x01, 0x01, 0x7b,
		0x20, 0x00, 0xfd, 0x00, 0x04, 0x00, 0x22, 0x01, 0x0b,
	}
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
	if got := metrics.Functions[0].NativeBytes; got > 164 {
		t.Fatalf("direct SIMD load tee emitted %d bytes, want at most 164", got)
	}
}

func TestARM64RecognizesEarlyReturnCapacityGuard(t *testing.T) {
	m := &wasm.Module{Code: []wasm.Func{{BodyBytes: []byte{
		0x20, 0x00, 0x23, 0x03, 0x4d, 0x04, 0x40, 0x0f, 0x0b, 0x23, 0x02, 0x0b,
	}}}}
	global, ok := arm64EarlyReturnI32LEGlobal(m, 1, 1)
	if !ok || global != 3 {
		t.Fatalf("capacity guard = (%d, %t), want (3, true)", global, ok)
	}
	m.Code[0].BodyBytes[4] = 0x4c
	if _, ok := arm64EarlyReturnI32LEGlobal(m, 1, 1); ok {
		t.Fatal("signed comparison was accepted as an unsigned capacity guard")
	}
}

func TestARM64ShufflePatterns(t *testing.T) {
	ror8 := [16]byte{1, 2, 3, 0, 5, 6, 7, 4, 9, 10, 11, 8, 13, 14, 15, 12}
	ror16 := [16]byte{2, 3, 0, 1, 6, 7, 4, 5, 10, 11, 8, 9, 14, 15, 12, 13}
	zip1S := [16]byte{0, 1, 2, 3, 16, 17, 18, 19, 4, 5, 6, 7, 20, 21, 22, 23}
	zip2D := [16]byte{8, 9, 10, 11, 12, 13, 14, 15, 24, 25, 26, 27, 28, 29, 30, 31}
	if !arm64ShuffleLaneRotate(ror8, 4, 1) || !arm64ShuffleLaneRotate(ror16, 4, 2) {
		t.Fatal("lane rotate pattern was not recognized")
	}
	if arm64ShuffleLaneRotate(zip1S, 4, 1) {
		t.Fatal("zip pattern was recognized as a lane rotate")
	}
	if !arm64ShuffleZip(zip1S, 4, false) || !arm64ShuffleZip(zip2D, 8, true) {
		t.Fatal("zip pattern was not recognized")
	}
	var a arm64.Asm
	if dst, ok := emitARM64SpecializedShuffle(&a, ror8, 4, 4, 5); !ok || dst == 4 || len(a.B) != 8 {
		t.Fatalf("in-place lane rotate = dst %d, ok %t, bytes %d; want scratch destination and two instructions", dst, ok, len(a.B))
	}
	a.B = a.B[:0]
	if dst, ok := emitARM64SpecializedShuffle(&a, zip1S, 6, 4, 5); !ok || dst != 6 || len(a.B) != 4 {
		t.Fatalf("zip1 = dst %d, ok %t, bytes %d; want requested destination and one instruction", dst, ok, len(a.B))
	}
}

func TestCompilerARM64MOPSBulkMemoryIsFeatureGated(t *testing.T) {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil)))
	funcSec := wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0)))
	memorySec := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))
	body := func(subopcode byte) []byte {
		instructions := []byte{
			0x20, 0x00,
			0x20, 0x01,
			0x20, 0x02,
			0xfc, subopcode,
		}
		if subopcode == 10 {
			instructions = append(instructions, 0x00, 0x00)
		} else {
			instructions = append(instructions, 0x00)
		}
		instructions = append(instructions, 0x0b)
		function := append([]byte{0x00}, instructions...)
		return append(wasmtest.ULEB(uint32(len(function))), function...)
	}
	source := wasmtest.Module(typeSec, funcSec, memorySec, wasmtest.Section(10, wasmtest.Vec(body(10), body(11))))
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	target := corecompiler.Target{GOOS: "linux", GOARCH: "arm64", Mode: corecompiler.TargetExplicit, CPUModel: "test-mops", TuningModel: "generic-arm64"}
	target.FeatureBits[0] = uint64(1) << corecompiler.TargetFeatureARM64MOPS
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	compiler := Compiler{FunctionCache: cache}
	withMOPS, err := compiler.Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	copySequence := []byte{0x40, 0x04, 0x01, 0x1d, 0x40, 0x04, 0x41, 0x1d, 0x40, 0x04, 0x81, 0x1d}
	setSequence := []byte{0x40, 0x04, 0xc1, 0x19, 0x40, 0x44, 0xc1, 0x19, 0x40, 0x84, 0xc1, 0x19}
	if !bytes.Contains(withMOPS.Code, copySequence) || !bytes.Contains(withMOPS.Code, setSequence) {
		t.Fatalf("MOPS code misses copy or set sequence: %x", withMOPS.Code)
	}
	if !withMOPS.RequiresARM64MOPS {
		t.Fatal("MOPS output omitted its runtime ISA requirement")
	}
	target.FeatureBits = [4]uint64{}
	withoutMOPS, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(withoutMOPS.Code, copySequence) || bytes.Contains(withoutMOPS.Code, setSequence) {
		t.Fatal("compatibility feature set emitted MOPS")
	}
	if withoutMOPS.RequiresARM64MOPS {
		t.Fatal("baseline output requires MOPS")
	}
	if len(withMOPS.Code) >= len(withoutMOPS.Code) {
		t.Fatalf("MOPS code size = %d, baseline = %d", len(withMOPS.Code), len(withoutMOPS.Code))
	}
	target.FeatureBits[0] = uint64(1) << corecompiler.TargetFeatureARM64MOPS
	tinyProfile := &compilerprofile.Module{
		Version: compilerprofile.Version, ModuleHash: sha256.Sum256(source), Source: compilerprofile.SourceStatic, Phase: compilerprofile.PhaseSteady,
		MemOpSizes: []compilerprofile.ValueHistogram{
			{Site: compilerprofile.Site{Function: 0, Offset: 6}, Buckets: []compilerprofile.ValueBucket{{Low: 0, High: 64, Count: 100}}},
			{Site: compilerprofile.Site{Function: 1, Offset: 6}, Buckets: []compilerprofile.ValueBucket{{Low: 0, High: 64, Count: 100}}},
		},
	}
	profiled, err := compiler.Compile(corecompiler.Input{Module: m, Source: source, Target: target, Profile: tinyProfile})
	if err != nil {
		t.Fatal(err)
	}
	if profiled.RequiresARM64MOPS || bytes.Contains(profiled.Code, copySequence) || bytes.Contains(profiled.Code, setSequence) {
		t.Fatal("tiny-dominated profile selected MOPS")
	}
	warm, err := compiler.Compile(corecompiler.Input{Module: m, Source: source, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if !warm.RequiresARM64MOPS || !bytes.Equal(warm.Code, withMOPS.Code) {
		t.Fatal("warm function artifacts lost MOPS code or its requirement")
	}
}

func TestCompilerARM64RailMachFinalizesBulkMemoryAndSaturatingConversion(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.F32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get destination
			0x20, 0x01, // local.get fill byte
			0x20, 0x02, // local.get length
			0xfc, 0x0b, 0x00, // memory.fill 0
			0x20, 0x03, // local.get float
			0xfc, 0x00, // i32.trunc_sat_f32_s
			0x0b,
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
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized {
		t.Fatalf("bulk-memory/saturating-conversion finalization = %#v", metrics.Functions)
	}
}

func TestARM64ConstantBulkMemory64IsCompactAndOverlapAware(t *testing.T) {
	var forward, backward, fill arm64.Asm
	emitARM64ConstantBulkMemory64(&forward, wasm.InstrMemoryCopy, 8192, 0)
	emitARM64ConstantBulkMemory64(&backward, wasm.InstrMemoryCopy, 32784, 32768)
	emitARM64ConstantBulkMemory64(&fill, wasm.InstrMemoryFill, 49152, 0xa5)
	for name, code := range map[string][]byte{"forward": forward.B, "backward": backward.B, "fill": fill.B} {
		if len(code) > 64 {
			t.Fatalf("constant %s 64-byte bulk sequence = %d bytes, want at most 64", name, len(code))
		}
	}
	if bytes.Equal(forward.B, backward.B) {
		t.Fatal("overlapping backward copy used forward load/store order")
	}
}

func TestARM64BulkMemoryCopyHasOverlapSafe32BytePath(t *testing.T) {
	var emitted arm64.Asm
	traps := 0
	if err := emitARM64BulkMemoryRegisters(&emitted, wasm.InstrMemoryCopy, 17, false, 0, nil, func(_ int, offset uint32) {
		if offset != 17 {
			t.Fatalf("trap offset = %d", offset)
		}
		traps++
	}); err != nil {
		t.Fatal(err)
	}
	if traps != 2 {
		t.Fatalf("memory.copy trap branches = %d", traps)
	}
	var fixed arm64.Asm
	fixed.LdrQ(arm64.X16, arm64.X1, 0)
	fixed.LdrQ(arm64.X17, arm64.X1, 16)
	fixed.StpQOffset(arm64.X16, arm64.X17, arm64.X0, 0)
	if !bytes.Contains(emitted.B, fixed.B) {
		t.Fatalf("memory.copy lacks load-before-store 32-byte path: %x", emitted.B)
	}
}

func TestCompilerARM64SignalsBoundsElideScalarChecks(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target, Bounds: corecompiler.BoundsExplicit})
	if err != nil {
		t.Fatal(err)
	}
	signals, err := (Compiler{}).Compile(corecompiler.Input{Module: m, Source: source, Target: target, Bounds: corecompiler.BoundsSignals})
	if err != nil {
		t.Fatal(err)
	}
	if len(signals.Code) >= len(explicit.Code) {
		t.Fatalf("signals/explicit native bytes = %d/%d, want signals smaller", len(signals.Code), len(explicit.Code))
	}
}

func TestCompilerNativeARM64RealizesNZCVPhysicalRename(t *testing.T) {
	locals := append(wasmtest.ULEB(2), byte(0x7f))
	body := append(wasmtest.Vec(locals), []byte{
		0x20, 0x00, // local.get 0
		0x20, 0x01, // local.get 1
		0x48,       // i32.lt_s
		0x21, 0x02, // local.set condition
		0x41, 0x07, // i32.const 7: MOV preserves NZCV
		0x21, 0x03, // local.set retained value
		0x20, 0x02, // local.get condition
		0x04, 0x7f, // if (result i32)
		0x41, 0x01, // then 1
		0x05,       // else
		0x41, 0x00, // else 0
		0x0b,       // end if
		0x20, 0x03, // retained constant
		0x6a, // i32.add
		0x0b,
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
	// Current schedulers correctly move the independent constant ahead of the
	// comparison. Force the constrained legal order that this repair exists for,
	// then rebuild every schedule-dependent product before finalization.
	schedule := *plan.Schedule
	schedule.Order = append([]uint32(nil), plan.Schedule.Order...)
	if len(schedule.Order) < 3 || schedule.Order[0] != 1 || schedule.Order[1] != 0 || schedule.Order[2] != 2 {
		t.Fatalf("unexpected source-stable compare schedule: %#v", schedule.Order)
	}
	schedule.Order[0], schedule.Order[1] = schedule.Order[1], schedule.Order[0]
	allocation, err := railmach.AllocateGreedyPForSchedule(plan.Machine, &schedule, railmach.DefaultGreedyConfig(railmach.TargetARM64), nil)
	if err != nil {
		t.Fatal(err)
	}
	exit, err := railmach.LateSSAExit(plan.Machine, &allocation.Allocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	postRA, err := railmach.PlanPostRA(railmach.TargetARM64, plan.Machine, plan.Selection, &schedule, allocation, exit, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rewrite := range postRA.Rewrites {
		found = found || rewrite.Kind == railmach.RewritePhysicalRename && rewrite.First == 0 && rewrite.Second == 2
	}
	if !found {
		t.Fatalf("forced post-RA rewrites = %#v", postRA.Rewrites)
	}
	forced := *plan
	forced.Schedule, forced.Allocation, forced.Exit, forced.PostRA = &schedule, allocation, exit, postRA
	forced.PostRAFusionWith = make([]uint32, len(plan.Machine.Insts))
	forced.PostRAFusionWith[0], forced.PostRAFusionWith[2] = 3, 1
	var relocs []arm64CallReloc
	var metrics FunctionMetrics
	optimized, _, ok, err := emitARM64RailMach(fn, &forced, false, nil, &relocs, &metrics, nil)
	if err != nil || !ok {
		t.Fatalf("optimized NZCV finalization = ok %t, err %v", ok, err)
	}
	baseline := forced
	clearPostRAEmissionRewrites(&baseline)
	relocs = relocs[:0]
	checked, _, ok, err := emitARM64RailMach(fn, &baseline, false, nil, &relocs, nil, nil)
	if err != nil || !ok {
		t.Fatalf("baseline NZCV finalization = ok %t, err %v", ok, err)
	}
	if metrics.PostRARewrites != 1 || len(optimized) >= len(checked) {
		t.Fatalf("NZCV realization = rewrites %d optimized %d baseline %d", metrics.PostRARewrites, len(optimized), len(checked))
	}
}

func TestARM64RailMachLeaSPLargeFrameOffset(t *testing.T) {
	var a arm64.Asm
	if !arm64RailMachLeaSP(&a, arm64.X8, 0x1234) || len(a.B) != 8 {
		t.Fatalf("large SP-relative address = %x", a.B)
	}
	if arm64RailMachLeaSP(&a, arm64.X8, 0x1000000) {
		t.Fatal("out-of-range SP-relative address accepted")
	}
}

func TestARM64StructuredRegisterModesKeepShallowOperandStackInRegisters(t *testing.T) {
	operandStack, full := arm64StructuredRegisterModes(false, false, false, false, len(arm64StackLocalRegisters)+1, 0, len(arm64OperandStackRegisters))
	if !operandStack || full {
		t.Fatalf("register modes = operand stack %t, full %t; want true, false", operandStack, full)
	}
	operandStack, full = arm64StructuredRegisterModes(false, false, false, false, len(arm64StackLocalRegisters), 8, len(arm64OperandStackRegisters))
	if !operandStack || !full {
		t.Fatalf("register modes = operand stack %t, full %t; want true, true", operandStack, full)
	}
	operandStack, full = arm64StructuredRegisterModes(true, false, false, false, len(arm64MixedScalarLocalRegisters), 0, arm64SIMDOperandStackRegisters)
	if !operandStack || full {
		t.Fatalf("mixed SIMD register modes = operand stack %t, full %t; want true, false", operandStack, full)
	}
	operandStack, _ = arm64StructuredRegisterModes(true, false, false, false, 0, 0, arm64SIMDOperandStackRegisters+1)
	if operandStack {
		t.Fatal("deep mixed SIMD operand stack was admitted to scalar registers")
	}
	operandStack, full = arm64StructuredRegisterModes(false, true, true, false, len(arm64CallPinnedLocalRegisters), 0, len(arm64OperandStackRegisters))
	if !operandStack || full {
		t.Fatalf("direct-call register modes = operand stack %t, full %t; want true, false", operandStack, full)
	}
	operandStack, full = arm64StructuredRegisterModes(true, true, true, false, len(arm64CallPinnedLocalRegisters), 0, arm64SIMDOperandStackRegisters)
	if !operandStack || full {
		t.Fatalf("SIMD direct-call register modes = operand stack %t, full %t; want true, false", operandStack, full)
	}
	operandStack, _ = arm64StructuredRegisterModes(true, true, true, true, 0, 0, arm64SIMDOperandStackRegisters)
	if !operandStack {
		t.Fatal("result-typed loop unnecessarily disabled the register operand stack")
	}
	operandStack, _ = arm64StructuredRegisterModes(false, true, false, false, 0, 0, 1)
	if operandStack {
		t.Fatal("unmanaged call boundary admitted the scalar operand stack to registers")
	}
}

func TestARM64StructuredOperandStackRegistersAdmitDeepCallFreeSIMD(t *testing.T) {
	registers, deep := arm64StructuredOperandStackRegisters(true, false, uint32(len(arm64DeepSIMDOperandStackRegisters)))
	if !deep || len(registers) != len(arm64DeepSIMDOperandStackRegisters) {
		t.Fatalf("deep SIMD registers = %v, deep %t", registers, deep)
	}
	seen := map[arm64.Reg]bool{}
	for _, reg := range registers {
		if reg == arm64.X15 || seen[reg] {
			t.Fatalf("unsafe or duplicate deep SIMD operand register %d in %v", reg, registers)
		}
		seen[reg] = true
	}
	for _, test := range []struct {
		hasV128, hasCall bool
		maxStack         uint32
	}{
		{hasV128: true, maxStack: arm64SIMDOperandStackRegisters},
		{hasV128: true, maxStack: uint32(len(arm64DeepSIMDOperandStackRegisters) + 1)},
		{hasV128: true, hasCall: true, maxStack: uint32(len(arm64DeepSIMDOperandStackRegisters))},
		{maxStack: uint32(len(arm64DeepSIMDOperandStackRegisters))},
	} {
		registers, deep := arm64StructuredOperandStackRegisters(test.hasV128, test.hasCall, test.maxStack)
		if deep || len(registers) != len(arm64OperandStackRegisters) {
			t.Fatalf("fallback registers(%t, %t, %d) = %v, deep %t", test.hasV128, test.hasCall, test.maxStack, registers, deep)
		}
	}
}

func TestARM64FloatBinaryPairRecognizesMatchingWidths(t *testing.T) {
	typ, f64, ok := arm64FloatBinaryPair(wasm.InstrF64Mul, wasm.InstrF64Add)
	if !ok || !f64 || typ != wasm.F64 {
		t.Fatalf("f64 mul/add pair = type %s, f64 %t, ok %t", typ, f64, ok)
	}
	if _, _, ok := arm64FloatBinaryPair(wasm.InstrF32Mul, wasm.InstrF64Add); ok {
		t.Fatal("mixed-width float pair accepted")
	}
}

func TestARM64RailMachCachesHighestCostFloatConstants(t *testing.T) {
	plan := &nativeBackendPlan{
		Machine: &railmach.Func{
			Insts: []railmach.Inst{
				{Op: wasm.InstrF64Const, Aux: 0x3f847ae147ae147b},
				{Op: wasm.InstrF64Const, Aux: 0},
				{Op: wasm.InstrF64Const, Aux: 0x3fe0000000000000},
				{Op: wasm.InstrF32Const, Aux: 1},
			},
			Blocks: []railmach.Block{{Weight: 8}, {Weight: 16}, {Weight: 4}, {Weight: 1}},
		},
		Schedule: &railmach.Schedule{BlockOf: []railssa.BlockID{0, 1, 2, 3}},
	}
	cached, count := arm64RailMachCachedFloatConstants(plan)
	if count != 3 {
		t.Fatalf("cached constant count = %d, want 3: %#v", count, cached)
	}
	if cached[0].kind != wasm.InstrF64Const || cached[0].bits != 0x3f847ae147ae147b {
		t.Fatalf("highest-cost constant = %#v", cached[0])
	}
	if cached[1].bits != 0 || cached[2].bits != 0x3fe0000000000000 {
		t.Fatalf("remaining cached constants = %#v", cached[1:])
	}
	plan.ABI.HasCall = true
	if _, count := arm64RailMachCachedFloatConstants(plan); count != 0 {
		t.Fatalf("call-making function cached %d constants", count)
	}
	plan.ABI.HasCall = false
	plan.Machine.Insts[0].Result = 1
	plan.Machine.VRegs = []railmach.VRegData{{}, {Def: 3, Type: railmach.TypeF64, Bank: railmach.BankFPR}}
	plan.Allocation = &railmach.GreedyAllocation{Allocation: railmach.Allocation{
		Locations:            []railmach.Location{{}, {Kind: railmach.LocationRegister, Bank: railmach.BankFPR}},
		InstructionPositions: []uint32{0, 1, 2, 3},
	}}
	if physical, ok := arm64RailMachCachedFloatValue(plan, 1, cached, count); !ok || physical != 24 {
		t.Fatalf("cached SSA constant = (%d, %v), want (24, true)", physical, ok)
	}
	plan.Machine.Transfers = []railmach.EdgeTransfer{{Src: 1}}
	if _, ok := arm64RailMachCachedFloatValue(plan, 1, cached, count); ok {
		t.Fatal("edge-transferred constant bypassed its allocated location")
	}
}

func TestARM64RailMachRenamesFinalEdgeMultiply(t *testing.T) {
	f := &railmach.Func{
		Insts:    []railmach.Inst{{Op: wasm.InstrF64Mul, Result: 3, OperandStart: 0, OperandCount: 2}},
		Operands: []railmach.Operand{{Reg: 1, Bank: railmach.BankFPR}, {Reg: 2, Bank: railmach.BankFPR}},
		VRegs: []railmach.VRegData{
			{},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegBlockParam},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegInitial},
			{Def: 3, Type: railmach.TypeF64, Bank: railmach.BankFPR},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegBlockParam},
		},
		Blocks:    []railmach.Block{{InstCount: 1}, {}},
		Edges:     []railmach.Edge{{From: 0, To: 1}},
		Transfers: []railmach.EdgeTransfer{{Src: 3, Dst: 4, Edge: 0}},
	}
	plan := &nativeBackendPlan{
		Machine: f,
		Schedule: &railmach.Schedule{
			Order:       []uint32{0},
			BlockRanges: []railmach.MoveRange{{Count: 1}, {Start: 1}},
			BlockOf:     []railssa.BlockID{0},
		},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{
			Locations: []railmach.Location{
				{},
				{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 9},
				{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 7},
				{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 6},
				{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 5},
			},
			InstructionPositions: []uint32{0},
		}},
		Exit: &railmach.SSAExit{
			Moves: []railmach.PhysicalMove{{
				Src:       railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 6},
				Dst:       railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 5},
				Reg:       3,
				Edge:      0,
				Kind:      railmach.MoveCopy,
				Placement: railmach.PlacePredecessorEnd,
				Bank:      railmach.BankFPR,
			}},
			EdgeMoves: []railmach.MoveRange{{Count: 1}},
		},
	}
	rename := arm64RailMachEdgeResultRename(plan, 0)
	if !rename.valid || rename.instruction != 0 || rename.edge != 0 || rename.move != 0 || rename.destination.Index != 5 {
		t.Fatalf("edge result rename = %#v", rename)
	}
	f.Insts[0].Op = wasm.InstrI64Add
	for index := range f.Operands {
		f.Operands[index].Bank = railmach.BankGPR
	}
	for index := 1; index < len(f.VRegs); index++ {
		f.VRegs[index].Type = railmach.TypeI64
		f.VRegs[index].Bank = railmach.BankGPR
		plan.Allocation.Locations[index].Bank = railmach.BankGPR
	}
	plan.Exit.Moves[0].Src.Bank = railmach.BankGPR
	plan.Exit.Moves[0].Dst.Bank = railmach.BankGPR
	plan.Exit.Moves[0].Bank = railmach.BankGPR
	if integer := arm64RailMachEdgeResultRename(plan, 0); !integer.valid || integer.destination.Index != 5 {
		t.Fatalf("integer edge result rename = %#v", integer)
	}
	f.Insts = append(f.Insts, make([]railmach.Inst, 255)...)
	if large := arm64RailMachEdgeResultRename(plan, 0); large.valid {
		t.Fatalf("large integer edge result rename = %#v, want disabled", large)
	}
	f.Insts = f.Insts[:1]
	f.Insts[0].Op = wasm.InstrF64Mul
	for index := range f.Operands {
		f.Operands[index].Bank = railmach.BankFPR
	}
	for index := 1; index < len(f.VRegs); index++ {
		f.VRegs[index].Type = railmach.TypeF64
		f.VRegs[index].Bank = railmach.BankFPR
		plan.Allocation.Locations[index].Bank = railmach.BankFPR
	}
	plan.Exit.Moves[0].Src.Bank = railmach.BankFPR
	plan.Exit.Moves[0].Dst.Bank = railmach.BankFPR
	plan.Exit.Moves[0].Bank = railmach.BankFPR
	f.Insts = append(f.Insts, railmach.Inst{Op: wasm.InstrF64Add, Result: 5, OperandStart: 2, OperandCount: 2})
	f.Operands = append(f.Operands, railmach.Operand{Reg: 1, Bank: railmach.BankFPR}, railmach.Operand{Reg: 3, Bank: railmach.BankFPR})
	f.VRegs = append(f.VRegs,
		railmach.VRegData{Def: 9, Type: railmach.TypeF64, Bank: railmach.BankFPR},
		railmach.VRegData{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegBlockParam},
	)
	f.Blocks[0].InstCount = 2
	f.Transfers = append(f.Transfers, railmach.EdgeTransfer{Src: 5, Dst: 6, Edge: 0})
	plan.Schedule.Order = append(plan.Schedule.Order, 1)
	plan.Schedule.BlockRanges[0].Count = 2
	plan.Schedule.BlockRanges[1].Start = 2
	plan.Schedule.BlockOf = append(plan.Schedule.BlockOf, 0)
	plan.Allocation.Locations = append(plan.Allocation.Locations,
		railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 8},
		railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 7},
	)
	plan.Allocation.InstructionPositions = append(plan.Allocation.InstructionPositions, 1)
	latest := railmach.PhysicalMove{
		Src:       railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 8},
		Dst:       railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 7},
		Reg:       5,
		Edge:      0,
		Kind:      railmach.MoveCopy,
		Placement: railmach.PlacePredecessorEnd,
		Bank:      railmach.BankFPR,
	}
	plan.Exit.Moves = append([]railmach.PhysicalMove{latest}, plan.Exit.Moves...)
	plan.Exit.EdgeMoves[0].Count = 2
	rename = arm64RailMachEdgeResultRename(plan, 0)
	if !rename.valid || rename.instruction != 1 || rename.move != 0 || rename.destination.Index != 7 ||
		!rename.chained || rename.chainedInstruction != 0 || rename.chainedMove != 1 || rename.chainedResult != 3 || rename.chainedDestination.Index != 5 {
		t.Fatalf("latest edge result rename = %#v", rename)
	}
	f.Insts = append(f.Insts, railmach.Inst{Op: wasm.InstrF64Neg, OperandStart: uint32(len(f.Operands)), OperandCount: 1})
	f.Operands = append(f.Operands, railmach.Operand{Reg: 5, Bank: railmach.BankFPR})
	if unsafe := arm64RailMachEdgeResultRename(plan, 0); unsafe.valid {
		t.Fatalf("rename abandoned an ordinary result use: %#v", unsafe)
	}
	f.Insts = f.Insts[:len(f.Insts)-1]
	f.Operands = f.Operands[:len(f.Operands)-1]
	plan.Exit.Moves = append(plan.Exit.Moves, railmach.PhysicalMove{Src: rename.destination, Dst: railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 4}})
	plan.Exit.EdgeMoves[0].Count++
	if unsafe := arm64RailMachEdgeResultRename(plan, 0); unsafe.valid {
		t.Fatalf("rename clobbered another edge source: %#v", unsafe)
	}
}

func TestARM64RailMachPrefersCoupledFPRCopiesOverLaterIntegerCopy(t *testing.T) {
	f := &railmach.Func{
		Insts: []railmach.Inst{
			{Op: wasm.InstrF64Mul, Result: 3, OperandStart: 0, OperandCount: 2},
			{Op: wasm.InstrF64Add, Result: 5, OperandStart: 2, OperandCount: 2},
			{Op: wasm.InstrI32Add, Result: 7, OperandStart: 4, OperandCount: 2},
		},
		Operands: []railmach.Operand{
			{Reg: 1, Bank: railmach.BankFPR}, {Reg: 2, Bank: railmach.BankFPR},
			{Reg: 1, Bank: railmach.BankFPR}, {Reg: 2, Bank: railmach.BankFPR},
			{Reg: 9, Bank: railmach.BankGPR}, {Reg: 10, Bank: railmach.BankGPR},
		},
		VRegs: []railmach.VRegData{
			{},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegBlockParam},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegBlockParam},
			{Def: 3, Type: railmach.TypeF64, Bank: railmach.BankFPR},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegBlockParam},
			{Def: 9, Type: railmach.TypeF64, Bank: railmach.BankFPR},
			{Type: railmach.TypeF64, Bank: railmach.BankFPR, Flags: railmach.VRegBlockParam},
			{Def: 15, Type: railmach.TypeI32, Bank: railmach.BankGPR},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Flags: railmach.VRegBlockParam},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Flags: railmach.VRegBlockParam},
			{Type: railmach.TypeI32, Bank: railmach.BankGPR, Flags: railmach.VRegInitial},
		},
		Blocks: []railmach.Block{{InstCount: 3}, {}},
		Edges:  []railmach.Edge{{From: 0, To: 1}},
		Transfers: []railmach.EdgeTransfer{
			{Src: 3, Dst: 4, Edge: 0}, {Src: 5, Dst: 6, Edge: 0}, {Src: 7, Dst: 8, Edge: 0},
		},
	}
	locations := []railmach.Location{
		{},
		{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 9},
		{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 10},
		{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 6},
		{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 5},
		{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 8},
		{Kind: railmach.LocationRegister, Bank: railmach.BankFPR, Index: 7},
		{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 2},
		{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 1},
		{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 3},
		{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: 4},
	}
	plan := &nativeBackendPlan{
		Machine: f,
		Schedule: &railmach.Schedule{
			Order: []uint32{0, 1, 2}, BlockRanges: []railmach.MoveRange{{Count: 3}, {Start: 3}}, BlockOf: []railssa.BlockID{0, 0, 0},
		},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: locations, InstructionPositions: []uint32{0, 1, 2}}},
		Exit: &railmach.SSAExit{
			Moves: []railmach.PhysicalMove{
				{Src: locations[3], Dst: locations[4], Reg: 3, Edge: 0, Kind: railmach.MoveCopy, Placement: railmach.PlacePredecessorEnd, Bank: railmach.BankFPR},
				{Src: locations[5], Dst: locations[6], Reg: 5, Edge: 0, Kind: railmach.MoveCopy, Placement: railmach.PlacePredecessorEnd, Bank: railmach.BankFPR},
				{Src: locations[7], Dst: locations[8], Reg: 7, Edge: 0, Kind: railmach.MoveCopy, Placement: railmach.PlacePredecessorEnd, Bank: railmach.BankGPR},
			},
			EdgeMoves: []railmach.MoveRange{{Count: 3}},
		},
	}
	rename := arm64RailMachEdgeResultRename(plan, 0)
	if !rename.valid || rename.instruction != 1 || rename.destination != locations[6] {
		t.Fatalf("FPR recurrence rename = %#v, want instruction 1 to %#v", rename, locations[6])
	}
	if !rename.independent || rename.independentInstruction != 2 || rename.independentMove != 2 || rename.independentDestination != locations[8] {
		t.Fatalf("independent recurrence rename = %#v, want instruction 2 to %#v", rename, locations[8])
	}
	f.Insts = append(f.Insts, railmach.Inst{Op: wasm.InstrI32Eqz, OperandStart: uint32(len(f.Operands)), OperandCount: 1})
	f.Operands = append(f.Operands, railmach.Operand{Reg: 7, Bank: railmach.BankGPR})
	if unsafe := arm64RailMachEdgeResultRename(plan, 0); unsafe.independent {
		t.Fatalf("independent rename abandoned an ordinary counter use: %#v", unsafe)
	}
}

func TestARM64RailMachPromotesTrapFreeMutableGlobal(t *testing.T) {
	plan := &nativeBackendPlan{
		Stack: &railssa.StackFunc{Globals: []wasm.ValType{wasm.I64}},
		Machine: &railmach.Func{
			Insts: []railmach.Inst{
				{Op: wasm.InstrGlobalGet, Result: 1},
				{Op: wasm.InstrI64Const, Result: 2},
				{Op: wasm.InstrI64Add, Result: 3, OperandStart: 0, OperandCount: 2},
				{Op: wasm.InstrGlobalSet, OperandStart: 2, OperandCount: 1},
			},
			Operands: []railmach.Operand{{Reg: 1, Bank: railmach.BankGPR}, {Reg: 2, Bank: railmach.BankGPR}, {Reg: 3, Bank: railmach.BankGPR}},
			VRegs: []railmach.VRegData{
				{},
				{Def: 3, Type: railmach.TypeI64, Bank: railmach.BankGPR},
				{Def: 9, Type: railmach.TypeI64, Bank: railmach.BankGPR},
				{Def: 15, Type: railmach.TypeI64, Bank: railmach.BankGPR},
			},
		},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: make([]railmach.Location, 4)}},
	}
	promoted := arm64RailMachPromotedGlobal(plan)
	if !promoted.valid || promoted.index != 0 || promoted.typ != wasm.I64 {
		t.Fatalf("promoted global = %#v", promoted)
	}
	if !arm64RailMachPromotedGlobalValue(plan, 1, promoted) || !arm64RailMachPromotedGlobalValue(plan, 3, promoted) {
		t.Fatal("global load or committed arithmetic result did not name the promoted register")
	}
	plan.Machine.Results = []railmach.VReg{1}
	if arm64RailMachPromotedGlobalValue(plan, 1, promoted) {
		t.Fatal("function result bypassed its allocated location")
	}
	plan.Machine.Results = nil
	plan.Machine.Insts = append(plan.Machine.Insts, railmach.Inst{Op: wasm.InstrI64Load})
	if unsafe := arm64RailMachPromotedGlobal(plan); unsafe.valid {
		t.Fatalf("trapping function promoted global: %#v", unsafe)
	}
}

func TestARM64RailMachSoleConsumerRejectsLaterUses(t *testing.T) {
	machine := &railmach.Func{
		Insts: []railmach.Inst{
			{Op: wasm.InstrI32Const, Result: 1},
			{Op: wasm.InstrI32Add, Result: 2, OperandCount: 1},
		},
		Operands: []railmach.Operand{{Reg: 1}},
		VRegs:    make([]railmach.VRegData, 3),
	}
	plan := &nativeBackendPlan{Machine: machine}
	if !arm64RailMachSoleConsumer(plan, 1, 1) {
		t.Fatal("single consumer was not recognized")
	}
	machine.Results = []railmach.VReg{1}
	if arm64RailMachSoleConsumer(plan, 1, 1) {
		t.Fatal("function result was treated as a sole instruction consumer")
	}
}

func TestARM64RailMachI32SpillUsesOneMemoryOperation(t *testing.T) {
	plan := &nativeBackendPlan{
		Machine: &railmach.Func{VRegs: []railmach.VRegData{{}, {Type: railmach.TypeI32, Bank: railmach.BankGPR}}},
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{
			Locations:  []railmach.Location{{}, {Kind: railmach.LocationSpill, Bank: railmach.BankGPR}},
			SpillSlots: 1,
		}},
	}
	var a arm64.Asm
	if _, err := arm64RailMachReadLocation(&a, plan, 1, plan.Allocation.Locations[1], arm64.X13, 0); err != nil {
		t.Fatal(err)
	}
	if got := a.Len(); got != 4 {
		t.Fatalf("i32 spill load bytes = %d, want 4", got)
	}
	a.B = a.B[:0]
	if err := arm64RailMachWriteLocation(&a, plan, 1, plan.Allocation.Locations[1], arm64.X13); err != nil {
		t.Fatal(err)
	}
	if got := a.Len(); got != 4 {
		t.Fatalf("i32 spill store bytes = %d, want 4", got)
	}
}

func TestARM64RailMachReusesDominatingMemoryCheckInBlock(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get address for store
			0x20, 0x00, // local.get address for load
			0x28, 0x02, 0x00, // i32.load
			0x41, 0x01, 0x6a, // i32.const 1; i32.add
			0x36, 0x02, 0x00, // i32.store to the same address
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
	var metadata functionEmissionMetadata
	if _, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, &metadata); err != nil || !ok {
		t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
	}
	if len(metadata.Traps) != 1 || metadata.Traps[0].Code != 3 {
		t.Fatalf("same-address memory traps = %#v, want one dominating bounds trap", metadata.Traps)
	}
}

func TestARM64RailMachCombinesAdjacentLoadBoundsChecks(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x28, 0x02, 0x00, // i32.load address 0
			0x20, 0x01, 0x28, 0x02, 0x00, // i32.load address 1
			0x6a, 0x0b, // i32.add; end
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
	var metadata functionEmissionMetadata
	code, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, &metadata)
	if err != nil || !ok {
		t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
	}
	conditionalCompares := 0
	for offset := 0; offset+4 <= len(code); offset += 4 {
		if binary.LittleEndian.Uint32(code[offset:])&0xffe00c10 == 0x7a400000 {
			conditionalCompares++
		}
	}
	if conditionalCompares != 1 {
		t.Fatalf("conditional bounds compares = %d, want 1", conditionalCompares)
	}
	if len(metadata.Traps) != 1 || metadata.Traps[0].Code != 3 {
		t.Fatalf("adjacent-load memory traps = %#v, want one combined bounds trap", metadata.Traps)
	}
}

func TestARM64RailMachPairsAndRotatesZeroTerminatedPointerChase(t *testing.T) {
	locals := append(wasmtest.ULEB(1), byte(0x7f))
	body := append(wasmtest.Vec(locals), []byte{
		0x41, 0x00, 0x21, 0x01, // sum = 0
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // break when pointer == 0
		0x20, 0x01, 0x20, 0x00, 0x28, 0x02, 0x00, 0x6a, 0x21, 0x01, // sum += node.value
		0x20, 0x00, 0x28, 0x02, 0x04, 0x21, 0x00, // pointer = node.next
		0x0c, 0x00, 0x0b, 0x0b, // continue; end loop/block
		0x20, 0x01, 0x0b,
	}...)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
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
	plan.SignalsBounds = true
	paired := false
	for _, encoded := range plan.PostRAPairWith {
		paired = paired || encoded != 0
	}
	rotated := false
	for edge, candidate := range plan.Machine.Edges {
		_, _, ok := arm64RailMachRotatedZeroTestLatch(plan, uint32(candidate.From), uint32(edge))
		rotated = rotated || ok
	}
	if !paired || !rotated {
		t.Fatalf("pointer chase optimization = pair %t, rotate %t; rewrites %#v", paired, rotated, plan.PostRA.Rewrites)
	}
	native, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, nil)
	if err != nil || !ok {
		t.Fatalf("pointer chase finalization = ok %t, err %v", ok, err)
	}
	loadPairs, backwardCBNZ := 0, 0
	for offset := 0; offset+4 <= len(native); offset += 4 {
		instruction := binary.LittleEndian.Uint32(native[offset:])
		if instruction&0xffc00000 == 0x29400000 {
			loadPairs++
		}
		if instruction&0x7f000000 == 0x35000000 && instruction&0x00800000 != 0 {
			backwardCBNZ++
		}
	}
	if loadPairs == 0 || backwardCBNZ == 0 {
		t.Fatalf("pointer chase code has load pairs=%d backward CBNZ=%d", loadPairs, backwardCBNZ)
	}
}

func TestARM64RailMachDoesNotPairLoadsAcrossTrap(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x28, 0x02, 0x00, // i32.load offset=0
			0x20, 0x01, 0x6d, // i32.div_s (may trap)
			0x20, 0x00, 0x28, 0x02, 0x04, // i32.load offset=4
			0x6a, 0x0b,
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
	for _, encoded := range plan.PostRAPairWith {
		if encoded != 0 {
			t.Fatalf("loads were paired across trapping division: %#v", plan.PostRA.Rewrites)
		}
	}
}

func TestARM64RailMachDefersUnreachableTrapsPastHotReturn(t *testing.T) {
	locals := append(wasmtest.ULEB(3), byte(0x7e))
	body := append(wasmtest.Vec(locals), []byte{
		0x42, 0x00, 0x21, 0x01, // a = 0
		0x42, 0x01, 0x21, 0x02, // b = 1
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x00, 0x45, 0x0d, 0x01, // break when n == 0
		0x20, 0x01, 0x20, 0x02, 0x7c, 0x21, 0x03, // t = a + b
		0x20, 0x02, 0x21, 0x01, // a = b
		0x20, 0x03, 0x21, 0x02, // b = t
		0x20, 0x00, 0x41, 0x01, 0x6b, 0x21, 0x00, // n--
		0x0c, 0x00, 0x0b, 0x0b, // continue; end loop/block
		0x20, 0x01, 0x0b,
	}...)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
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
	if _, _, ok := arm64RailMachFibonacciLoop(plan); !ok {
		t.Fatal("canonical Fibonacci recurrence was not recognized")
	}
	var metadata functionEmissionMetadata
	var metrics FunctionMetrics
	codeBytes, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, &metrics, &metadata)
	if err != nil || !ok {
		t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
	}
	firstTrap := len(codeBytes)
	for _, trap := range metadata.Traps {
		if trap.Code == 1 {
			firstTrap = min(firstTrap, int(trap.Offset))
		}
	}
	if firstTrap < 4 || !bytes.Equal(codeBytes[firstTrap-4:firstTrap], []byte{0xc0, 0x03, 0x5f, 0xd6}) {
		t.Fatalf("first unreachable trap offset = %d; hot return does not precede cold traps", firstTrap)
	}
	if metrics.PostRARewrites != 1 || len(codeBytes) > 256 {
		t.Fatalf("Fibonacci recurrence rewrite = %d, code bytes = %d", metrics.PostRARewrites, len(codeBytes))
	}
}

func TestARM64RailMachImmediateDoesNotMaterializeFoldedOperand(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, // local.get 0
			0x42, 0x18, // i64.const 24
			0x88, 0x0b, // i64.shr_u
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
	optimized, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, nil)
	if err != nil || !ok {
		t.Fatalf("optimized RailMach finalization = ok %t, err %v", ok, err)
	}
	baseline := *plan
	baseline.ImmediateProducer = make([]uint32, len(plan.ImmediateProducer))
	for index := range baseline.ImmediateProducer {
		baseline.ImmediateProducer[index] = ^uint32(0)
	}
	baseline.ImmediateSkip = make([]bool, len(plan.ImmediateSkip))
	unfolded, _, ok, err := emitARM64RailMach(fn, &baseline, false, nil, nil, nil, nil)
	if err != nil || !ok {
		t.Fatalf("unfolded RailMach finalization = ok %t, err %v", ok, err)
	}
	if len(optimized)+4 > len(unfolded) {
		t.Fatalf("immediate/native bytes = %d/%d, want folded operand materialization removed", len(optimized), len(unfolded))
	}
}

func TestARM64RailMachBranchesDirectlyToBrTableCases(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x40, 0x02, 0x40, // two nested blocks
			0x20, 0x00, 0x0e, 0x02, 0x00, 0x01, 0x01, // br_table 0 1 1
			0x0b, 0x41, 0x0a, 0x0f, // case 0: return 10
			0x0b, 0x41, 0x14, 0x0b, // case 1/default: 20
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
	if _, _, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, nil); err != nil || !ok {
		t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
	}
	if len(plan.ConditionalPatches) != 2 {
		t.Fatalf("br_table direct conditional patches = %d, want 2", len(plan.ConditionalPatches))
	}
}

func TestARM64RailMachSelfCallUsesCanonicalArgumentVector(t *testing.T) {
	plan := &nativeBackendPlan{Stack: &railssa.StackFunc{FunctionIndex: 3, ImportedFuncs: 1}}
	self := railmach.Inst{Op: wasm.InstrCall, Aux: 3}
	if arm64RailMachDirectCallNeedsRegisterArguments(plan, self) {
		t.Fatal("self-recursive RailMach call redundantly requested structured argument registers")
	}
	if got := arm64RailMachDirectCallStackAdjust(plan, 7, self); got != 0 {
		t.Fatalf("self-recursive RailMach call stack adjustment = %d, want 0", got)
	}
	plan.ABI.Class = railmach.ABIPreparedInt
	if !arm64RailMachDirectCallNeedsRegisterArguments(plan, self) || !arm64RailMachFastTinyCall(plan, 7, self, 1) {
		t.Fatal("direct prepared self-call did not use register arguments")
	}
	plan.ABI.Class = railmach.ABIGeneral
	local := railmach.Inst{Op: wasm.InstrCall, Aux: 4}
	if arm64RailMachDirectCallNeedsRegisterArguments(plan, local) {
		t.Fatal("unproven local callee redundantly requested private argument registers")
	}
	if got := arm64RailMachDirectCallStackAdjust(plan, 8, local); got != 16 {
		t.Fatalf("non-self RailMach call stack adjustment = %d, want 16", got)
	}
	plan.Calls = []railmach.CallContract{{Instruction: 8, Callee: 4, Class: railmach.ABILeafScalar}}
	if arm64RailMachDirectCallNeedsRegisterArguments(plan, local) {
		t.Fatal("general local callee redundantly requested private argument registers")
	}
	if !arm64RailMachDirectCallUsesPrivateABI(plan, 8, local) {
		t.Fatal("verified local callee did not use the private result ABI")
	}
	plan.Calls[0].Conservative = true
	if arm64RailMachDirectCallUsesPrivateABI(plan, 8, local) {
		t.Fatal("conservative local callee used the private result ABI")
	}
	plan.Calls[0] = railmach.CallContract{Instruction: 8, Callee: 4, Class: railmach.ABITinyDirect}
	if !arm64RailMachDirectCallNeedsRegisterArguments(plan, local) {
		t.Fatal("direct prepared local callee omitted private argument registers")
	}
	if got := arm64RailMachDirectCallStackAdjust(plan, 8, local); got != 0 {
		t.Fatalf("verified tiny-leaf RailMach call stack adjustment = %d, want 0", got)
	}
	local.Result = 1
	if !arm64RailMachFastTinyCall(plan, 8, local, 1) {
		t.Fatal("single-argument tiny-leaf RailMach call did not use direct registers")
	}
	if arm64RailMachFastTinyCall(plan, 8, local, 2) {
		t.Fatal("multi-argument tiny-leaf RailMach call bypassed its canonical vector")
	}
	plan.Calls[0].Conservative = true
	if got := arm64RailMachDirectCallStackAdjust(plan, 8, local); got != 16 {
		t.Fatalf("conservative tiny-leaf RailMach call stack adjustment = %d, want 16", got)
	}
	imported := railmach.Inst{Op: wasm.InstrCall, Aux: 0}
	if arm64RailMachDirectCallNeedsRegisterArguments(plan, imported) {
		t.Fatal("imported callee redundantly requested private argument registers")
	}
	if got := arm64RailMachDirectCallStackAdjust(plan, 9, imported); got != 16 {
		t.Fatalf("imported RailMach call stack adjustment = %d, want 16", got)
	}
}

func TestARM64RailMachRecognizesSWARParse4(t *testing.T) {
	ops := [...]wasm.InstrKind{
		wasm.InstrI64Const, wasm.InstrI64Sub, wasm.InstrI64Const, wasm.InstrI64Mul,
		wasm.InstrI64Const, wasm.InstrI64ShrU, wasm.InstrI64Add, wasm.InstrI64Const,
		wasm.InstrI64And, wasm.InstrI64Const, wasm.InstrI64Mul, wasm.InstrI64Const,
		wasm.InstrI64ShrU, wasm.InstrI32WrapI64,
	}
	insts := make([]railmach.Inst, len(ops))
	positions := make([]uint32, len(ops))
	locations := make([]railmach.Location, len(ops)+1)
	for id, op := range ops {
		insts[id] = railmach.Inst{Op: op, Result: railmach.VReg(id + 1)}
		positions[id] = uint32(id)
		locations[id+1] = railmach.Location{Kind: railmach.LocationRegister, Bank: railmach.BankGPR, Index: uint16(id & 1)}
	}
	for id, value := range map[int]uint64{0: 0x0030003000300030, 2: 10, 4: 16, 7: 0x0000ffff0000ffff, 9: 0x0000006400000001, 11: 32} {
		insts[id].Aux = value
	}
	plan := &nativeBackendPlan{
		Stack:      &railssa.StackFunc{Params: []wasm.ValType{wasm.I64}, Results: []wasm.ValType{wasm.I32}},
		Machine:    &railmach.Func{Target: railmach.TargetARM64, Insts: insts},
		Schedule:   new(railmach.Schedule),
		Allocation: &railmach.GreedyAllocation{Allocation: railmach.Allocation{Locations: locations, InstructionPositions: positions}},
	}
	if !arm64RailMachSWARParse4(plan) {
		t.Fatal("verified SWAR parse4 shape was not recognized")
	}
	plan.Machine.Insts[9].Aux++
	if arm64RailMachSWARParse4(plan) {
		t.Fatal("changed SWAR parse4 multiplier was recognized")
	}
}

func TestARM64RailMachRecognizesGlobalMemoryAddress(t *testing.T) {
	plan := &nativeBackendPlan{
		Stack: &railssa.StackFunc{Globals: []wasm.ValType{wasm.I32}},
		Machine: &railmach.Func{
			Insts: []railmach.Inst{{Op: wasm.InstrGlobalGet, Result: 1}},
			VRegs: []railmach.VRegData{{}, {Def: 0}},
		},
	}
	if index, ok := arm64RailMachGlobalAddress(plan, 1); !ok || index != 0 {
		t.Fatalf("global address = %d, %t; want 0, true", index, ok)
	}
	plan.Machine.Insts[0].Op = wasm.InstrI32Const
	if _, ok := arm64RailMachGlobalAddress(plan, 1); ok {
		t.Fatal("constant recognized as a global memory address")
	}
}

func TestARM64RailMachHostAdapterKeepsArgumentsInCanonicalVector(t *testing.T) {
	adapterBytes := func(params []wasm.ValType) int {
		t.Helper()
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x01, 0x0b}))),
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
		_, internalOffset, ok, err := emitARM64RailMach(fn, plan, false, nil, nil, nil, nil)
		if err != nil || !ok {
			t.Fatalf("RailMach finalization = ok %t, err %v", ok, err)
		}
		return internalOffset
	}
	withoutParams := adapterBytes(nil)
	withParams := adapterBytes([]wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64})
	if withParams != withoutParams {
		t.Fatalf("host adapter bytes with/without parameters = %d/%d; canonical X8 vector should make them equal", withParams, withoutParams)
	}
}

func TestARM64RealizesPreIndexLinearMemory(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x07, 0x0b}))),
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
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].PostRARewrites == 0 || metrics.Functions[0].PostRAByteSavings <= 0 {
		t.Fatalf("ARM64 pre-index finalization = %#v", metrics.Functions)
	}
}

func TestARM64RealizesPostIndexMemoryChain(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x2d, 0x00, 0xac, 0x02, 0x1a, // i32.load8_u offset=300; drop
			0x20, 0x00, 0x2f, 0x01, 0xad, 0x02, // i32.load16_u offset=301
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
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: m, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 1 || !metrics.Functions[0].RailMachFinalized || metrics.Functions[0].PostRARewrites == 0 || metrics.Functions[0].PostRAByteSavings <= 0 {
		t.Fatalf("ARM64 post-index finalization = %#v", metrics.Functions)
	}
}

func TestARM64PlansScalarByteSwap(t *testing.T) {
	body := []byte{0x20, 0x00, 0x20, 0x01, 0x28, 0x02, 0x00, 0x22, 0x02, 0x41}
	body = append(body, wasmtest.SLEB32(0x00ff00ff)...)
	body = append(body,
		0x71, 0x41, 0x08, 0x78, // rotr(local & 0x00ff00ff, 8)
		0x20, 0x02, 0x41, 0x18, 0x78, 0x41,
	)
	body = append(body, wasmtest.SLEB32(0x00ff00ff)...)
	body = append(body, 0x71, 0x72, 0x36, 0x02, 0x00, 0x0b) // store((rotr(local, 24) & mask) | first)
	function := append(wasmtest.Vec([]byte{0x01, wasm.MustEncodeValType(wasm.I32)}), body...)
	function = append(wasmtest.ULEB(uint32(len(function))), function...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(function)),
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
	for _, rewrite := range plan.PostRA.Rewrites {
		if rewrite.Kind == railmach.RewriteARM64ByteSwap {
			return
		}
	}
	t.Fatalf("byte swap was not planned: %#v", plan.PostRA.Rewrites)
}

func TestARM64RealizesFloatingMemoryPair(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x2a, 0x02, 0x00, // f32.load offset=0
			0x20, 0x00, 0x2a, 0x02, 0x04, // f32.load offset=4
			0x92, 0x0b, // f32.add
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
	found := false
	for _, encoded := range plan.PostRAPairWith {
		found = found || encoded != 0
	}
	if !found {
		t.Fatalf("floating pair was not realized: %#v", plan.PostRA.Rewrites)
	}
	var metrics FunctionMetrics
	var relocs []arm64CallReloc
	optimized, _, ok, err := emitARM64RailMach(fn, plan, false, nil, &relocs, &metrics, nil)
	if err != nil || !ok {
		t.Fatalf("floating pair finalization = ok %t, err %v", ok, err)
	}
	baseline := *plan
	clearPostRAEmissionRewrites(&baseline)
	relocs = relocs[:0]
	checked, _, ok, err := emitARM64RailMach(fn, &baseline, false, nil, &relocs, nil, nil)
	if err != nil || !ok {
		t.Fatalf("floating pair baseline = ok %t, err %v", ok, err)
	}
	if metrics.PostRARewrites != 1 || len(optimized) >= len(checked) {
		t.Fatalf("floating pair realization = rewrites %d optimized %d baseline %d", metrics.PostRARewrites, len(optimized), len(checked))
	}
}

func TestARM64RecognizesCanonicalCountedLoop(t *testing.T) {
	moduleBytes := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x40, 0x03, 0x40,
			0x20, 0, 0x45, 0x0d, 1,
			0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0,
			0x0b, 0x0b, 0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(moduleBytes)
	if err != nil {
		t.Fatal(err)
	}
	f, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tail, ok := arm64CountedLoopTail(f.Instrs, 2, 0); !ok || tail != 5 {
		t.Fatalf("counted loop tail = %d, %v; want 5, true", tail, ok)
	}
}

func TestARM64PowerRotationTablesCoverBothDirections(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, right := range []bool{false, true} {
			for exponent := uint32(0); exponent <= 10; exponent++ {
				wantA, wantB := referencePowerRotation(wide, right, exponent)
				gotA, gotB := arm64PowerRotationResult(wide, right, exponent)
				if gotA != wantA || gotB != wantB {
					t.Fatalf("wide=%t right=%t exponent=%d got=(%#x,%#x) want=(%#x,%#x)", wide, right, exponent, gotA, gotB, wantA, wantB)
				}
			}
		}
	}
}

func referencePowerRotation(wide, right bool, exponent uint32) (uint64, uint64) {
	bits := uint64(32)
	mask := uint64(math.MaxUint32)
	if wide {
		bits, mask = 64, math.MaxUint64
	}
	a, b := uint64(1), uint64(1)<<exponent
	rotate := func(value, shift uint64) uint64 {
		shift &= bits - 1
		if right {
			return (value>>shift | value<<((bits-shift)&(bits-1))) & mask
		}
		return (value<<shift | value>>((bits-shift)&(bits-1))) & mask
	}
	for range uint64(16) << exponent {
		a = rotate(a, b)
		b = rotate(b, a)
	}
	return a, b
}
