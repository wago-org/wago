package railssa

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type HostEffectContract struct {
	Reads    HeapMask
	Writes   HeapMask
	Flags    EffectFlags
	Declared bool
}

type SpecializationKind uint8

const (
	SpecializeInvalid SpecializationKind = iota
	SpecializeSameInstanceCall
	SpecializeHostEffects
	SpecializePreservedBounds
	SpecializeIndirectTarget
	SpecializeExactGCType
	SpecializeFreshObject
)

type Specialization struct {
	Instruction uint32
	Target      uint32
	Certificate uint32
	Result      uint16
	Reads       HeapMask
	Writes      HeapMask
	Flags       EffectFlags
	Kind        SpecializationKind
	GCFact      codegen.GCRefFact
}

type SpecializationPlan struct {
	Entries []Specialization
}

// GCValueFact is one explicit fact for a source-ordered semantic result. Facts
// are sparse and must be strictly sorted by instruction then result.
type GCValueFact struct {
	Instruction uint32
	Result      uint16
	Fact        codegen.GCRefFact
}

// SpecializationInputs groups immutable runtime/profile facts consumed by the
// specialization module. Nil slices and pointers retain conservative behavior.
type SpecializationInputs struct {
	FunctionIndex uint32
	Host          []HostEffectContract
	Observations  *profile.Module
	GCValues      []GCValueFact
}

// ProduceGCValueFacts derives facts guaranteed by an instruction's Wasm
// semantics. Allocation results have an exact concrete heap type, are non-null,
// and remain unpublished at the defining instruction. The sparse output is
// semantic-instruction ordered and reusable across functions.
func ProduceGCValueFacts(f *StackFunc, semantic *SemanticFunc, reuse []GCValueFact) ([]GCValueFact, error) {
	facts := reuse[:0]
	if f == nil || f.Module == nil || semantic == nil {
		return facts, fmt.Errorf("railssa: GC fact production requires function and semantic SSA")
	}
	identity := uint32(0)
	for semanticID, instruction := range semantic.Insts {
		source := f.Instrs[instruction.Source]
		switch instruction.Op {
		case wasm.InstrStructNew, wasm.InstrStructNewDefault,
			wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed,
			wasm.InstrArrayNewData, wasm.InstrArrayNewElem:
		default:
			continue
		}
		if instruction.ResultCount() != 1 {
			return facts, fmt.Errorf("railssa: allocating instruction %d has %d results", semanticID, instruction.ResultCount())
		}
		typ, ok := f.InstructionResultType(instruction.Source, source, 0)
		if !ok || typ.Kind() != wasm.ValRef || typ.Ref().Nullable() || typ.Ref().Heap().Kind() != wasm.HeapTypeIndex {
			return facts, fmt.Errorf("railssa: allocating instruction %d has no exact non-null indexed result", semanticID)
		}
		typeIndex := typ.Ref().Heap().Type().Index
		kind, ok := f.Module.GCCompositeKind(typeIndex)
		if !ok || kind != wasm.CompStruct && kind != wasm.CompArray {
			return facts, fmt.Errorf("railssa: allocating instruction %d type %d is not a collector composite", semanticID, typeIndex)
		}
		if identity == codegen.MaxGCRefFactIdentity {
			continue
		}
		identity++
		heap := codegen.GCHeapStruct
		if kind == wasm.CompArray {
			heap = codegen.GCHeapArray
		}
		fact := codegen.ExactGCRefFact(typeIndex, identity, heap).WithFreshness(codegen.GCFreshUnpublished)
		if instruction.Op == wasm.InstrArrayNewFixed {
			fact = fact.WithKnownArrayLength(source.Params())
		}
		facts = append(facts, GCValueFact{Instruction: uint32(semanticID), Fact: fact})
	}
	if err := validateGCValueFacts(f, semantic, facts); err != nil {
		return facts, err
	}
	return facts, nil
}

// PlanSpecialization consumes only explicit runtime/profile facts. It never
// guesses an import contract or indirect target, and every entry remains tied
// to the original Wasm source instruction.
func PlanSpecialization(f *StackFunc, semantic *SemanticFunc, metadata *Metadata, simplified *SimplifyResult, inputs SpecializationInputs, reuse *SpecializationPlan) (*SpecializationPlan, error) {
	if f == nil || semantic == nil || metadata == nil || simplified == nil {
		return nil, fmt.Errorf("railssa: specialization requires semantic facts")
	}
	if inputs.Observations != nil {
		if err := inputs.Observations.Validate(); err != nil {
			return nil, err
		}
	}
	if err := validateGCValueFacts(f, semantic, inputs.GCValues); err != nil {
		return nil, err
	}
	if reuse == nil {
		reuse = new(SpecializationPlan)
	}
	entries := reuse.Entries[:0]
	*reuse = SpecializationPlan{Entries: entries}
	gcIndex := 0
	for semanticID, instruction := range semantic.Insts {
		source := f.Instrs[instruction.Source]
		switch instruction.Op {
		case wasm.InstrCall:
			target := source.U32()
			if target >= f.ImportedFuncs {
				reuse.Entries = append(reuse.Entries, Specialization{Instruction: uint32(semanticID), Target: target, Reads: metadata.Instructions[instruction.Source].Reads, Writes: metadata.Instructions[instruction.Source].Writes, Flags: metadata.Instructions[instruction.Source].Flags, Kind: SpecializeSameInstanceCall})
			} else if int(target) < len(inputs.Host) && inputs.Host[target].Declared {
				contract := inputs.Host[target]
				reuse.Entries = append(reuse.Entries, Specialization{Instruction: uint32(semanticID), Target: target, Reads: contract.Reads, Writes: contract.Writes, Flags: contract.Flags | EffectCall, Kind: SpecializeHostEffects})
			}
		case wasm.InstrCallIndirect:
			if inputs.Observations != nil {
				site := profile.Site{Function: inputs.FunctionIndex, Offset: source.Offset}
				if target, ok := dominantProfileTarget(inputs.Observations, site); ok && target >= f.ImportedFuncs && target < f.FuncCount {
					reuse.Entries = append(reuse.Entries, Specialization{Instruction: uint32(semanticID), Target: target, Reads: metadata.Instructions[instruction.Source].Reads, Writes: metadata.Instructions[instruction.Source].Writes, Flags: metadata.Instructions[instruction.Source].Flags, Kind: SpecializeIndirectTarget})
				}
			}
		}
		for gcIndex < len(inputs.GCValues) && inputs.GCValues[gcIndex].Instruction == uint32(semanticID) {
			value := inputs.GCValues[gcIndex]
			if typ, exact := value.Fact.ExactType(); exact {
				reuse.Entries = append(reuse.Entries, Specialization{Instruction: uint32(semanticID), Target: typ, Result: value.Result, Kind: SpecializeExactGCType, GCFact: value.Fact})
			}
			if value.Fact.Freshness() == codegen.GCFreshUnpublished {
				reuse.Entries = append(reuse.Entries, Specialization{Instruction: uint32(semanticID), Certificate: value.Fact.Identity(), Result: value.Result, Kind: SpecializeFreshObject, GCFact: value.Fact})
			}
			gcIndex++
		}
	}
	for certificateID, certificate := range simplified.Bounds {
		reuse.Entries = append(reuse.Entries, Specialization{Instruction: certificate.Instruction, Certificate: uint32(certificateID) + 1, Reads: HeapLinearMemory, Kind: SpecializePreservedBounds})
	}
	if err := VerifySpecialization(f, semantic, metadata, simplified, inputs, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func dominantProfileTarget(observations *profile.Module, site profile.Site) (uint32, bool) {
	for _, histogram := range observations.CallTargets {
		if histogram.Site != site || len(histogram.Targets) == 0 {
			continue
		}
		total, bestCount, bestTarget := uint64(0), uint64(0), uint32(0)
		for _, target := range histogram.Targets {
			if target.Count > ^uint64(0)-total {
				total = ^uint64(0)
			} else {
				total += target.Count
			}
			if target.Count > bestCount {
				bestCount, bestTarget = target.Count, target.Function
			}
		}
		// total-total/10 is ceil(90% of total) without overflowing uint64.
		return bestTarget, total >= 10 && bestCount >= total-total/10
	}
	return 0, false
}

func VerifySpecialization(f *StackFunc, semantic *SemanticFunc, metadata *Metadata, simplified *SimplifyResult, inputs SpecializationInputs, plan *SpecializationPlan) error {
	if plan == nil {
		return fmt.Errorf("railssa: nil specialization plan")
	}
	if err := validateGCValueFacts(f, semantic, inputs.GCValues); err != nil {
		return err
	}
	for id, entry := range plan.Entries {
		if int(entry.Instruction) >= len(semantic.Insts) || entry.Kind == SpecializeInvalid {
			return fmt.Errorf("railssa: invalid specialization %d: %#v", id, entry)
		}
		instruction := semantic.Insts[entry.Instruction]
		source := f.Instrs[instruction.Source]
		switch entry.Kind {
		case SpecializeSameInstanceCall:
			if instruction.Op != wasm.InstrCall || entry.Target < f.ImportedFuncs || entry.Target != source.U32() {
				return fmt.Errorf("railssa: invalid same-instance specialization %d", id)
			}
		case SpecializeHostEffects:
			if instruction.Op != wasm.InstrCall || entry.Target >= f.ImportedFuncs || int(entry.Target) >= len(inputs.Host) || !inputs.Host[entry.Target].Declared || entry.Reads != inputs.Host[entry.Target].Reads || entry.Writes != inputs.Host[entry.Target].Writes || entry.Flags != inputs.Host[entry.Target].Flags|EffectCall {
				return fmt.Errorf("railssa: invalid host specialization %d", id)
			}
		case SpecializePreservedBounds:
			if entry.Certificate == 0 || int(entry.Certificate) > len(simplified.Bounds) || simplified.Bounds[entry.Certificate-1].Instruction != entry.Instruction {
				return fmt.Errorf("railssa: invalid bounds specialization %d", id)
			}
		case SpecializeIndirectTarget:
			if inputs.Observations == nil || instruction.Op != wasm.InstrCallIndirect || entry.Target < f.ImportedFuncs || entry.Target >= f.FuncCount {
				return fmt.Errorf("railssa: invalid indirect specialization %d", id)
			}
			target, ok := dominantProfileTarget(inputs.Observations, profile.Site{Function: inputs.FunctionIndex, Offset: source.Offset})
			if !ok || target != entry.Target {
				return fmt.Errorf("railssa: unproven indirect target specialization %d", id)
			}
		case SpecializeExactGCType:
			fact, ok := findGCValueFact(inputs.GCValues, entry.Instruction, entry.Result)
			typ, exact := entry.GCFact.ExactType()
			if !ok || fact.Fact != entry.GCFact || !exact || entry.Target != typ {
				return fmt.Errorf("railssa: unproven exact GC type specialization %d", id)
			}
		case SpecializeFreshObject:
			fact, ok := findGCValueFact(inputs.GCValues, entry.Instruction, entry.Result)
			if !ok || fact.Fact != entry.GCFact || entry.GCFact.Freshness() != codegen.GCFreshUnpublished || entry.GCFact.Identity() == 0 || entry.Certificate != entry.GCFact.Identity() {
				return fmt.Errorf("railssa: unproven fresh-object specialization %d", id)
			}
		}
	}
	return nil
}

func validateGCValueFacts(f *StackFunc, semantic *SemanticFunc, facts []GCValueFact) error {
	for index, value := range facts {
		if int(value.Instruction) >= len(semantic.Insts) || value.Fact.IsZero() {
			return fmt.Errorf("railssa: invalid GC value fact %d", index)
		}
		if index != 0 {
			previous := facts[index-1]
			if previous.Instruction > value.Instruction || previous.Instruction == value.Instruction && previous.Result >= value.Result {
				return fmt.Errorf("railssa: GC value facts are not strictly source ordered")
			}
		}
		instruction := semantic.Insts[value.Instruction]
		source := f.Instrs[instruction.Source]
		if uint32(value.Result) >= instruction.ResultCount() {
			return fmt.Errorf("railssa: GC value fact %d result %d is unavailable", index, value.Result)
		}
		typ, ok := f.InstructionResultType(instruction.Source, source, uint32(value.Result))
		if !ok || typ.Kind() != wasm.ValRef {
			return fmt.Errorf("railssa: GC value fact %d does not describe a reference result", index)
		}
		if value.Fact.Freshness() == codegen.GCFreshUnpublished && (value.Fact.Identity() == 0 || value.Fact.Nullability() != codegen.GCKnownNonNull || value.Fact.HeapClass() != codegen.GCHeapStruct && value.Fact.HeapClass() != codegen.GCHeapArray) {
			return fmt.Errorf("railssa: GC value fact %d has invalid fresh-object proof", index)
		}
	}
	return nil
}

func findGCValueFact(facts []GCValueFact, instruction uint32, result uint16) (GCValueFact, bool) {
	for _, fact := range facts {
		if fact.Instruction == instruction && fact.Result == result {
			return fact, true
		}
		if fact.Instruction > instruction || fact.Instruction == instruction && fact.Result > result {
			break
		}
	}
	return GCValueFact{}, false
}
