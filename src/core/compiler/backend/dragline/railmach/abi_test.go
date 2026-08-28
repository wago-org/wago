package railmach

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func buildABITest(t *testing.T, target Target, m *wasm.Module) (*Func, *GreedyAllocation, *railssa.Metadata) {
	t.Helper()
	stack, err := railssa.BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := railssa.BuildCFG(stack, nil)
	locals, _ := railssa.BuildLocalSSA(stack, cfg, nil)
	flow, _ := railssa.BuildValueFlow(stack, cfg, locals, nil)
	semantic, _ := railssa.BuildSemanticFunc(stack, cfg, flow, nil)
	machine, err := Build(target, cfg, flow, semantic, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := AllocateGreedyP(machine, DefaultGreedyConfig(target), nil)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := railssa.BuildMetadata(stack, nil)
	if err != nil {
		t.Fatal(err)
	}
	return machine, allocation, metadata
}

func TestAnalyzeABIAndRefineDirectCall(t *testing.T) {
	m := machineModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, []byte{
		0x20, 0x00,
		0x10, 0x00,
		0x0b,
	})
	f, allocation, metadata := buildABITest(t, TargetAMD64, m)
	contract, calls, err := AnalyzeABI(f, allocation, metadata, 0)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Params != 1 || contract.Results != 1 || contract.RegisterResults != 1 || !contract.HasCall || len(calls) != 1 {
		t.Fatalf("contract=%#v calls=%#v", contract, calls)
	}
	callee := ABIContract{Class: ABITinyDirect, GPRClobbers: 3, FPRClobbers: 4}
	if refined := RefineCallContracts(calls, []ABIContract{callee}, 0); refined != 1 {
		t.Fatalf("refined = %d calls=%#v", refined, calls)
	}
	if calls[0].GPRClobbers != 3 || calls[0].FPRClobbers != 4 || calls[0].Class != ABITinyDirect || calls[0].Conservative {
		t.Fatalf("refined call = %#v", calls[0])
	}
}

func TestFrameForAllocationUsesRegisterPrefixForMultipleResults(t *testing.T) {
	allocation := new(GreedyAllocation)
	requirements, layout, err := FrameForAllocation(ABIContract{Results: 6, RegisterResults: 4}, allocation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if requirements.ResultAreaBytes != 48 || requirements.RuntimeBytes != 8 || layout.ResultAreaOffset != 0 || layout.RuntimeOffset != 48 || layout.TotalBytes != 64 {
		t.Fatalf("requirements=%#v layout=%#v", requirements, layout)
	}
	if _, _, err := FrameForAllocation(ABIContract{Results: 2, RegisterResults: 3}, allocation, 8); err == nil {
		t.Fatal("invalid multi-result register prefix was accepted")
	}
}

func TestFrameForAllocationReservesCanonicalOutgoingCallVector(t *testing.T) {
	requirements, layout, err := FrameForAllocation(ABIContract{}, new(GreedyAllocation), 11)
	if err != nil {
		t.Fatal(err)
	}
	if requirements.CallAreaBytes != 88 || layout.CallAreaOffset != 0 || layout.TotalBytes != 96 {
		t.Fatalf("requirements=%#v layout=%#v", requirements, layout)
	}
}

func TestAnalyzeABIAssignsBoundedMultiResultRegisterPrefix(t *testing.T) {
	m := machineModule(nil, []wasm.ValType{wasm.I64}, []byte{0x42, 0x01, 0x0b})
	f, allocation, metadata := buildABITest(t, TargetARM64, m)
	result := f.Results[0]
	f.Results = []VReg{result, result, result, result, result, result}
	contract, _, err := AnalyzeABI(f, allocation, metadata, 0)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Results != 6 || contract.RegisterResults != 4 || contract.GPRClobbers&0xf != 0xf {
		t.Fatalf("multi-result contract = %#v", contract)
	}
}

func TestPropagateCallClobbersUsesOnlyVolatileRegisters(t *testing.T) {
	contract := ABIContract{}
	config := GreedyConfig{CallerGPRs: 2, CallerFPRs: 1}
	PropagateCallClobbers(&contract, []CallContract{{GPRClobbers: 0b1101, FPRClobbers: 0b11}}, config)
	if contract.GPRClobbers != 0b01 || contract.FPRClobbers != 0b1 || contract.CalleeGPRs != 0 || contract.CalleeFPRs != 0 {
		t.Fatalf("propagated contract = %#v", contract)
	}
}

func TestAnalyzeABIAccountsForRegionalFragmentRegisters(t *testing.T) {
	m := machineModule(nil, []wasm.ValType{wasm.I64}, []byte{0x42, 0x01, 0x0b})
	f, allocation, metadata := buildABITest(t, TargetAMD64, m)
	allocation.Fragments = append(allocation.Fragments, AllocationFragment{
		Reg: 1, Location: Location{Kind: LocationRegister, Bank: BankGPR, Index: 7},
	})
	contract, _, err := AnalyzeABI(f, allocation, metadata, 0)
	if err != nil {
		t.Fatal(err)
	}
	if contract.GPRClobbers&(uint64(1)<<7) == 0 || contract.CalleeGPRs&(uint64(1)<<7) == 0 {
		t.Fatalf("fragment register missing from contract: %#v", contract)
	}
}

func TestComposeFrameIsAlignedAndIncludesCalleeSaves(t *testing.T) {
	requirements := FrameRequirements{SpillSlots: 3, RootSlots: 1, CalleeGPRs: 0b101, CalleeFPRs: 0b10, CallAreaBytes: 24, RuntimeBytes: 8}
	layout, err := ComposeFrame(requirements)
	if err != nil {
		t.Fatal(err)
	}
	if layout.SpillBytes != 24 || layout.RootBytes != 8 || layout.CalleeSaveBytes != 24 || layout.TotalBytes&15 != 0 || layout.TotalBytes < 88 {
		t.Fatalf("layout = %#v", layout)
	}
}
