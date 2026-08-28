package railssa

import (
	"fmt"
	"math"
	"math/bits"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// EmissionPlan is the verified semantic information consumed by both native
// emitters. Source-indexed facts keep the emitter seam independent of the
// planner's internal CFG and SSA storage.
type EmissionPlan struct {
	boundsChecksElided []bool
	SemanticInsts      uint32
	SemanticArgs       uint32
	ProofQueries       uint32
}

func (p *EmissionPlan) ElidesBoundsCheck(source uint32) bool {
	return p != nil && int(source) < len(p.boundsChecksElided) && p.boundsChecksElided[source]
}

func (p *EmissionPlan) ElidedBoundsChecks() uint32 {
	if p == nil {
		return 0
	}
	var count uint32
	for _, elided := range p.boundsChecksElided {
		if elided {
			count++
		}
	}
	return count
}

// EmissionPlanner owns reusable one-function scratch for semantic planning.
// A returned plan remains valid until the next call to Plan.
type EmissionPlanner struct {
	cfg          CFG
	locals       LocalSSA
	flow         ValueFlow
	semantic     SemanticFunc
	metadata     Metadata
	simplified   SimplifyResult
	plan         EmissionPlan
	addressStack []uint32
	verifyBounds []bool
	localMaximum []uint64
	localProven  []bool
}

// CapacityBytes reports retained one-function planner scratch. It is used by
// opt-in compiler metrics and deliberately includes reusable headroom.
func (p *EmissionPlanner) CapacityBytes() uint64 {
	if p == nil {
		return 0
	}
	return capacityBytes(p.cfg.Blocks) + capacityBytes(p.cfg.Edges) + capacityBytes(p.cfg.EdgeStacks) + capacityBytes(p.cfg.Refinements) + capacityBytes(p.cfg.Preds) + capacityBytes(p.cfg.Succs) + capacityBytes(p.cfg.leaders) + capacityBytes(p.cfg.regionAtStart) + capacityBytes(p.cfg.regionAtElse) + capacityBytes(p.cfg.regionAtEnd) + capacityBytes(p.cfg.raw) + capacityBytes(p.cfg.active) + capacityBytes(p.cfg.starts) + capacityBytes(p.cfg.instrBlock) + capacityBytes(p.cfg.planned) +
		capacityBytes(p.locals.Definitions) + capacityBytes(p.locals.Params) + capacityBytes(p.locals.EdgeArgs) + capacityBytes(p.locals.EntryValues) + capacityBytes(p.locals.ExitValues) + capacityBytes(p.locals.InstructionValues) + capacityBytes(p.locals.Reachable) + capacityBytes(p.locals.LiveIn) + capacityBytes(p.locals.work) + capacityBytes(p.locals.ready) + capacityBytes(p.locals.before) + capacityBytes(p.locals.liveScratch) +
		capacityBytes(p.flow.Values) + capacityBytes(p.flow.Params) + capacityBytes(p.flow.EdgeArgs) + capacityBytes(p.flow.EntryStacks) + capacityBytes(p.flow.ExitStacks) + capacityBytes(p.flow.EntryDepths) + capacityBytes(p.flow.ExitDepths) + capacityBytes(p.flow.InstructionValues) + capacityBytes(p.flow.LocalDefinitionValues) + capacityBytes(p.flow.Reachable) + capacityBytes(p.flow.phi) + capacityBytes(p.flow.ready) + capacityBytes(p.flow.before) + capacityBytes(p.flow.merge) + capacityBytes(p.flow.stack) +
		capacityBytes(p.semantic.Insts) + capacityBytes(p.semantic.Args) + capacityBytes(p.semantic.Blocks) + capacityBytes(p.semantic.InstructionMap) + capacityBytes(p.semantic.stack) +
		capacityBytes(p.metadata.Instructions) +
		capacityBytes(p.simplified.Aliases) + capacityBytes(p.simplified.Facts) + capacityBytes(p.simplified.Reachable) + capacityBytes(p.simplified.LiveInsts) + capacityBytes(p.simplified.Branches) + capacityBytes(p.simplified.Remaining) + capacityBytes(p.simplified.UseOffsets) + capacityBytes(p.simplified.Uses) + capacityBytes(p.simplified.Bounds) + capacityBytes(p.simplified.liveValues) + capacityBytes(p.simplified.instructionBlock) + capacityBytes(p.simplified.liveWork) + capacityBytes(p.simplified.gvnKeys) + capacityBytes(p.simplified.gvnValues) +
		capacityBytes(p.plan.boundsChecksElided) + capacityBytes(p.addressStack) + capacityBytes(p.verifyBounds) + capacityBytes(p.localMaximum) + capacityBytes(p.localProven)
}

func capacityBytes[T any](values []T) uint64 {
	var value T
	return uint64(cap(values)) * uint64(unsafe.Sizeof(value))
}

// NeedsEmissionPlan reports whether the current production emitter can consume
// semantic facts for f. The first production consumer is a directly constant
// load or a directly masked address; other memory shapes retain their checks
// until their selected address forms can carry certificates. Keeping this test exact and cheap avoids
// constructing SSA when no implemented emission decision can change code.
func NeedsEmissionPlan(f *StackFunc) bool {
	if f == nil {
		return false
	}
	hasMemory, hasIntegerAnd := false, false
	for index, instruction := range f.Instrs {
		if instruction.Kind == wasm.InstrI32And || instruction.Kind == wasm.InstrI64And {
			hasIntegerAnd = true
		}
		if memoryAccessBytes(instruction.Kind) != 0 {
			hasMemory = true
		}
		if index != 0 && instruction.Kind >= wasm.InstrI32Load && instruction.Kind <= wasm.InstrI64Load32U && f.Instrs[index-1].Kind == wasm.InstrI32Const {
			return true
		}
		if index >= 2 && instruction.Kind >= wasm.InstrI32Store && instruction.Kind <= wasm.InstrI64Store32 && f.Instrs[index-2].Kind == wasm.InstrI32Const && directEmissionValue(f.Instrs[index-1].Kind) {
			return true
		}
		if index != 0 && instruction.Kind >= wasm.InstrI32Load && instruction.Kind <= wasm.InstrI64Load32U && (f.Instrs[index-1].Kind == wasm.InstrI32And || f.Instrs[index-1].Kind == wasm.InstrI64And) {
			return true
		}
		if index >= 2 && instruction.Kind >= wasm.InstrI32Store && instruction.Kind <= wasm.InstrI64Store32 && (f.Instrs[index-2].Kind == wasm.InstrI32And || f.Instrs[index-2].Kind == wasm.InstrI64And) && directEmissionValue(f.Instrs[index-1].Kind) {
			return true
		}
	}
	return hasMemory && hasIntegerAnd
}

func directEmissionValue(kind wasm.InstrKind) bool {
	return kind == wasm.InstrI32Const || kind == wasm.InstrI64Const || kind == wasm.InstrF32Const || kind == wasm.InstrF64Const ||
		kind == wasm.InstrLocalGet || kind == wasm.InstrGlobalGet || kind == wasm.InstrMemorySize
}

func (p *EmissionPlanner) Plan(f *StackFunc) (*EmissionPlan, error) {
	if f == nil {
		return nil, fmt.Errorf("railssa: emission planning requires a function")
	}
	if f.MaxLoopDepth != 0 {
		return p.planStructuredLoopBounds(f)
	}
	cfg, err := BuildCFG(f, &p.cfg)
	if err != nil {
		return nil, err
	}
	locals, err := BuildLocalSSA(f, cfg, &p.locals)
	if err != nil {
		return nil, err
	}
	flow, err := BuildValueFlow(f, cfg, locals, &p.flow)
	if err != nil {
		return nil, err
	}
	semantic, err := BuildSemanticFunc(f, cfg, flow, &p.semantic)
	if err != nil {
		return nil, err
	}
	metadata, err := BuildMetadata(f, &p.metadata)
	if err != nil {
		return nil, err
	}
	simplified, err := SparseSimplify(f, cfg, flow, semantic, metadata, DefaultSimplifyConfig(), &p.simplified)
	if err != nil {
		return nil, err
	}
	plan, err := BuildEmissionPlan(f, flow, semantic, metadata, simplified, &p.plan)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// planStructuredLoopBounds proves source-local masked induction accesses using
// the compact StackFunc. Loop fallback emission therefore avoids constructing
// a second CFG/SSA product solely for check decisions.
func (p *EmissionPlanner) planStructuredLoopBounds(f *StackFunc) (*EmissionPlan, error) {
	bounds := resizeClear(p.plan.boundsChecksElided, len(f.Instrs))
	p.plan = EmissionPlan{boundsChecksElided: bounds}
	if err := p.deriveStructuredLoopBounds(f, p.plan.boundsChecksElided); err != nil {
		return nil, err
	}
	if err := p.verifyStructuredLoopBounds(f, &p.plan); err != nil {
		return nil, err
	}
	return &p.plan, nil
}

func (p *EmissionPlanner) verifyStructuredLoopBounds(f *StackFunc, plan *EmissionPlan) error {
	if plan == nil || len(plan.boundsChecksElided) != len(f.Instrs) {
		return fmt.Errorf("railssa: malformed structured loop emission plan")
	}
	p.verifyBounds = resizeClear(p.verifyBounds, len(f.Instrs))
	if err := p.deriveStructuredLoopBounds(f, p.verifyBounds); err != nil {
		return err
	}
	for source := range plan.boundsChecksElided {
		if plan.boundsChecksElided[source] != p.verifyBounds[source] {
			return fmt.Errorf("railssa: source instruction %d structured loop bounds decision disagrees", source)
		}
	}
	return nil
}

func (p *EmissionPlanner) deriveStructuredLoopBounds(f *StackFunc, bounds []bool) error {
	p.localMaximum = resizeClear(p.localMaximum, len(f.Locals))
	p.localProven = resizeClear(p.localProven, len(f.Locals))
	for regionID, region := range f.Regions {
		if region.Kind != wasm.InstrLoop {
			continue
		}
		nested := false
		for _, child := range f.Regions {
			if child.Parent == RegionID(regionID) {
				nested = true
				break
			}
		}
		if nested {
			continue
		}
		clear(p.localProven)
		for _, local := range region.LoopLocals(f) {
			maximum, ok := proveStructuredMaskedLocalInRegion(f, RegionID(regionID), local)
			if ok {
				p.localMaximum[local], p.localProven[local] = maximum, true
			}
		}
		if err := p.scanStructuredLoopAccesses(f, RegionID(regionID), func(source, local uint32) {
			if int(local) >= len(p.localProven) || !p.localProven[local] {
				return
			}
			instruction := f.Instrs[source]
			maximum, offset, width := p.localMaximum[local], uint64(instruction.U32()), memoryAccessBytes(instruction.Kind)
			end := maximum + offset + width
			if width != 0 && end >= maximum && end >= offset && end <= f.MemoryMinBytes {
				bounds[source] = true
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *EmissionPlanner) scanStructuredLoopAccesses(f *StackFunc, region RegionID, visit func(source, local uint32)) error {
	depth := int(f.Regions[region].StackDepth)
	if cap(p.addressStack) < int(f.MaxStack) {
		p.addressStack = make([]uint32, int(f.MaxStack))
	} else {
		p.addressStack = p.addressStack[:int(f.MaxStack)]
		clear(p.addressStack)
	}
	stack := p.addressStack[:depth]
	push := func(value uint32) error {
		if len(stack) == cap(stack) {
			return fmt.Errorf("railssa: structured address replay exceeds max stack")
		}
		stack = stack[:len(stack)+1]
		stack[len(stack)-1] = value
		return nil
	}
	pop := func(count int) error {
		if count > len(stack) {
			return fmt.Errorf("railssa: structured address replay underflow")
		}
		stack = stack[:len(stack)-count]
		return nil
	}
	for index := f.Regions[region].StartInstr + 1; index < f.Regions[region].EndInstr; index++ {
		instruction := f.Instrs[index]
		switch instruction.Kind {
		case wasm.InstrInvalid, wasm.InstrNop, wasm.InstrBr, wasm.InstrUnreachable:
			continue
		case wasm.InstrLocalGet:
			if err := push(instruction.U32() + 1); err != nil {
				return err
			}
			continue
		case wasm.InstrLocalSet, wasm.InstrDrop:
			if err := pop(1); err != nil {
				return fmt.Errorf("railssa: structured address replay instruction %d %s: %w", index, instruction.Kind, err)
			}
			continue
		case wasm.InstrLocalTee:
			continue
		case wasm.InstrBlock, wasm.InstrLoop, wasm.InstrIf:
			return nil
		}
		operands, emit, err := semanticOperandCount(f, index, instruction)
		if err != nil || !emit {
			return err
		}
		if memoryAccessBytes(instruction.Kind) != 0 && operands != 0 && operands <= len(stack) {
			address := stack[len(stack)-operands]
			if address != 0 {
				visit(index, address-1)
			}
		}
		if err := pop(operands); err != nil {
			return fmt.Errorf("railssa: structured address replay instruction %d %s operands=%d depth=%d: %w", index, instruction.Kind, operands, len(stack), err)
		}
		if stackInstructionHasValueResult(instruction) {
			if err := push(0); err != nil {
				return err
			}
		}
	}
	return nil
}

func stackInstructionHasValueResult(instruction StackInstr) bool {
	kind := instruction.Kind
	switch {
	case kind == wasm.InstrCall || kind == wasm.InstrCallIndirect:
		return instruction.HasResult()
	case kind == wasm.InstrGlobalGet || kind == wasm.InstrMemorySize || kind == wasm.InstrMemoryGrow ||
		kind == wasm.InstrI32Const || kind == wasm.InstrI64Const || kind == wasm.InstrF32Const || kind == wasm.InstrF64Const ||
		kind == wasm.InstrRefNull || kind == wasm.InstrRefFunc || kind == wasm.InstrRefIsNull || kind == wasm.InstrRefEq || kind == wasm.InstrRefAsNonNull || kind == wasm.InstrRefTest || kind == wasm.InstrRefCast ||
		kind == wasm.InstrAnyConvertExtern || kind == wasm.InstrExternConvertAny ||
		kind == wasm.InstrRefI31 || kind == wasm.InstrI31GetS || kind == wasm.InstrI31GetU ||
		kind == wasm.InstrStructNew || kind == wasm.InstrStructNewDefault ||
		kind == wasm.InstrStructGet || kind == wasm.InstrStructGetS || kind == wasm.InstrStructGetU ||
		kind == wasm.InstrArrayNew || kind == wasm.InstrArrayNewDefault || kind == wasm.InstrArrayNewFixed || kind == wasm.InstrArrayNewData || kind == wasm.InstrArrayNewElem || kind == wasm.InstrArrayGet || kind == wasm.InstrArrayGetS || kind == wasm.InstrArrayGetU || kind == wasm.InstrArrayLen ||
		kind == wasm.InstrSelect || scalarBinaryKind(kind) || scalarComparisonKind(kind) ||
		kind == wasm.InstrI32Eqz || kind == wasm.InstrI64Eqz ||
		kind >= wasm.InstrI32Clz && kind <= wasm.InstrI32Popcnt ||
		kind >= wasm.InstrI64Clz && kind <= wasm.InstrI64Popcnt ||
		kind >= wasm.InstrI32Extend8S && kind <= wasm.InstrI64Extend32S ||
		kind >= wasm.InstrF32Abs && kind <= wasm.InstrF32Sqrt ||
		kind >= wasm.InstrF64Abs && kind <= wasm.InstrF64Sqrt ||
		kind >= wasm.InstrI32Load && kind <= wasm.InstrI64Load32U:
		return true
	default:
		_, _, ok := conversionTypes(kind)
		return ok
	}
}

func proveStructuredMaskedLocalInRegion(f *StackFunc, region RegionID, local uint32) (uint64, bool) {
	if int(local) < len(f.Params) || int(local) >= len(f.Locals) || f.Locals[local] != wasm.I32 {
		return 0, false
	}
	if int(region) >= len(f.Regions) || f.Regions[region].Kind != wasm.InstrLoop {
		return 0, false
	}
	alignment, maximum, updates := 32, uint32(0), 0
	loop := f.Regions[region]
	for index := uint32(0); index <= loop.StartInstr; index++ {
		instruction := f.Instrs[index]
		if (instruction.Kind == wasm.InstrLocalSet || instruction.Kind == wasm.InstrLocalTee) && instruction.U32() == local {
			return 0, false
		}
	}
	for index := loop.StartInstr + 1; index < loop.EndInstr; index++ {
		instruction := f.Instrs[index]
		if (instruction.Kind != wasm.InstrLocalSet && instruction.Kind != wasm.InstrLocalTee) || instruction.U32() != local {
			continue
		}
		if instruction.Kind != wasm.InstrLocalSet || index < loop.StartInstr+6 {
			return 0, false
		}
		sequence := f.Instrs[index-5 : index+1]
		if sequence[0].Kind != wasm.InstrLocalGet || sequence[0].U32() != local || sequence[1].Kind != wasm.InstrI32Const ||
			(sequence[2].Kind != wasm.InstrI32Add && sequence[2].Kind != wasm.InstrI32Sub) || sequence[3].Kind != wasm.InstrI32Const || sequence[4].Kind != wasm.InstrI32And {
			return 0, false
		}
		step, mask := sequence[1].U32(), sequence[3].U32()
		alignment = min(alignment, bits.TrailingZeros32(step))
		maximum = max(maximum, mask)
		updates++
	}
	if updates == 0 || alignment == 0 {
		return 0, false
	}
	lowZero := uint32(math.MaxUint32)
	if alignment < 32 {
		lowZero = uint32(1)<<alignment - 1
	}
	return uint64(maximum &^ lowZero), true
}

// BuildEmissionPlan publishes source-indexed decisions from an already-built
// verified RailSSA product. Native compilation uses this seam to avoid building
// a duplicate CFG/value graph merely to feed the established fallback emitter.
func BuildEmissionPlan(f *StackFunc, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, simplified *SimplifyResult, reuse *EmissionPlan) (*EmissionPlan, error) {
	if f == nil || flow == nil || semantic == nil || metadata == nil || simplified == nil {
		return nil, fmt.Errorf("railssa: emission planning requires verified semantic facts")
	}
	if reuse == nil {
		reuse = new(EmissionPlan)
	}
	bounds := resizeClear(reuse.boundsChecksElided, len(f.Instrs))
	*reuse = EmissionPlan{boundsChecksElided: bounds, SemanticInsts: uint32(len(semantic.Insts)), SemanticArgs: uint32(len(semantic.Args))}
	// Every certificate is queried exactly once while publishing the plan. Avoid
	// retaining a cache that cannot have a hit in this single forward pass.
	proofs := ProofEngine{Flow: flow, Semantic: semantic, Metadata: metadata, Facts: simplified}
	for _, certificate := range simplified.Bounds {
		if int(certificate.Instruction) >= len(semantic.Insts) {
			return nil, fmt.Errorf("railssa: bounds certificate has invalid semantic instruction %d", certificate.Instruction)
		}
		proof, err := proofs.demandProofOnce(ProofRequest{Kind: ProofBounds, Value: certificate.Address, Aux: certificate.Instruction, Fuel: ^uint16(0)})
		reuse.ProofQueries++
		if err != nil {
			return nil, fmt.Errorf("railssa: demand bounds proof for semantic instruction %d: %w", certificate.Instruction, err)
		}
		if !proof.Proven {
			return nil, fmt.Errorf("railssa: bounds certificate for semantic instruction %d failed demand proof", certificate.Instruction)
		}
		source := semantic.Insts[certificate.Instruction].Source
		reuse.boundsChecksElided[source] = true
	}
	if err := VerifyEmissionPlan(f, semantic, metadata, simplified, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func VerifyEmissionPlan(f *StackFunc, semantic *SemanticFunc, metadata *Metadata, simplified *SimplifyResult, plan *EmissionPlan) error {
	if f == nil || semantic == nil || metadata == nil || simplified == nil || plan == nil || len(plan.boundsChecksElided) != len(f.Instrs) {
		return fmt.Errorf("railssa: malformed emission plan")
	}
	if plan.SemanticInsts != uint32(len(semantic.Insts)) || plan.SemanticArgs != uint32(len(semantic.Args)) {
		return fmt.Errorf("railssa: emission plan semantic counts disagree")
	}
	for source, elided := range plan.boundsChecksElided {
		if !elided {
			continue
		}
		semanticMap := semantic.InstructionMap[source]
		if semanticMap == 0 {
			return fmt.Errorf("railssa: source instruction %d has no semantic bounds operation", source)
		}
		semanticID := semanticMap - 1
		if memoryAccessBytes(semantic.Insts[semanticID].Op) == 0 || metadata.Instructions[source].Obligations&ObligationMemoryBounds == 0 || simplified.Remaining[semanticID]&ObligationMemoryBounds != 0 {
			return fmt.Errorf("railssa: source instruction %d has unproven bounds elision", source)
		}
		found := false
		for _, certificate := range simplified.Bounds {
			if certificate.Instruction == semanticID {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("railssa: source instruction %d lacks a bounds certificate", source)
		}
	}
	return nil
}
