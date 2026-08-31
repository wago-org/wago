package railssa

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestMetadataClassifiesEffectsTrapsAndEpochs(t *testing.T) {
	f := &StackFunc{ImportedFuncs: 1, Instrs: []StackInstr{
		{Kind: wasm.InstrI32Load, Offset: 1},
		{Kind: wasm.InstrGlobalSet, Offset: 3},
		{Kind: wasm.InstrI32DivS, Offset: 5},
		{Kind: wasm.InstrCall, Offset: 7, aux: 0},
		{Kind: wasm.InstrCallIndirect, Offset: 9},
	}}
	metadata, err := BuildMetadata(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	load := metadata.Instructions[0]
	if load.Reads != HeapLinearMemory || load.Traps != TrapMemoryBounds || load.Obligations&ObligationMemoryBounds == 0 {
		t.Fatalf("load metadata = %#v", load)
	}
	if metadata.Instructions[1].Writes != HeapGlobal || metadata.Instructions[1].Epoch <= load.Epoch {
		t.Fatalf("global.set metadata = %#v", metadata.Instructions[1])
	}
	div := metadata.Instructions[2]
	if div.Traps != TrapIntegerDivideByZero|TrapIntegerOverflow || div.Obligations&ObligationSignedDivisionRange == 0 {
		t.Fatalf("division metadata = %#v", div)
	}
	host := metadata.Instructions[3]
	if host.Flags&(EffectCall|EffectMayReenter|EffectMayThrow) != EffectCall|EffectMayReenter|EffectMayThrow || host.Reads&HeapImportState == 0 {
		t.Fatalf("host call metadata = %#v", host)
	}
	indirect := metadata.Instructions[4]
	if indirect.Traps != TrapTableBounds|TrapIndirectNull|TrapIndirectSignature || indirect.Reads&HeapTable == 0 {
		t.Fatalf("indirect call metadata = %#v", indirect)
	}
	if metadata.Epochs != 6 {
		t.Fatalf("epochs = %d, want 6", metadata.Epochs)
	}
}

func TestSaturatingConversionsDoNotCarryTrapObligations(t *testing.T) {
	f := &StackFunc{Instrs: []StackInstr{{Kind: wasm.InstrI32TruncSatF32S}, {Kind: wasm.InstrI64ExtendI32S}}}
	metadata, err := BuildMetadata(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := metadata.Instructions[0]; got.Traps != 0 || got.Obligations != 0 || got.Flags&EffectMayTrap != 0 {
		t.Fatalf("saturating conversion metadata = %#v", got)
	}
	if got := metadata.Instructions[1]; got.Traps != 0 || got.Obligations != 0 || got.Flags&EffectMayTrap != 0 {
		t.Fatalf("integer extension metadata = %#v", got)
	}
}

func TestContextFreeTrapFree(t *testing.T) {
	pure := &StackFunc{Instrs: []StackInstr{{Kind: wasm.InstrLocalGet}, {Kind: wasm.InstrI32Add}}}
	if !ContextFreeTrapFree(pure, false) {
		t.Fatal("pure integer function requires guest context")
	}
	for _, kind := range []wasm.InstrKind{wasm.InstrI32Load, wasm.InstrGlobalGet, wasm.InstrI32DivU, wasm.InstrUnreachable} {
		if ContextFreeTrapFree(&StackFunc{Instrs: []StackInstr{{Kind: kind}}}, false) {
			t.Fatalf("%s was classified as context-free and trap-free", kind)
		}
	}
	localCall := &StackFunc{Instrs: []StackInstr{{Kind: wasm.InstrCall, aux: 0}}}
	if ContextFreeTrapFree(localCall, false) || !ContextFreeTrapFree(localCall, true) {
		t.Fatal("inlined local-call handling is inconsistent")
	}
	hostCall := &StackFunc{ImportedFuncs: 1, Instrs: []StackInstr{{Kind: wasm.InstrCall, aux: 0}}}
	if ContextFreeTrapFree(hostCall, true) {
		t.Fatal("host call was ignored as an inlined local call")
	}
}

func TestVerifyMetadataRejectsLostTrapOrder(t *testing.T) {
	f := &StackFunc{Instrs: []StackInstr{{Kind: wasm.InstrUnreachable, Offset: 2}}}
	metadata := &Metadata{Instructions: []InstructionMetadata{{Offset: 2, Epoch: 0, Traps: TrapUnreachable, Flags: EffectMayTrap}}, Epochs: 1}
	if err := VerifyMetadata(f, metadata); err == nil {
		t.Fatal("trapping instruction without order obligation accepted")
	}
}

func TestRefineHostEffectsPreservesUnknownAndRefinesDeclaredCall(t *testing.T) {
	f := &StackFunc{ImportedFuncs: 2, Instrs: []StackInstr{
		{Kind: wasm.InstrCall, Offset: 1, aux: 0},
		{Kind: wasm.InstrCall, Offset: 3, aux: 1},
	}}
	metadata, err := BuildMetadata(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	host := []HostEffectContract{
		{Reads: HeapGlobal, Declared: true},
		{},
	}
	if err := RefineHostEffects(f, metadata, host); err != nil {
		t.Fatal(err)
	}
	if got := metadata.Instructions[0]; got.Reads != HeapGlobal || got.Writes != 0 || got.Flags != EffectCall {
		t.Fatalf("declared host metadata = %#v", got)
	}
	if got := metadata.Instructions[1]; got.Reads&HeapHostUnknown == 0 || got.Writes&HeapHostUnknown == 0 || got.Flags&EffectMayReenter == 0 {
		t.Fatalf("undeclared host metadata lost conservative effects: %#v", got)
	}
	if metadata.Instructions[1].Epoch != 0 || metadata.Epochs != 2 {
		t.Fatalf("refined epochs = %#v, count %d", metadata.Instructions, metadata.Epochs)
	}
}

func TestRefineHostEffectsRequiresFunctionImportOrder(t *testing.T) {
	f := &StackFunc{ImportedFuncs: 2, Instrs: []StackInstr{{Kind: wasm.InstrCall, aux: 0}}}
	metadata, err := BuildMetadata(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := RefineHostEffects(f, metadata, []HostEffectContract{{Declared: true}}); err == nil {
		t.Fatal("short host effect array accepted")
	}
}
