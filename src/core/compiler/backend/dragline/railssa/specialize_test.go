package railssa

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func specializationFacts(instructions []StackInstr, imported uint32) (*StackFunc, *SemanticFunc, *Metadata, *SimplifyResult) {
	f := &StackFunc{Instrs: instructions, ImportedFuncs: imported, FuncCount: imported + 16}
	semantic := &SemanticFunc{Insts: make([]SemanticInst, len(instructions))}
	for i, instruction := range instructions {
		semantic.Insts[i] = SemanticInst{Op: instruction.Kind, Source: uint32(i)}
	}
	metadata, err := BuildMetadata(f, nil)
	if err != nil {
		panic(err)
	}
	return f, semantic, metadata, &SimplifyResult{}
}

func validSpecializationProfile(site profile.Site, targets ...profile.TargetCount) *profile.Module {
	return &profile.Module{
		Version: profile.Version,
		Source:  profile.SourceInstrumented,
		Phase:   profile.PhaseSteady,
		CallTargets: []profile.TargetHistogram{{
			Site: site, Targets: targets,
		}},
	}
}

func TestPlanSpecializationRecordsLocalAndHostCalls(t *testing.T) {
	f, semantic, metadata, simplified := specializationFacts([]StackInstr{
		{Kind: wasm.InstrCall, Offset: 3, aux: 0},
		{Kind: wasm.InstrCall, Offset: 5, aux: 2},
	}, 1)
	host := []HostEffectContract{{Reads: HeapGlobal, Writes: HeapRuntimeState, Flags: EffectMayThrow, Declared: true}}
	inputs := SpecializationInputs{FunctionIndex: 7, Host: host}
	plan, err := PlanSpecialization(f, semantic, metadata, simplified, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("entries = %#v", plan.Entries)
	}
	if got := plan.Entries[0]; got.Kind != SpecializeHostEffects || got.Reads != HeapGlobal || got.Writes != HeapRuntimeState || got.Flags != EffectMayThrow|EffectCall {
		t.Fatalf("host specialization = %#v", got)
	}
	if got := plan.Entries[1]; got.Kind != SpecializeSameInstanceCall || got.Target != 2 {
		t.Fatalf("same-instance specialization = %#v", got)
	}
	plan.Entries[0].Flags = 0
	if err := VerifySpecialization(f, semantic, metadata, simplified, inputs, plan); err == nil {
		t.Fatal("corrupt host effect flags accepted")
	}
}

func TestPlanSpecializationRequiresDominantIndirectTarget(t *testing.T) {
	f, semantic, metadata, simplified := specializationFacts([]StackInstr{{Kind: wasm.InstrCallIndirect, Offset: 11}}, 0)
	site := profile.Site{Function: 4, Offset: 11}
	for _, test := range []struct {
		name    string
		targets []profile.TargetCount
		want    bool
	}{
		{name: "too few", targets: []profile.TargetCount{{Function: 3, Count: 9}}, want: false},
		{name: "below threshold", targets: []profile.TargetCount{{Function: 3, Count: 89}, {Function: 8, Count: 11}}, want: false},
		{name: "exact threshold", targets: []profile.TargetCount{{Function: 3, Count: 90}, {Function: 8, Count: 10}}, want: true},
		{name: "overflow safe", targets: []profile.TargetCount{{Function: 3, Count: math.MaxUint64}}, want: true},
		{name: "saturated total", targets: []profile.TargetCount{{Function: 3, Count: math.MaxUint64}, {Function: 8, Count: 1}}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			observations := validSpecializationProfile(site, test.targets...)
			plan, err := PlanSpecialization(f, semantic, metadata, simplified, SpecializationInputs{FunctionIndex: 4, Observations: observations}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(plan.Entries) == 1; got != test.want {
				t.Fatalf("specialized = %t, want %t: %#v", got, test.want, plan.Entries)
			}
		})
	}
}

func TestPlanSpecializationRequiresLocalIndirectTarget(t *testing.T) {
	f, semantic, metadata, simplified := specializationFacts([]StackInstr{{Kind: wasm.InstrCallIndirect, Offset: 11}}, 2)
	f.FuncCount = 5
	site := profile.Site{Function: 4, Offset: 11}
	for _, target := range []uint32{0, 1, 5, math.MaxUint32} {
		observations := validSpecializationProfile(site, profile.TargetCount{Function: target, Count: 10})
		plan, err := PlanSpecialization(f, semantic, metadata, simplified, SpecializationInputs{FunctionIndex: 4, Observations: observations}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Entries) != 0 {
			t.Fatalf("target %d specialization = %#v, want none", target, plan.Entries)
		}
	}
	observations := validSpecializationProfile(site, profile.TargetCount{Function: 2, Count: 10})
	plan, err := PlanSpecialization(f, semantic, metadata, simplified, SpecializationInputs{FunctionIndex: 4, Observations: observations}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Entries) != 1 || plan.Entries[0].Target != 2 {
		t.Fatalf("local specialization = %#v", plan.Entries)
	}
}

func TestPlanSpecializationPreservesBoundsCertificate(t *testing.T) {
	f, semantic, metadata, simplified := specializationFacts([]StackInstr{{Kind: wasm.InstrI32Load, Offset: 2}}, 0)
	simplified.Bounds = []BoundsCertificate{{Instruction: 0}}
	inputs := SpecializationInputs{}
	plan, err := PlanSpecialization(f, semantic, metadata, simplified, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Entries) != 1 || plan.Entries[0].Kind != SpecializePreservedBounds || plan.Entries[0].Certificate != 1 {
		t.Fatalf("entries = %#v", plan.Entries)
	}
	plan.Entries[0].Certificate = 0
	if err := VerifySpecialization(f, semantic, metadata, simplified, inputs, plan); err == nil {
		t.Fatal("invalid bounds certificate accepted")
	}
}

func TestPlanSpecializationRecordsAndReplaysExplicitGCFacts(t *testing.T) {
	refType := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 3}), false))
	instruction := StackInstr{Kind: wasm.InstrCall, meta: stackInstrResult}
	f := &StackFunc{Instrs: []StackInstr{instruction}, ResultTypes: []wasm.ValType{refType}, MultiResults: []MultiResultRange{{Instruction: 0, Count: 1}}, ImportedFuncs: 1, FuncCount: 1}
	semantic := &SemanticFunc{Insts: []SemanticInst{{Op: wasm.InstrCall, Source: 0, Result: 1, Aux: uint64(1) << 32}}}
	metadata := &Metadata{Instructions: make([]InstructionMetadata, 1)}
	simplified := &SimplifyResult{}
	fact := codegen.ExactGCRefFact(3, 9, codegen.GCHeapStruct).WithFreshness(codegen.GCFreshUnpublished)
	inputs := SpecializationInputs{GCValues: []GCValueFact{{Instruction: 0, Fact: fact}}}
	plan, err := PlanSpecialization(f, semantic, metadata, simplified, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Entries) != 2 || plan.Entries[0].Kind != SpecializeExactGCType || plan.Entries[0].Target != 3 || plan.Entries[1].Kind != SpecializeFreshObject || plan.Entries[1].Certificate != 9 {
		t.Fatalf("GC specializations = %#v", plan.Entries)
	}
	plan.Entries[1].Certificate++
	if err := VerifySpecialization(f, semantic, metadata, simplified, inputs, plan); err == nil {
		t.Fatal("corrupt fresh-object certificate accepted")
	}
}

func TestPlanSpecializationRejectsInvalidGCFacts(t *testing.T) {
	refType := wasm.RefVal(wasm.AbsRef(wasm.HeapStruct))
	instruction := StackInstr{Kind: wasm.InstrCall, meta: stackInstrResult}
	f := &StackFunc{Instrs: []StackInstr{instruction}, ResultTypes: []wasm.ValType{refType}, MultiResults: []MultiResultRange{{Instruction: 0, Count: 1}}, ImportedFuncs: 1, FuncCount: 1}
	semantic := &SemanticFunc{Insts: []SemanticInst{{Op: wasm.InstrCall, Source: 0, Result: 1, Aux: uint64(1) << 32}}}
	metadata := &Metadata{Instructions: make([]InstructionMetadata, 1)}
	simplified := &SimplifyResult{}
	for _, fact := range []codegen.GCRefFact{
		codegen.ExactGCRefFact(3, 0, codegen.GCHeapStruct).WithFreshness(codegen.GCFreshUnpublished),
		codegen.ExactGCRefFact(3, 1, codegen.GCHeapFunc).WithFreshness(codegen.GCFreshUnpublished),
	} {
		if _, err := PlanSpecialization(f, semantic, metadata, simplified, SpecializationInputs{GCValues: []GCValueFact{{Instruction: 0, Fact: fact}}}, nil); err == nil {
			t.Fatalf("invalid fresh fact accepted: %#v", fact)
		}
	}
	if _, err := PlanSpecialization(f, semantic, metadata, simplified, SpecializationInputs{GCValues: []GCValueFact{{Instruction: 1, Fact: codegen.NewGCRefFact(codegen.GCKnownNull, codegen.GCHeapStruct)}}}, nil); err == nil {
		t.Fatal("out-of-range GC fact accepted")
	}
}
