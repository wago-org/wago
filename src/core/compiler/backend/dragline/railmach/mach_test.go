package railmach

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func machineModule(params, results []wasm.ValType, body []byte) *wasm.Module {
	typeSec := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results)))
	funcSec := wasmtest.Section(3, wasmtest.Vec([]byte{0}))
	codeSec := wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body)))
	m, err := wasm.DecodeModule(wasmtest.Module(typeSec, funcSec, codeSec))
	if err != nil {
		panic(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		panic(err)
	}
	return m
}

func buildMachineTest(t *testing.T, target Target, m *wasm.Module) *Func {
	t.Helper()
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := railssa.BuildCFG(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := railssa.BuildLocalSSA(stack, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := railssa.BuildValueFlow(stack, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := railssa.BuildSemanticFunc(stack, cfg, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := Build(target, cfg, flow, semantic, nil)
	if err != nil {
		t.Fatal(err)
	}
	return machine
}

func TestDenseRecordSizes(t *testing.T) {
	if got := unsafe.Sizeof(Inst{}); got != 24 {
		t.Fatalf("Inst size = %d, want 24", got)
	}
	if got := unsafe.Sizeof(Operand{}); got != 12 {
		t.Fatalf("Operand size = %d, want 12", got)
	}
}

func TestSelectedOpcodeNamespaceIsTargetSpecific(t *testing.T) {
	if IsSelectedOpcode(wasm.InstrV128Load) || !IsSelectedOpcode(OpAMD64V128Load) || !IsSelectedOpcode(OpARM64V128Load) {
		t.Fatal("selected opcode namespace overlaps generic Wasm operations")
	}
	if SelectedOpcodeTarget(OpAMD64V128Load) != TargetAMD64 || SelectedOpcodeTarget(OpARM64V128Load) != TargetARM64 {
		t.Fatal("selected vector forms have the wrong target")
	}
	f := memoryFixture()
	f.Insts[0].Op = OpAMD64V128Load
	if err := Verify(f); err == nil || !strings.Contains(err.Error(), "invalid arm64 opcode") {
		t.Fatalf("Verify cross-target selected opcode = %v", err)
	}
	f.Insts[0].Op = OpAMD64I32Add
	if err := Verify(f); err == nil || !strings.Contains(err.Error(), "invalid arm64 opcode") {
		t.Fatalf("Verify cross-target selected scalar opcode = %v", err)
	}
}

func TestSelectTargetOpcodesV128Foundation(t *testing.T) {
	for _, test := range []struct {
		name   string
		target Target
		want   []MOpcode
	}{
		{"amd64", TargetAMD64, []MOpcode{OpAMD64V128Const, OpAMD64V128Load, OpAMD64V128And, OpAMD64V128Store}},
		{"arm64", TargetARM64, []MOpcode{OpARM64V128Const, OpARM64V128Load, OpARM64V128And, OpARM64V128Store}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := vectorFoundationFixture(test.target)
			count, err := SelectTargetOpcodes(f)
			if err != nil {
				t.Fatal(err)
			}
			if count != len(test.want) {
				t.Fatalf("selected %d operations, want %d", count, len(test.want))
			}
			for index, want := range test.want {
				if got := f.Insts[index].Op; got != want {
					t.Fatalf("instruction %d opcode = %d, want %d", index, got, want)
				}
			}
		})
	}
}

func TestSelectTargetOpcodesIntegerAddSub(t *testing.T) {
	for _, test := range []struct {
		name   string
		target Target
		want   [4]MOpcode
	}{
		{"amd64", TargetAMD64, [4]MOpcode{OpAMD64I32Add, OpAMD64I64Add, OpAMD64I32Sub, OpAMD64I64Sub}},
		{"arm64", TargetARM64, [4]MOpcode{OpARM64I32Add, OpARM64I64Add, OpARM64I32Sub, OpARM64I64Sub}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, op := range []byte{0x6a, 0x7c, 0x6b, 0x7d} {
				type_ := wasm.I32
				if index&1 != 0 {
					type_ = wasm.I64
				}
				m := machineModule([]wasm.ValType{type_, type_}, []wasm.ValType{type_}, []byte{0x20, 0, 0x20, 1, op, 0x0b})
				f := buildMachineTest(t, test.target, m)
				count, err := SelectTargetOpcodes(f)
				if err != nil {
					t.Fatal(err)
				}
				if count != 1 || len(f.Insts) != 1 || f.Insts[0].Op != test.want[index] {
					t.Fatalf("selected instructions = %#v, count %d, want %d", f.Insts, count, test.want[index])
				}
				if got := SemanticOpcode(f.Insts[0].Op); got != [4]MOpcode{wasm.InstrI32Add, wasm.InstrI64Add, wasm.InstrI32Sub, wasm.InstrI64Sub}[index] {
					t.Fatalf("instruction %d semantic opcode = %d", index, got)
				}
			}
		})
	}
}

func vectorFoundationFixture(target Target) *Func {
	f := &Func{
		Target: target,
		VRegs:  []VRegData{{}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeV128, Bank: BankFPR}, {Type: TypeV128, Bank: BankFPR}, {Type: TypeV128, Bank: BankFPR}},
		Insts: []Inst{
			{Op: wasm.InstrV128Const, Result: 2, Source: 1},
			{Op: wasm.InstrV128Load, Result: 3, Source: 2, Aux: 8, OperandStart: 0, OperandCount: 1},
			{Op: wasm.InstrV128And, Result: 4, Source: 3, OperandStart: 1, OperandCount: 2},
			{Op: wasm.InstrV128Store, Source: 4, Aux: 24, OperandStart: 3, OperandCount: 2},
		},
		Operands: []Operand{
			{Reg: 1, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse},
			{Reg: 2, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse}, {Reg: 3, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse},
			{Reg: 1, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse}, {Reg: 4, Fixed: NoFixedReg, Bank: BankFPR, Flags: OperandUse},
		},
		Blocks: []Block{{InstCount: 4}},
		Memory: []MemoryAccess{
			{Instruction: 1, AddressValue: 1, Offset: 8, TrapSite: 2, SemanticWidth: 16, EncodedWidth: 16, Alignment: 1},
			{Instruction: 3, AddressValue: 1, Offset: 24, TrapSite: 4, SemanticWidth: 16, EncodedWidth: 16, Alignment: 1},
		},
	}
	for index := range f.Insts {
		f.SIMD = append(f.SIMD, railssa.SemanticSIMDImmediate{Instruction: uint32(index)})
	}
	return f
}

func TestVerifyMemoryAccessIdentityAndWidth(t *testing.T) {
	f := memoryFixture()
	if err := Verify(f); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		edit func(*Func)
		want string
	}{
		{"widened access", func(f *Func) { f.Memory[0].EncodedWidth = 16 }, "touches 16 bytes"},
		{"different address", func(f *Func) { f.Memory[0].AddressValue = 2 }, "different address"},
		{"different offset", func(f *Func) { f.Memory[0].Offset++ }, "different constant offset"},
		{"different trap", func(f *Func) { f.Memory[0].TrapSite++ }, "different trap source"},
		{"invalid alignment", func(f *Func) { f.Memory[0].Alignment = 3 }, "invalid 3-byte alignment"},
		{"missing descriptor", func(f *Func) { f.Memory = nil }, "no access descriptor"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := memoryFixture()
			test.edit(candidate)
			if err := Verify(candidate); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Verify = %v, want %q", err, test.want)
			}
		})
	}
}

func memoryFixture() *Func {
	return &Func{
		Target:   TargetARM64,
		VRegs:    []VRegData{{}, {Type: TypeI32, Bank: BankGPR}, {Type: TypeI32, Bank: BankGPR}},
		Operands: []Operand{{Reg: 1, Fixed: NoFixedReg, Bank: BankGPR, Flags: OperandUse}},
		Insts:    []Inst{{Op: wasm.InstrI32Load, Aux: 24, Result: 2, OperandCount: 1, Source: 7}},
		Blocks:   []Block{{InstCount: 1}},
		Memory:   []MemoryAccess{{Instruction: 0, AddressValue: 1, Offset: 24, TrapSite: 7, SemanticWidth: 4, EncodedWidth: 4, Alignment: 1}},
	}
}

func TestBuildPreservesV128InFPRBank(t *testing.T) {
	body := []byte{0xfd, 0x0c}
	body = append(body, make([]byte, 16)...)
	body = append(body, 0x0b)
	m := machineModule(nil, []wasm.ValType{wasm.V128}, body)
	for _, target := range []Target{TargetAMD64, TargetARM64} {
		f := buildMachineTest(t, target, m)
		if len(f.SIMD) != 1 || f.SIMD[0].Bytes != [16]byte{} || f.SIMD[0].Class != wasm.SIMDEffectConst {
			t.Fatalf("%s SIMD constant = %#v", target, f.SIMD)
		}
		if len(f.Results) != 1 {
			t.Fatalf("%s results = %#v", target, f.Results)
		}
		result := f.VRegs[f.Results[0]]
		if result.Type != TypeV128 || result.Bank != BankFPR || !result.Type.IsVector() {
			t.Fatalf("%s v128 result = %#v", target, result)
		}
		if err := Verify(f); err != nil {
			t.Fatalf("%s verify: %v", target, err)
		}
	}
}

func TestBuildPreservesSparseSIMDMemorySemantics(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xfd, 0x07, 0x00, 0x09, 0x0b}))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	f := buildMachineTest(t, TargetARM64, m)
	if len(f.SIMD) != 1 || f.SIMD[0].Offset != 9 || f.SIMD[0].Class != wasm.SIMDEffectLoad {
		t.Fatalf("SIMD immediate = %#v", f.SIMD)
	}
	if len(f.Memory) != 1 || f.Memory[0].SemanticWidth != 1 || f.Memory[0].EncodedWidth != 1 || f.Memory[0].Offset != 9 || f.Memory[0].AddressValue == 0 {
		t.Fatalf("SIMD memory access = %#v", f.Memory)
	}
}

func TestVerifyRejectsMachineTypeInWrongBank(t *testing.T) {
	f := &Func{Target: TargetARM64, VRegs: []VRegData{{}, {Type: TypeV128, Bank: BankGPR}}}
	if err := Verify(f); err == nil || !strings.Contains(err.Error(), "invalid type/bank") {
		t.Fatalf("Verify wrong-bank v128 = %v", err)
	}
}

func TestBuildPreservesBlockArgumentsAndSourceOrder(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	if len(f.Transfers) != 2 {
		t.Fatalf("transfers = %#v", f.Transfers)
	}
	if err := ScheduleSourceStable(f); err != nil {
		t.Fatal(err)
	}
	dump := Dump(f)
	if !strings.Contains(dump, "target arm64") || !strings.Contains(dump, "edge") || !strings.Contains(dump, "I32Const") {
		t.Fatalf("dump:\n%s", dump)
	}
}

func TestBuildElidesVerifiedDominatingCrossBlockDefinitions(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00, 0x41, 0x07, 0x6a, 0x1a,
		0x20, 0x00,
		0x04, 0x7f,
		0x20, 0x00, 0x41, 0x07, 0x6a,
		0x05,
		0x41, 0x00,
		0x0b,
		0x0b,
	})
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := railssa.BuildCFG(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := railssa.BuildLocalSSA(stack, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := railssa.BuildValueFlow(stack, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := railssa.BuildSemanticFunc(stack, cfg, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := railssa.BuildMetadata(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	simplified, err := railssa.SparseSimplify(stack, cfg, flow, semantic, metadata, railssa.DefaultSimplifyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := BuildWithSimplify(TargetARM64, cfg, flow, semantic, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	var crossBlock int
	for value, alias := range simplified.Aliases {
		canonical := resolveMachineAlias(simplified.Aliases, alias)
		if value == 0 || canonical == railssa.FlowValueID(value) || flow.Values[value].Kind != railssa.FlowValueInstruction || flow.Values[canonical].Kind != railssa.FlowValueInstruction || flow.Values[value].Block == flow.Values[canonical].Block {
			continue
		}
		crossBlock++
		if machine.VRegs[value].Flags&VRegElided == 0 {
			t.Fatalf("dominated cross-block alias v%d -> v%d was retained", value, canonical)
		}
		for instructionID := range machine.Insts {
			for _, operand := range machine.InstructionOperands(uint32(instructionID)) {
				if operand.Reg == VReg(value) {
					t.Fatalf("instruction %d still uses elided cross-block alias v%d", instructionID, value)
				}
			}
		}
	}
	if crossBlock == 0 {
		t.Fatal("fixture produced no cross-block instruction alias")
	}
}

func TestBuildElidesVerifiedTrivialBlockParameters(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x02, 0x40,
		0x03, 0x40,
		0x20, 0x00,
		0x21, 0x00,
		0x20, 0x00,
		0x0d, 0x01,
		0x0c, 0x00,
		0x0b,
		0x0b,
		0x20, 0x00,
		0x0b,
	})
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := railssa.BuildCFG(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := railssa.BuildLocalSSA(stack, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := railssa.BuildValueFlow(stack, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	semantic, err := railssa.BuildSemanticFunc(stack, cfg, flow, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := railssa.BuildMetadata(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	simplified, err := railssa.SparseSimplify(stack, cfg, flow, semantic, metadata, railssa.DefaultSimplifyConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := BuildWithSimplify(TargetARM64, cfg, flow, semantic, simplified, nil)
	if err != nil {
		t.Fatal(err)
	}
	elided := 0
	for value, record := range flow.Values {
		if record.Kind != railssa.FlowValueBlockParam || resolveMachineAlias(simplified.Aliases, railssa.FlowValueID(value)) == railssa.FlowValueID(value) {
			continue
		}
		elided++
		if machine.VRegs[value].Flags&VRegElided == 0 {
			t.Fatalf("trivial block parameter v%d was retained", value)
		}
		for _, transfer := range machine.Transfers {
			if transfer.Dst == VReg(value) {
				t.Fatalf("trivial block parameter v%d retains transfer %#v", value, transfer)
			}
		}
	}
	if elided == 0 {
		t.Fatal("fixture produced no trivial block parameter")
	}
}

func TestAMD64ShiftCountIsFixed(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x20, 0x01,
		0x86,
		0x0b,
	})
	amd := buildMachineTest(t, TargetAMD64, m)
	if len(amd.Insts) != 1 {
		t.Fatalf("instructions = %#v", amd.Insts)
	}
	operands := amd.InstructionOperands(0)
	if len(operands) != 2 || operands[1].Flags&OperandFixed == 0 || operands[1].Fixed != 1 {
		t.Fatalf("AMD64 shift operands = %#v", operands)
	}
	arm := buildMachineTest(t, TargetARM64, m)
	if arm.InstructionOperands(0)[1].Flags&OperandFixed != 0 {
		t.Fatalf("ARM64 shift count unexpectedly fixed: %#v", arm.InstructionOperands(0)[1])
	}
}

func TestColdConstantUseDoesNotExtendAllocatedInterval(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x41, 0x07,
		0x20, 0x00,
		0x6a,
		0x0b,
	})
	f := buildMachineTest(t, TargetARM64, m)
	if len(f.Insts) != 2 || f.Insts[0].Op != wasm.InstrI32Const || f.Insts[1].Op != wasm.InstrI32Add {
		t.Fatalf("instructions = %#v", f.Insts)
	}
	value := f.Insts[0].Result
	pressure := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(value), Aux: 7, Kind: railssa.RematConstant}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(value), Instruction: 1, HotWeight: 8, ColdWeight: 1}},
	}
	committed, err := ApplyColdRematerialization(f, pressure, nil)
	if err != nil {
		t.Fatal(err)
	}
	if committed != 1 || f.InstructionOperands(1)[0].Flags&OperandColdRemat == 0 {
		t.Fatalf("committed=%d operands=%#v", committed, f.InstructionOperands(1))
	}
	allocation, err := AllocateLinearQ(f, LinearQConfig{GPRs: 2, FPRs: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, interval := range allocation.Intervals {
		if interval.Reg == value {
			t.Fatalf("cold-only constant retained interval %#v", interval)
		}
	}
}

func TestColdExtensionAndAffineUsesCommitWhenTargetLegal(t *testing.T) {
	extendModule := machineModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0xac,
		0x42, 0x03,
		0x7c,
		0x0b,
	})
	extend := buildMachineTest(t, TargetARM64, extendModule)
	extended := extend.Insts[0].Result
	extendPlan := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(extended), Base: railssa.FlowValueID(extend.InstructionOperands(0)[0].Reg), Kind: railssa.RematExtend}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(extended), Instruction: 2, HotWeight: 8, ColdWeight: 1}},
	}
	if committed, err := ApplyColdRematerialization(extend, extendPlan, nil); err != nil || committed != 1 || extend.InstructionOperands(2)[0].Flags&OperandColdRemat == 0 {
		t.Fatalf("extension committed=%d err=%v operands=%#v", committed, err, extend.InstructionOperands(2))
	}

	affineModule := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x42, 0x03,
		0x7c,
		0x42, 0x02,
		0x7e,
		0x0b,
	})
	affine := buildMachineTest(t, TargetARM64, affineModule)
	affineValue := affine.Insts[1].Result
	affinePlan := &railssa.PressurePlan{
		Remats:   []railssa.RematRecipe{{Value: railssa.FlowValueID(affineValue), Base: railssa.FlowValueID(affine.InstructionOperands(1)[0].Reg), Aux: 3, Kind: railssa.RematAffine}},
		ColdUses: []railssa.ColdUse{{Value: railssa.FlowValueID(affineValue), Instruction: 3, HotWeight: 8, ColdWeight: 1}},
	}
	priced := &RematPlan{Decisions: []RematDecision{{Value: affineValue, Base: affine.InstructionOperands(1)[0].Reg, RecipeCost: 2, SpillCost: 20, Profitable: true}}}
	if committed, err := ApplyColdRematerialization(affine, affinePlan, priced); err != nil || committed != 1 || affine.InstructionOperands(3)[0].Flags&OperandColdRemat == 0 {
		t.Fatalf("affine committed=%d err=%v operands=%#v", committed, err, affine.InstructionOperands(3))
	}
}
