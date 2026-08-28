package railssa

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestKnownValueCapacityExcludesAliasesAndValuelessInstructions(t *testing.T) {
	f := &StackFunc{Instrs: []StackInstr{
		{Kind: wasm.InstrNop},
		{Kind: wasm.InstrI32Const},
		{Kind: wasm.InstrLocalSet},
		{Kind: wasm.InstrSelect},
	}}
	locals := &LocalSSA{Definitions: []Definition{
		{},
		{Kind: DefinitionInitial},
		{Kind: DefinitionLocalSet},
		{Kind: DefinitionBlockParam},
	}}
	if got, want := knownValueCapacity(f, locals), 5; got != want {
		t.Fatalf("known value capacity = %d, want %d", got, want)
	}
}

func TestValueFlowCreatesIfResultBlockArgument(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x04, 0x7f,
		0x41, 0x01,
		0x05,
		0x41, 0x02,
		0x0b,
		0x0b,
	})
	f, err := BuildStackFunc(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildCFG(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	locals, err := BuildLocalSSA(f, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := BuildValueFlow(f, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Params) != 1 || flow.Params[0].Slot != 0 || flow.Values[flow.Params[0].Value].Type != wasm.I32 {
		t.Fatalf("stack params = %#v values=%#v", flow.Params, flow.Values)
	}
	if len(flow.EdgeArgs) != 2 || flow.EdgeArgs[0].Argument == flow.EdgeArgs[1].Argument {
		t.Fatalf("stack edge args = %#v", flow.EdgeArgs)
	}
	exit := BlockID(len(cfg.Blocks) - 1)
	if flow.EntryDepths[exit] != 1 || flow.entry(exit)[0] != flow.Params[0].Value {
		t.Fatalf("exit stack depth=%d values=%v param=%d", flow.EntryDepths[exit], flow.entry(exit), flow.Params[0].Value)
	}
}

func TestValueFlowAliasesLocalDefinitions(t *testing.T) {
	m := scalarModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{
		0x20, 0x00,
		0x41, 0x01,
		0x6a,
		0x22, 0x00,
		0x1a,
		0x20, 0x00,
		0x0b,
	})
	f, _ := BuildStackFunc(m, 0)
	cfg, _ := BuildCFG(f, nil)
	locals, _ := BuildLocalSSA(f, cfg, nil)
	flow, err := BuildValueFlow(f, cfg, locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	setDefinition := locals.InstructionValues[3]
	if setDefinition == 0 || flow.LocalDefinitionValues[setDefinition] == 0 || flow.InstructionValues[5] != flow.LocalDefinitionValues[setDefinition] {
		t.Fatalf("local aliases definitions=%v instructions=%v", flow.LocalDefinitionValues, flow.InstructionValues)
	}
	if flow.InstructionValues[2] != flow.InstructionValues[5] {
		t.Fatalf("local set source=%d later get=%d", flow.InstructionValues[2], flow.InstructionValues[5])
	}
}
