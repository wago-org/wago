package railssa

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type IntegerFact struct {
	KnownZero   uint64
	KnownOne    uint64
	Min         uint64
	Max         uint64
	Known       bool
	RangeKnown  bool
	Width       uint8
	SignExtBits uint8
	_           [4]byte
}

type BranchDecision uint8

const (
	BranchUnknown BranchDecision = iota
	BranchFalse
	BranchTrue
)

type BoundsCertificate struct {
	Instruction uint32
	Address     FlowValueID
	MemoryBytes uint64
	End         uint64
}

type SimplifyMetrics struct {
	Aliases            uint32
	TrivialArguments   uint32
	CrossBlockAliases  uint32
	Constants          uint32
	DeadInstructions   uint32
	DeadBlocks         uint32
	BranchesSimplified uint32
	ObligationsRemoved uint32
	BoundsCertificates uint32
	FuelUsed           uint32
}

type SimplifyResult struct {
	Aliases []FlowValueID
	// Facts is dense for integer-heavy functions and otherwise contains only
	// integer-typed values. Use IntegerFactAt rather than indexing it by value.
	Facts      []IntegerFact
	Reachable  []bool
	LiveInsts  []bool
	Branches   []BranchDecision
	Remaining  []ObligationMask
	UseOffsets []uint32
	Uses       []uint32
	Bounds     []BoundsCertificate
	Metrics    SimplifyMetrics

	liveValues       []bool
	instructionBlock []BlockID
	liveWork         []FlowValueID
	gvnKeys          []uint64
	gvnValues        []FlowValueID
	factIndex        []uint32
}

// IntegerFactAt returns the inferred integer fact for value. Non-integer and
// out-of-range values have the zero fact.
func (r *SimplifyResult) IntegerFactAt(value FlowValueID) IntegerFact {
	if r == nil || int(value) >= len(r.Aliases) {
		return IntegerFact{}
	}
	if len(r.factIndex) == 0 {
		return r.Facts[value]
	}
	slot := r.factIndex[value]
	if slot == 0 {
		return IntegerFact{}
	}
	return r.Facts[slot-1]
}

// HasIntegerFactDomain reports whether the result can answer fact queries for
// every value in a domain of the given size.
func (r *SimplifyResult) HasIntegerFactDomain(values int) bool {
	return r != nil && (len(r.factIndex) == 0 && len(r.Facts) == values || len(r.factIndex) == values)
}

func (r *SimplifyResult) integerFactPtr(value FlowValueID) *IntegerFact {
	if len(r.factIndex) == 0 {
		return &r.Facts[value]
	}
	slot := r.factIndex[value]
	if slot == 0 {
		panic("railssa: requested integer fact for non-integer value")
	}
	return &r.Facts[slot-1]
}

type SimplifyConfig struct {
	RewriteFuel uint32
	UseBudget   uint32
}

func DefaultSimplifyConfig() SimplifyConfig {
	return SimplifyConfig{RewriteFuel: 4096, UseBudget: 1 << 20}
}

// SparseSimplify computes a bounded semantic overlay without mutating the
// verified source graph. Aliases, facts, reachability, liveness, use CSR, and
// discharged obligations can therefore be checked independently before a
// later compacting rewrite commits them.
func SparseSimplify(f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, config SimplifyConfig, reuse *SimplifyResult) (*SimplifyResult, error) {
	if err := VerifySemanticFunc(f, cfg, flow, semantic); err != nil {
		return nil, err
	}
	if err := VerifyMetadata(f, metadata); err != nil {
		return nil, err
	}
	if config.RewriteFuel == 0 || config.UseBudget == 0 {
		return nil, fmt.Errorf("railssa: SparseSimplify requires nonzero budgets")
	}
	if reuse == nil {
		reuse = new(SimplifyResult)
	}
	aliases := resizeClear(reuse.Aliases, len(flow.Values))
	integerValues := 0
	for _, value := range flow.Values {
		if value.Type == wasm.I32 || value.Type == wasm.I64 {
			integerValues++
		}
	}
	// The sparse index costs four bytes per value. Require at least a 40% direct
	// saving before selecting it; this also avoids retaining a second index slab
	// in mixed modules whose other functions need dense integer facts.
	facts := reuse.Facts[:0]
	factIndex := reuse.factIndex[:0]
	if integerValues*2 >= len(flow.Values) {
		facts = resizeClear(facts, len(flow.Values))
	} else {
		facts = resizeClear(facts, integerValues)
		factIndex = resizeClear(factIndex, len(flow.Values))
		slot := uint32(1)
		for value, record := range flow.Values {
			if record.Type == wasm.I32 || record.Type == wasm.I64 {
				factIndex[value] = slot
				slot++
			}
		}
	}
	reachable := resizeClear(reuse.Reachable, len(cfg.Blocks))
	live := resizeClear(reuse.LiveInsts, len(semantic.Insts))
	branches := resizeClear(reuse.Branches, len(semantic.Insts))
	remaining := resizeClear(reuse.Remaining, len(semantic.Insts))
	useOffsets := resizeClear(reuse.UseOffsets, len(flow.Values)+1)
	uses := reuse.Uses[:0]
	bounds := reuse.Bounds[:0]
	liveValues := resizeClear(reuse.liveValues, len(flow.Values))
	instructionBlock := resizeClear(reuse.instructionBlock, len(semantic.Insts))
	liveWork := reuse.liveWork[:0]
	gvnKeys := reuse.gvnKeys[:0]
	gvnValues := reuse.gvnValues[:0]
	*reuse = SimplifyResult{Aliases: aliases, Facts: facts, Reachable: reachable, LiveInsts: live, Branches: branches, Remaining: remaining, UseOffsets: useOffsets, Uses: uses, Bounds: bounds, liveValues: liveValues, instructionBlock: instructionBlock, liveWork: liveWork, gvnKeys: gvnKeys, gvnValues: gvnValues, factIndex: factIndex}
	for id := range reuse.Aliases {
		reuse.Aliases[id] = FlowValueID(id)
	}
	for id, value := range flow.Values {
		if id == 0 {
			continue
		}
		if value.Kind == FlowValueInitialLocal && int(value.Local) >= len(f.Params) && (value.Type == wasm.I32 || value.Type == wasm.I64) {
			setIntegerConstant(reuse.integerFactPtr(FlowValueID(id)), 0, value.Type)
		}
	}

	fuel := config.RewriteFuel
	consume := func() bool {
		if fuel == 0 {
			return false
		}
		fuel--
		reuse.Metrics.FuelUsed++
		return true
	}
	// Remove block parameters whose non-self incoming values all agree. A
	// bounded fixed point also handles chains of trivial local/stack arguments.
	for changed := true; changed && fuel != 0; {
		changed = false
		for paramID, value := range flow.Values {
			if value.Kind != FlowValueBlockParam || reuse.Aliases[paramID] != FlowValueID(paramID) {
				continue
			}
			incoming := FlowValueID(0)
			valid := true
			for _, edge := range flow.EdgeArgs {
				if edge.Param != FlowValueID(paramID) {
					continue
				}
				argument := resolveAlias(reuse.Aliases, edge.Argument)
				if argument == FlowValueID(paramID) {
					continue
				}
				if flow.Values[argument].Type != value.Type {
					valid = false
					break
				}
				if incoming == 0 {
					incoming = argument
				} else if incoming != argument {
					valid = false
					break
				}
			}
			if valid && incoming != 0 && consume() {
				reuse.Aliases[paramID] = incoming
				reuse.Metrics.Aliases++
				reuse.Metrics.TrivialArguments++
				changed = true
			}
		}
	}

	// Bounded sparse propagation. Values are source ordered; repeated passes are
	// needed only for block-argument aliases and loop facts.
	for pass := 0; pass < len(cfg.Blocks)+1 && fuel != 0; pass++ {
		changed := false
		for semanticID, instruction := range semantic.Insts {
			result := instruction.Result
			if result == 0 || reuse.IntegerFactAt(result).Known {
				continue
			}
			args := semantic.Operands(uint32(semanticID))
			resolved := args
			if len(args) != 0 {
				// At most three scalar operands outside calls; calls are effectful and
				// never enter constant evaluation.
				var local [3]FlowValueID
				if len(args) <= len(local) {
					resolved = local[:len(args)]
					for index, argument := range args {
						resolved[index] = resolveAlias(reuse.Aliases, argument)
					}
				}
			}
			known := true
			for _, argument := range resolved {
				if !reuse.IntegerFactAt(argument).Known {
					known = false
					break
				}
			}
			if known {
				value, err := evalIntegerInstructionFromFacts(instruction, resolved, nil, reuse)
				if err == nil && consume() {
					setIntegerConstant(reuse.integerFactPtr(result), value, flow.Values[result].Type)
					reuse.Metrics.Constants++
					changed = true
				}
				continue
			}
			if fact, ok := inferIntegerFact(instruction.Op, resolved, reuse, flow.Values[result].Type); ok && fact != reuse.IntegerFactAt(result) && consume() {
				*reuse.integerFactPtr(result) = fact
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for value := FlowValueID(1); int(value) < len(flow.Values) && fuel != 0; value++ {
		fact, ok := maskedInductionFact(f, flow, semantic, reuse.Aliases, value)
		if !ok || fact == reuse.IntegerFactAt(value) || !consume() {
			continue
		}
		*reuse.integerFactPtr(value) = fact
	}
	// Re-run the cheap nonconstant transfer functions after loop facts become
	// available. This is a bounded descending refinement: masked induction
	// alignment flows through affine updates and back into masked addresses.
	for pass := 0; pass < len(cfg.Blocks)+1 && fuel != 0; pass++ {
		changed := false
		for semanticID, instruction := range semantic.Insts {
			result := instruction.Result
			if result == 0 || reuse.IntegerFactAt(result).Known {
				continue
			}
			args := semantic.Operands(uint32(semanticID))
			var local [3]FlowValueID
			resolved := args
			if len(args) <= len(local) {
				resolved = local[:len(args)]
				for index, argument := range args {
					resolved[index] = resolveAlias(reuse.Aliases, argument)
				}
			}
			fact, ok := inferIntegerFact(instruction.Op, resolved, reuse, flow.Values[result].Type)
			if ok && fact != reuse.IntegerFactAt(result) && consume() {
				*reuse.integerFactPtr(result) = fact
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	if err := localPureGVN(cfg, flow, semantic, metadata, reuse, consume); err != nil {
		return nil, err
	}

	for semanticID, instruction := range semantic.Insts {
		meta := metadata.Instructions[instruction.Source]
		reuse.Remaining[semanticID] = meta.Obligations
		args := semantic.Operands(uint32(semanticID))
		if instruction.Op == wasm.InstrIf || instruction.Op == wasm.InstrBrIf {
			condition := resolveAlias(reuse.Aliases, args[0])
			if fact := reuse.IntegerFactAt(condition); fact.Known {
				if fact.Min == 0 {
					reuse.Branches[semanticID] = BranchFalse
				} else {
					reuse.Branches[semanticID] = BranchTrue
				}
				reuse.Metrics.BranchesSimplified++
			}
		}
		if len(args) != 0 && meta.Obligations&ObligationMemoryBounds != 0 {
			address := resolveAlias(reuse.Aliases, args[0])
			if fact := reuse.IntegerFactAt(address); fact.RangeKnown {
				width := memoryAccessBytes(instruction.Op)
				offset := uint64(uint32(instruction.Aux))
				end := fact.Max + offset + width
				if width != 0 && end >= fact.Max && end >= offset && end <= f.MemoryMinBytes {
					reuse.Bounds = append(reuse.Bounds, BoundsCertificate{Instruction: uint32(semanticID), Address: address, MemoryBytes: f.MemoryMinBytes, End: end})
					reuse.Remaining[semanticID] &^= ObligationMemoryBounds
					reuse.Metrics.BoundsCertificates++
					reuse.Metrics.ObligationsRemoved++
				}
			}
		}
		if len(args) >= 2 && meta.Obligations&ObligationNonzeroDivisor != 0 {
			divisor := resolveAlias(reuse.Aliases, args[1])
			if fact := reuse.IntegerFactAt(divisor); fact.Known && fact.Min != 0 {
				reuse.Remaining[semanticID] &^= ObligationNonzeroDivisor
				reuse.Metrics.ObligationsRemoved++
			}
		}
		if _, proven := finiteConversionOrigin(flow, semantic, reuse, uint32(semanticID)); meta.Obligations&ObligationFiniteConversion != 0 && proven {
			reuse.Remaining[semanticID] &^= ObligationFiniteConversion
			reuse.Metrics.ObligationsRemoved++
		}
		if alias, ok := integerFloatRoundTripAlias(flow, semantic, reuse, uint32(semanticID)); ok && instruction.Result != 0 && reuse.Aliases[instruction.Result] == instruction.Result && consume() {
			reuse.Aliases[instruction.Result] = alias
			reuse.Metrics.Aliases++
		}
		if alias, ok := redundantSignExtensionAlias(flow, semantic, reuse, uint32(semanticID)); ok && instruction.Result != 0 && reuse.Aliases[instruction.Result] == instruction.Result && consume() {
			reuse.Aliases[instruction.Result] = alias
			reuse.Metrics.Aliases++
		}
	}

	computeSimplifiedReachability(cfg, semantic, reuse)
	if err := buildUseCSR(flow, semantic, reuse, config.UseBudget); err != nil {
		return nil, err
	}
	markLiveSemantic(f, cfg, flow, semantic, metadata, reuse)
	for block, wasReachable := range flow.Reachable {
		if wasReachable && !reuse.Reachable[block] {
			reuse.Metrics.DeadBlocks++
		}
	}
	for _, isLive := range reuse.LiveInsts {
		if !isLive {
			reuse.Metrics.DeadInstructions++
		}
	}
	if err := VerifySimplify(f, cfg, flow, semantic, metadata, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func finiteConversionProven(flow *ValueFlow, semantic *SemanticFunc, result *SimplifyResult, instructionID uint32) bool {
	_, ok := finiteConversionOrigin(flow, semantic, result, instructionID)
	return ok
}

func finiteConversionOrigin(flow *ValueFlow, semantic *SemanticFunc, result *SimplifyResult, instructionID uint32) (FlowValueID, bool) {
	if int(instructionID) >= len(semantic.Insts) {
		return 0, false
	}
	instruction := semantic.Insts[instructionID]
	args := semantic.Operands(instructionID)
	if len(args) != 1 {
		return 0, false
	}
	targetMax := uint64(math.MaxUint64)
	switch instruction.Op {
	case wasm.InstrI32TruncF32S, wasm.InstrI32TruncF64S:
		targetMax = math.MaxInt32
	case wasm.InstrI32TruncF32U, wasm.InstrI32TruncF64U:
		targetMax = math.MaxUint32
	case wasm.InstrI64TruncF32S, wasm.InstrI64TruncF64S:
		targetMax = math.MaxInt64
	case wasm.InstrI64TruncF32U, wasm.InstrI64TruncF64U:
	default:
		return 0, false
	}
	origin, exactMax, ok := exactIntegerFloatOrigin(flow, semantic, result.Aliases, args[0], 0)
	if !ok || int(origin) >= len(result.Aliases) {
		return 0, false
	}
	fact := result.IntegerFactAt(origin)
	return origin, fact.RangeKnown && fact.Max <= exactMax && fact.Max <= targetMax
}

func integerFloatRoundTripAlias(flow *ValueFlow, semantic *SemanticFunc, result *SimplifyResult, instructionID uint32) (FlowValueID, bool) {
	if int(instructionID) >= len(semantic.Insts) {
		return 0, false
	}
	instruction := semantic.Insts[instructionID]
	// Every i32 is represented exactly in f64. Matching signed and unsigned
	// saturating round trips therefore reproduce the original i32 bit pattern;
	// unlike f32, no range proof is required.
	if instruction.Op == wasm.InstrI32TruncSatF64S || instruction.Op == wasm.InstrI32TruncSatF64U {
		args := semantic.Operands(instructionID)
		if len(args) == 1 {
			converted := resolveAlias(result.Aliases, args[0])
			definition, definitionArgs, defined := definingSemantic(flow, semantic, converted)
			matching := defined && len(definitionArgs) == 1 &&
				(instruction.Op == wasm.InstrI32TruncSatF64S && definition.Op == wasm.InstrF64ConvertI32S ||
					instruction.Op == wasm.InstrI32TruncSatF64U && definition.Op == wasm.InstrF64ConvertI32U)
			if matching {
				origin := resolveAlias(result.Aliases, definitionArgs[0])
				if int(origin) < len(flow.Values) && flow.Values[origin].Type == wasm.I32 {
					return origin, true
				}
			}
		}
	}
	if origin, ok := finiteConversionOrigin(flow, semantic, result, instructionID); ok {
		origin = resolveAlias(result.Aliases, origin)
		if flow.Values[instruction.Result].Type == flow.Values[origin].Type {
			return origin, true
		}
		if flow.Values[instruction.Result].Type == wasm.I32 && flow.Values[origin].Type == wasm.I64 {
			definition, args, defined := definingSemantic(flow, semantic, origin)
			if defined && len(args) == 1 && (definition.Op == wasm.InstrI64ExtendI32S || definition.Op == wasm.InstrI64ExtendI32U) {
				return resolveAlias(result.Aliases, args[0]), true
			}
		}
	}
	if instruction.Op != wasm.InstrI32WrapI64 {
		return 0, false
	}
	args := semantic.Operands(instructionID)
	if len(args) != 1 {
		return 0, false
	}
	truncValue := resolveAlias(result.Aliases, args[0])
	truncDef := flow.Values[truncValue]
	if truncDef.Kind != FlowValueInstruction {
		return 0, false
	}
	truncID := semantic.InstructionMap[truncDef.Instr]
	if truncID == 0 {
		return 0, false
	}
	definition := semantic.Insts[truncID-1]
	definitionArgs := semantic.Operands(truncID - 1)
	if len(definitionArgs) == 1 && (definition.Op == wasm.InstrI64ExtendI32S || definition.Op == wasm.InstrI64ExtendI32U) {
		return resolveAlias(result.Aliases, definitionArgs[0]), true
	}
	origin, ok := finiteConversionOrigin(flow, semantic, result, truncID-1)
	if !ok || flow.Values[origin].Type != wasm.I32 {
		return 0, false
	}
	return resolveAlias(result.Aliases, origin), true
}

func exactIntegerFloatOrigin(flow *ValueFlow, semantic *SemanticFunc, aliases []FlowValueID, value FlowValueID, inheritedMax uint64) (FlowValueID, uint64, bool) {
	value = resolveAlias(aliases, value)
	instruction, args, ok := definingSemantic(flow, semantic, value)
	if !ok || len(args) != 1 {
		return 0, 0, false
	}
	limit := inheritedMax
	setLimit := func(candidate uint64) {
		if limit == 0 || candidate < limit {
			limit = candidate
		}
	}
	switch instruction.Op {
	case wasm.InstrF32ConvertI32S, wasm.InstrF32ConvertI32U, wasm.InstrF32ConvertI64S, wasm.InstrF32ConvertI64U:
		setLimit(1 << 24)
		return resolveAlias(aliases, args[0]), limit, true
	case wasm.InstrF64ConvertI32S, wasm.InstrF64ConvertI32U, wasm.InstrF64ConvertI64S, wasm.InstrF64ConvertI64U:
		setLimit(1 << 53)
		return resolveAlias(aliases, args[0]), limit, true
	case wasm.InstrF32DemoteF64:
		setLimit(1 << 24)
		return exactIntegerFloatOrigin(flow, semantic, aliases, args[0], limit)
	case wasm.InstrF64PromoteF32:
		return exactIntegerFloatOrigin(flow, semantic, aliases, args[0], limit)
	default:
		return 0, 0, false
	}
}

func localPureGVN(cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, result *SimplifyResult, consume func() bool) error {
	maxBlock := uint32(0)
	for _, block := range semantic.Blocks {
		maxBlock = max(maxBlock, block.InstCount)
	}
	size := 4
	for uint64(size) < uint64(maxBlock)*4 {
		size <<= 1
	}
	result.gvnKeys = resizeClear(result.gvnKeys, size)
	result.gvnValues = resizeClear(result.gvnValues, size)
	mask := uint64(size - 1)
	insert := func(instructionID uint32) {
		instruction := semantic.Insts[instructionID]
		if instruction.Result == 0 || !pureGVNOp(instruction.Op) {
			return
		}
		meta := metadata.Instructions[instruction.Source]
		if !gvnEligible(instruction.Op, meta) {
			return
		}
		hash := gvnHash(semantic, result.Aliases, instructionID)
		key := hash
		if key == 0 {
			key = 1
		}
		for probe := uint64(0); probe <= mask; probe++ {
			slot := (hash + probe) & mask
			if result.gvnKeys[slot] == 0 {
				result.gvnKeys[slot], result.gvnValues[slot] = key, instruction.Result
				return
			}
		}
	}
	for blockID, block := range semantic.Blocks {
		clear(result.gvnKeys)
		clear(result.gvnValues)
		// Seed every block on the unique-predecessor chain. Such a chain is a
		// directly replayable dominance proof, and retaining all of its pure
		// values lets control-heavy loops reuse work computed before successive
		// early-exit checks. Stop at a join or cycle; the independent verifier
		// repeats the same dominance walk for every committed alias.
		predecessorOf := BlockID(blockID)
		for steps := 0; steps < len(cfg.Blocks); steps++ {
			predRecord := cfg.Blocks[predecessorOf]
			if predRecord.PredCount != 1 {
				break
			}
			predecessor := cfg.Preds[predRecord.PredStart]
			if int(predecessor) >= len(semantic.Blocks) || predecessor == predecessorOf || predecessor == BlockID(blockID) {
				break
			}
			predBlock := semantic.Blocks[predecessor]
			for instructionID := predBlock.InstStart; instructionID < predBlock.InstStart+predBlock.InstCount; instructionID++ {
				// Runtime-state reads are CSEd only inside one basic block. This
				// avoids needing path-sensitive memory epochs at joins.
				if semantic.Insts[instructionID].Op == wasm.InstrMemorySize {
					continue
				}
				insert(instructionID)
			}
			predecessorOf = predecessor
		}
		for instructionID := block.InstStart; instructionID < block.InstStart+block.InstCount; instructionID++ {
			instruction := semantic.Insts[instructionID]
			if instruction.Result == 0 || !pureGVNOp(instruction.Op) {
				continue
			}
			meta := metadata.Instructions[instruction.Source]
			if !gvnEligible(instruction.Op, meta) {
				continue
			}
			hash := gvnHash(semantic, result.Aliases, instructionID)
			key := hash
			if key == 0 {
				key = 1
			}
			for probe := uint64(0); probe <= mask; probe++ {
				slot := (hash + probe) & mask
				if result.gvnKeys[slot] == 0 {
					result.gvnKeys[slot], result.gvnValues[slot] = key, instruction.Result
					break
				}
				candidate := resolveAlias(result.Aliases, result.gvnValues[slot])
				if result.gvnKeys[slot] == key && gvnEquivalent(flow, semantic, result.Aliases, instructionID, candidate) {
					if consume() {
						result.Aliases[instruction.Result] = candidate
						result.Metrics.Aliases++
						if semanticInstructionBlock(semantic, instructionID) != semanticInstructionBlock(semantic, semantic.InstructionMap[flow.Values[candidate].Instr]-1) {
							result.Metrics.CrossBlockAliases++
						}
					}
					break
				}
			}
		}
	}
	return nil
}

func gvnHash(semantic *SemanticFunc, aliases []FlowValueID, instructionID uint32) uint64 {
	instruction := semantic.Insts[instructionID]
	hash := uint64(instruction.Op)*0x9e3779b185ebca87 ^ instruction.Aux
	for _, argument := range semantic.Operands(instructionID) {
		hash ^= uint64(resolveAlias(aliases, argument)) + 0x9e3779b97f4a7c15 + hash<<6 + hash>>2
	}
	return hash
}

func gvnEquivalent(flow *ValueFlow, semantic *SemanticFunc, aliases []FlowValueID, instructionID uint32, candidate FlowValueID) bool {
	definition, candidateArgs, ok := definingSemantic(flow, semantic, candidate)
	if !ok {
		return false
	}
	instruction := semantic.Insts[instructionID]
	args := semantic.Operands(instructionID)
	if instruction.Op != definition.Op || instruction.Aux != definition.Aux || len(args) != len(candidateArgs) {
		return false
	}
	for index := range args {
		if resolveAlias(aliases, args[index]) != resolveAlias(aliases, candidateArgs[index]) {
			return false
		}
	}
	return true
}

func pureGVNOp(kind wasm.InstrKind) bool {
	switch kind {
	case wasm.InstrI32Const, wasm.InstrI64Const, wasm.InstrF32Const, wasm.InstrF64Const,
		wasm.InstrI32Eqz, wasm.InstrI64Eqz,
		wasm.InstrI32Clz, wasm.InstrI32Ctz, wasm.InstrI32Popcnt,
		wasm.InstrI64Clz, wasm.InstrI64Ctz, wasm.InstrI64Popcnt,
		wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI32Mul,
		wasm.InstrI64Add, wasm.InstrI64Sub, wasm.InstrI64Mul,
		wasm.InstrI32And, wasm.InstrI32Or, wasm.InstrI32Xor,
		wasm.InstrI64And, wasm.InstrI64Or, wasm.InstrI64Xor,
		wasm.InstrI32Shl, wasm.InstrI32ShrS, wasm.InstrI32ShrU, wasm.InstrI32Rotl, wasm.InstrI32Rotr,
		wasm.InstrI64Shl, wasm.InstrI64ShrS, wasm.InstrI64ShrU, wasm.InstrI64Rotl, wasm.InstrI64Rotr,
		wasm.InstrF32Add, wasm.InstrF32Sub, wasm.InstrF32Mul, wasm.InstrF32Div,
		wasm.InstrF64Add, wasm.InstrF64Sub, wasm.InstrF64Mul, wasm.InstrF64Div,
		wasm.InstrI32Eq, wasm.InstrI32Ne, wasm.InstrI32LtS, wasm.InstrI32LtU, wasm.InstrI32GtS, wasm.InstrI32GtU, wasm.InstrI32LeS, wasm.InstrI32LeU, wasm.InstrI32GeS, wasm.InstrI32GeU,
		wasm.InstrI64Eq, wasm.InstrI64Ne, wasm.InstrI64LtS, wasm.InstrI64LtU, wasm.InstrI64GtS, wasm.InstrI64GtU, wasm.InstrI64LeS, wasm.InstrI64LeU, wasm.InstrI64GeS, wasm.InstrI64GeU,
		wasm.InstrI32WrapI64, wasm.InstrI64ExtendI32S, wasm.InstrI64ExtendI32U,
		wasm.InstrMemorySize:
		return true
	default:
		return false
	}
}

func gvnEligible(kind wasm.InstrKind, meta InstructionMetadata) bool {
	if meta.Writes != 0 || meta.Flags != 0 || meta.Traps != 0 || meta.Obligations != 0 {
		return false
	}
	if kind == wasm.InstrMemorySize {
		return meta.Reads == HeapLinearMemory|HeapRuntimeState
	}
	return meta.Reads == 0
}

func resolveAlias(aliases []FlowValueID, value FlowValueID) FlowValueID {
	for aliases[value] != value {
		value = aliases[value]
	}
	return value
}

func setIntegerConstant(fact *IntegerFact, value uint64, typ wasm.ValType) {
	width := uint8(64)
	if typ == wasm.I32 {
		width, value = 32, uint64(uint32(value))
	}
	mask := ^uint64(0)
	if width == 32 {
		mask = math.MaxUint32
	}
	*fact = IntegerFact{KnownZero: ^value & mask, KnownOne: value & mask, Min: value, Max: value, Known: true, RangeKnown: true, Width: width}
}

func inferIntegerFact(op wasm.InstrKind, args []FlowValueID, facts *SimplifyResult, typ wasm.ValType) (IntegerFact, bool) {
	if len(args) == 1 {
		source := facts.IntegerFactAt(args[0])
		if width := signExtensionBits(op); width != 0 {
			if source.SignExtBits >= width {
				return source, true
			}
			return IntegerFact{Width: source.Width, SignExtBits: width}, true
		}
		if !source.RangeKnown {
			return IntegerFact{}, false
		}
		switch op {
		case wasm.InstrI64ExtendI32U:
			return IntegerFact{KnownZero: source.KnownZero | 0xffffffff00000000, KnownOne: source.KnownOne, Min: source.Min, Max: source.Max, RangeKnown: true, Width: 64}, true
		case wasm.InstrI64ExtendI32S:
			if source.Max <= math.MaxInt32 {
				return IntegerFact{KnownZero: source.KnownZero | 0xffffffff00000000, KnownOne: source.KnownOne, Min: source.Min, Max: source.Max, RangeKnown: true, Width: 64}, true
			}
		case wasm.InstrI32WrapI64:
			if source.Max <= math.MaxUint32 {
				return IntegerFact{KnownZero: source.KnownZero & math.MaxUint32, KnownOne: source.KnownOne & math.MaxUint32, Min: source.Min, Max: source.Max, RangeKnown: true, Width: 32}, true
			}
		}
		return IntegerFact{}, false
	}
	if len(args) != 2 || typ != wasm.I32 && typ != wasm.I64 {
		return IntegerFact{}, false
	}
	width := uint8(64)
	mask := ^uint64(0)
	if typ == wasm.I32 {
		width, mask = 32, math.MaxUint32
	}
	a, b := facts.IntegerFactAt(args[0]), facts.IntegerFactAt(args[1])
	var zero, one uint64
	switch op {
	case wasm.InstrI32Add, wasm.InstrI32Sub, wasm.InstrI64Add, wasm.InstrI64Sub:
		alignment := min(trailingKnownZeros(a.KnownZero, width), trailingKnownZeros(b.KnownZero, width))
		if alignment == 64 {
			zero = math.MaxUint64
		} else if alignment != 0 {
			zero = uint64(1)<<alignment - 1
		}
	case wasm.InstrI32And, wasm.InstrI64And:
		zero, one = a.KnownZero|b.KnownZero, a.KnownOne&b.KnownOne
	case wasm.InstrI32Or, wasm.InstrI64Or:
		zero, one = a.KnownZero&b.KnownZero, a.KnownOne|b.KnownOne
	case wasm.InstrI32Xor, wasm.InstrI64Xor:
		zero = a.KnownZero&b.KnownZero | a.KnownOne&b.KnownOne
		one = a.KnownOne&b.KnownZero | a.KnownZero&b.KnownOne
	default:
		return IntegerFact{}, false
	}
	zero, one = zero&mask, one&mask
	return IntegerFact{KnownZero: zero, KnownOne: one, Min: one, Max: mask &^ zero, RangeKnown: true, Width: width}, true
}

func signExtensionBits(op wasm.InstrKind) uint8 {
	switch op {
	case wasm.InstrI32Extend8S, wasm.InstrI64Extend8S:
		return 8
	case wasm.InstrI32Extend16S, wasm.InstrI64Extend16S:
		return 16
	case wasm.InstrI64Extend32S:
		return 32
	default:
		return 0
	}
}

// redundantSignExtensionAlias removes a sign extension only when the source is
// independently known to have already been sign-extended from an equal or
// narrower width. The marker is killed by every operation other than another
// sign extension, so this cannot cross arithmetic that may change the sign bit.
func redundantSignExtensionAlias(flow *ValueFlow, semantic *SemanticFunc, result *SimplifyResult, instructionID uint32) (FlowValueID, bool) {
	if int(instructionID) >= len(semantic.Insts) {
		return 0, false
	}
	instruction := semantic.Insts[instructionID]
	width := signExtensionBits(instruction.Op)
	args := semantic.Operands(instructionID)
	if width == 0 || len(args) != 1 {
		return 0, false
	}
	source := resolveAlias(result.Aliases, args[0])
	return source, int(source) < len(flow.Values) && result.IntegerFactAt(source).SignExtBits >= width
}

func trailingKnownZeros(knownZero uint64, width uint8) int {
	knownZero &= integerWidthMask(width)
	count := bits.TrailingZeros64(^knownZero)
	if count > int(width) {
		return int(width)
	}
	return count
}

// maskedInductionFact proves the unsigned range of an i32 block parameter when
// every incoming value is either a direct aligned constant or the exact
// recurrence (param +/- aligned-constant) & constant-mask. The proof is closed
// under i32 wraparound because addition/subtraction by an aligned step and AND
// both preserve the low zero bits.
func maskedInductionFact(f *StackFunc, flow *ValueFlow, semantic *SemanticFunc, aliases []FlowValueID, value FlowValueID) (IntegerFact, bool) {
	value = resolveAlias(aliases, value)
	if value == 0 || int(value) >= len(flow.Values) || flow.Values[value].Kind != FlowValueBlockParam || flow.Values[value].Type != wasm.I32 {
		return IntegerFact{}, false
	}
	alignment := 32
	baseSeen, recurrenceSeen := false, false
	var maximum uint32
	for _, incoming := range flow.EdgeArgs {
		if resolveAlias(aliases, incoming.Param) != value {
			continue
		}
		argument := resolveAlias(aliases, incoming.Argument)
		if constant, ok := directI32Constant(f, flow, semantic, aliases, argument); ok {
			baseSeen = true
			maximum = max(maximum, constant)
			alignment = min(alignment, bits.TrailingZeros32(constant))
			continue
		}
		mask, stepAlignment, ok := maskedInductionChain(f, flow, semantic, aliases, value, argument)
		if !ok {
			return IntegerFact{}, false
		}
		recurrenceSeen = true
		alignment = min(alignment, stepAlignment)
		maximum = max(maximum, mask)
	}
	if !baseSeen || !recurrenceSeen || alignment == 0 {
		return IntegerFact{}, false
	}
	lowZero := uint32(math.MaxUint32)
	if alignment < 32 {
		lowZero = uint32(1)<<alignment - 1
	}
	maximum &^= lowZero
	return IntegerFact{KnownZero: uint64(lowZero), Min: 0, Max: uint64(maximum), RangeKnown: true, Width: 32}, true
}

func maskedInductionChain(f *StackFunc, flow *ValueFlow, semantic *SemanticFunc, aliases []FlowValueID, param, value FlowValueID) (maximum uint32, alignment int, ok bool) {
	alignment = 32
	for fuel := len(semantic.Insts) + 1; fuel != 0; fuel-- {
		value = resolveAlias(aliases, value)
		if value == param {
			return maximum, alignment, true
		}
		previous, mask, step, matched := maskedAffineStep(f, flow, semantic, aliases, value)
		if !matched {
			return 0, 0, false
		}
		maximum = max(maximum, mask)
		alignment = min(alignment, bits.TrailingZeros32(step))
		value = previous
	}
	return 0, 0, false
}

func maskedAffineStep(f *StackFunc, flow *ValueFlow, semantic *SemanticFunc, aliases []FlowValueID, value FlowValueID) (previous FlowValueID, mask, step uint32, ok bool) {
	instruction, args, ok := definingSemantic(flow, semantic, value)
	if !ok || instruction.Op != wasm.InstrI32And || len(args) != 2 {
		return 0, 0, 0, false
	}
	mask64, constantOnRight := directI32Constant(f, flow, semantic, aliases, resolveAlias(aliases, args[1]))
	other := resolveAlias(aliases, args[0])
	if !constantOnRight {
		mask64, ok = directI32Constant(f, flow, semantic, aliases, resolveAlias(aliases, args[0]))
		if !ok {
			return 0, 0, 0, false
		}
		other = resolveAlias(aliases, args[1])
	}
	update, updateArgs, ok := definingSemantic(flow, semantic, other)
	if !ok || len(updateArgs) != 2 || update.Op != wasm.InstrI32Add && update.Op != wasm.InstrI32Sub {
		return 0, 0, 0, false
	}
	left, right := resolveAlias(aliases, updateArgs[0]), resolveAlias(aliases, updateArgs[1])
	constant, rightConstant := directI32Constant(f, flow, semantic, aliases, right)
	if update.Op == wasm.InstrI32Sub {
		if !rightConstant {
			return 0, 0, 0, false
		}
		return left, mask64, constant, true
	}
	if rightConstant {
		return left, mask64, constant, true
	}
	constant, leftConstant := directI32Constant(f, flow, semantic, aliases, left)
	if leftConstant {
		return right, mask64, constant, true
	}
	return 0, 0, 0, false
}

func directI32Constant(f *StackFunc, flow *ValueFlow, semantic *SemanticFunc, aliases []FlowValueID, value FlowValueID) (uint32, bool) {
	value = resolveAlias(aliases, value)
	if value == 0 || int(value) >= len(flow.Values) || flow.Values[value].Type != wasm.I32 {
		return 0, false
	}
	definition := flow.Values[value]
	if definition.Kind == FlowValueInitialLocal && int(definition.Local) >= len(f.Params) {
		return 0, true
	}
	instruction, _, ok := definingSemantic(flow, semantic, value)
	if !ok || instruction.Op != wasm.InstrI32Const {
		return 0, false
	}
	return uint32(instruction.Aux), true
}

func definingSemantic(flow *ValueFlow, semantic *SemanticFunc, value FlowValueID) (SemanticInst, []FlowValueID, bool) {
	if value == 0 || int(value) >= len(flow.Values) {
		return SemanticInst{}, nil, false
	}
	definition := flow.Values[value]
	if definition.Kind != FlowValueInstruction || int(definition.Instr) >= len(semantic.InstructionMap) {
		return SemanticInst{}, nil, false
	}
	mapped := semantic.InstructionMap[definition.Instr]
	if mapped == 0 || int(mapped-1) >= len(semantic.Insts) {
		return SemanticInst{}, nil, false
	}
	instruction := semantic.Insts[mapped-1]
	return instruction, semantic.Operands(mapped - 1), true
}

func computeSimplifiedReachability(cfg *CFG, semantic *SemanticFunc, result *SimplifyResult) {
	work := make([]BlockID, 1, len(cfg.Blocks))
	work[0], result.Reachable[0] = 0, true
	for len(work) != 0 {
		block := work[len(work)-1]
		work = work[:len(work)-1]
		decision := BranchUnknown
		record := semantic.Blocks[block]
		if record.InstCount != 0 {
			decision = result.Branches[record.InstStart+record.InstCount-1]
		}
		for _, edge := range cfg.Edges {
			if edge.From != block || decision == BranchTrue && edge.Kind == EdgeFalse || decision == BranchFalse && edge.Kind == EdgeTrue {
				continue
			}
			if !result.Reachable[edge.To] {
				result.Reachable[edge.To] = true
				work = append(work, edge.To)
			}
		}
	}
}

func buildUseCSR(flow *ValueFlow, semantic *SemanticFunc, result *SimplifyResult, budget uint32) error {
	count := uint64(0)
	for semanticID := range semantic.Insts {
		for _, argument := range semantic.Operands(uint32(semanticID)) {
			argument = resolveAlias(result.Aliases, argument)
			result.UseOffsets[argument+1]++
			count++
		}
	}
	for _, edge := range flow.EdgeArgs {
		argument := resolveAlias(result.Aliases, edge.Argument)
		result.UseOffsets[argument+1]++
		count++
	}
	if count > uint64(budget) {
		return fmt.Errorf("railssa: use CSR requires %d cells, exceeds %d", count, budget)
	}
	for index := 1; index < len(result.UseOffsets); index++ {
		result.UseOffsets[index] += result.UseOffsets[index-1]
	}
	result.Uses = resizeClear(result.Uses, int(count))
	cursor := append([]uint32(nil), result.UseOffsets[:len(result.UseOffsets)-1]...)
	for semanticID := range semantic.Insts {
		for _, argument := range semantic.Operands(uint32(semanticID)) {
			argument = resolveAlias(result.Aliases, argument)
			result.Uses[cursor[argument]] = uint32(semanticID) + 1
			cursor[argument]++
		}
	}
	for edgeID, edge := range flow.EdgeArgs {
		argument := resolveAlias(result.Aliases, edge.Argument)
		result.Uses[cursor[argument]] = ^uint32(edgeID)
		cursor[argument]++
	}
	return nil
}

func markLiveSemantic(f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, result *SimplifyResult) {
	work := result.liveWork[:0]
	markValue := func(value FlowValueID) {
		value = resolveAlias(result.Aliases, value)
		if value == 0 || result.liveValues[value] {
			return
		}
		result.liveValues[value] = true
		work = append(work, value)
	}
	markInstruction := func(id uint32) {
		if result.LiveInsts[id] {
			return
		}
		result.LiveInsts[id] = true
		for _, argument := range semantic.Operands(id) {
			markValue(argument)
		}
	}
	for block, record := range semantic.Blocks {
		for id := record.InstStart; id < record.InstStart+record.InstCount; id++ {
			result.instructionBlock[id] = BlockID(block)
		}
	}
	for id, instruction := range semantic.Insts {
		blockReachable := result.Reachable[result.instructionBlock[id]]
		meta := metadata.Instructions[instruction.Source]
		if blockReachable && (instruction.Result == 0 || meta.Reads != 0 || meta.Writes != 0 || meta.Flags != 0 || cfgTerminator(f.Instrs[instruction.Source])) {
			markInstruction(uint32(id))
		}
	}
	exit := BlockID(len(cfg.Blocks) - 1)
	if result.Reachable[exit] {
		for _, value := range flow.BlockEntry(exit) {
			markValue(value)
		}
	}
	for len(work) != 0 {
		value := work[len(work)-1]
		work = work[:len(work)-1]
		definition := flow.Values[value]
		switch definition.Kind {
		case FlowValueInstruction:
			semanticID := semantic.InstructionMap[definition.Instr]
			if semanticID != 0 {
				markInstruction(semanticID - 1)
			}
		case FlowValueBlockParam:
			for _, argument := range flow.EdgeArgs {
				if argument.Param != value || int(argument.Edge) >= len(cfg.Edges) {
					continue
				}
				edge := cfg.Edges[argument.Edge]
				if result.Reachable[edge.From] && simplifiedEdgeExecutable(cfg, semantic, result, argument.Edge) {
					markValue(argument.Argument)
				}
			}
		}
	}
	result.liveWork = work[:0]
}

func simplifiedEdgeExecutable(cfg *CFG, semantic *SemanticFunc, result *SimplifyResult, edgeID uint32) bool {
	edge := cfg.Edges[edgeID]
	record := semantic.Blocks[edge.From]
	decision := BranchUnknown
	if record.InstCount != 0 {
		decision = result.Branches[record.InstStart+record.InstCount-1]
	}
	return !(decision == BranchTrue && edge.Kind == EdgeFalse || decision == BranchFalse && edge.Kind == EdgeTrue)
}

func memoryAccessBytes(kind wasm.InstrKind) uint64 {
	switch kind {
	case wasm.InstrI32Load8S, wasm.InstrI32Load8U, wasm.InstrI64Load8S, wasm.InstrI64Load8U, wasm.InstrI32Store8, wasm.InstrI64Store8:
		return 1
	case wasm.InstrI32Load16S, wasm.InstrI32Load16U, wasm.InstrI64Load16S, wasm.InstrI64Load16U, wasm.InstrI32Store16, wasm.InstrI64Store16:
		return 2
	case wasm.InstrI64Load32S, wasm.InstrI64Load32U, wasm.InstrI32Load, wasm.InstrF32Load, wasm.InstrI32Store, wasm.InstrF32Store, wasm.InstrI64Store32:
		return 4
	case wasm.InstrI64Load, wasm.InstrF64Load, wasm.InstrI64Store, wasm.InstrF64Store:
		return 8
	default:
		return 0
	}
}

func VerifySimplify(f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, result *SimplifyResult) error {
	factsValid := result != nil && (len(result.factIndex) == 0 && len(result.Facts) == len(flow.Values) || len(result.factIndex) == len(flow.Values))
	if result == nil || len(result.Aliases) != len(flow.Values) || !factsValid || len(result.Reachable) != len(cfg.Blocks) || len(result.LiveInsts) != len(semantic.Insts) || len(result.Remaining) != len(semantic.Insts) || len(result.UseOffsets) != len(flow.Values)+1 {
		return fmt.Errorf("railssa: malformed SparseSimplify result")
	}
	if len(result.factIndex) != 0 {
		slot := uint32(1)
		for value, record := range flow.Values {
			integer := record.Type == wasm.I32 || record.Type == wasm.I64
			if integer && result.factIndex[value] != slot || !integer && result.factIndex[value] != 0 {
				return fmt.Errorf("railssa: malformed integer fact index at v%d", value)
			}
			if integer {
				slot++
			}
		}
		if int(slot-1) != len(result.Facts) {
			return fmt.Errorf("railssa: integer fact index has %d slots, want %d", slot-1, len(result.Facts))
		}
	}
	verifiedTrivialArguments := uint32(0)
	for id, alias := range result.Aliases {
		if int(alias) >= len(flow.Values) || resolveAlias(result.Aliases, FlowValueID(id)) == 0 && id != 0 {
			return fmt.Errorf("railssa: invalid alias v%d -> v%d", id, alias)
		}
		if id != 0 && alias != FlowValueID(id) && flow.Values[id].Type != flow.Values[alias].Type {
			return fmt.Errorf("railssa: alias v%d changes type", id)
		}
		if id != 0 && alias != FlowValueID(id) {
			switch flow.Values[id].Kind {
			case FlowValueBlockParam:
				verifiedTrivialArguments++
				target, found := resolveAlias(result.Aliases, FlowValueID(id)), false
				for _, incoming := range flow.EdgeArgs {
					if incoming.Param != FlowValueID(id) {
						continue
					}
					argument := resolveAlias(result.Aliases, incoming.Argument)
					if argument == FlowValueID(id) {
						continue
					}
					if argument != target {
						return fmt.Errorf("railssa: parameter alias v%d disagrees with incoming v%d", id, argument)
					}
					found = true
				}
				if !found {
					return fmt.Errorf("railssa: parameter alias v%d has no supporting incoming value", id)
				}
			case FlowValueInstruction:
				current := semantic.InstructionMap[flow.Values[id].Instr]
				if current == 0 {
					return fmt.Errorf("railssa: alias v%d has no semantic instruction", id)
				}
				target := resolveAlias(result.Aliases, FlowValueID(id))
				conversionOrigin, conversionAlias := integerFloatRoundTripAlias(flow, semantic, result, current-1)
				if conversionAlias && resolveAlias(result.Aliases, conversionOrigin) == target && flow.Values[id].Type == flow.Values[target].Type {
					break
				}
				signOrigin, signAlias := redundantSignExtensionAlias(flow, semantic, result, current-1)
				if signAlias && resolveAlias(result.Aliases, signOrigin) == target && flow.Values[id].Type == flow.Values[target].Type {
					break
				}
				targetDef := flow.Values[target]
				if targetDef.Kind != FlowValueInstruction {
					return fmt.Errorf("railssa: GVN alias v%d has no instruction target", id)
				}
				previous := semantic.InstructionMap[targetDef.Instr]
				previousBlock, currentBlock := semanticInstructionBlock(semantic, previous-1), semanticInstructionBlock(semantic, current-1)
				currentInstruction := semantic.Insts[current-1]
				if previous == 0 || previous >= current || !gvnBlockDominates(cfg, previousBlock, currentBlock) || !pureGVNOp(currentInstruction.Op) || !gvnEquivalent(flow, semantic, result.Aliases, current-1, target) || currentInstruction.Op == wasm.InstrMemorySize && previousBlock != currentBlock {
					return fmt.Errorf("railssa: GVN alias v%d -> v%d lacks dominating equivalence", id, target)
				}
				previousMeta := metadata.Instructions[semantic.Insts[previous-1].Source]
				currentMeta := metadata.Instructions[currentInstruction.Source]
				for _, candidate := range [...]struct {
					kind wasm.InstrKind
					meta InstructionMetadata
				}{{semantic.Insts[previous-1].Op, previousMeta}, {currentInstruction.Op, currentMeta}} {
					if !gvnEligible(candidate.kind, candidate.meta) {
						return fmt.Errorf("railssa: GVN alias v%d crosses semantic effects", id)
					}
				}
				if currentInstruction.Op == wasm.InstrMemorySize && previousMeta.Epoch != currentMeta.Epoch {
					return fmt.Errorf("railssa: GVN alias v%d crosses memory epoch", id)
				}
			default:
				return fmt.Errorf("railssa: unsupported alias v%d -> v%d", id, alias)
			}
		}
	}
	if verifiedTrivialArguments != result.Metrics.TrivialArguments {
		return fmt.Errorf("railssa: trivial argument count %d does not match metrics %d", verifiedTrivialArguments, result.Metrics.TrivialArguments)
	}
	for id := range flow.Values {
		fact := result.IntegerFactAt(FlowValueID(id))
		if fact.Known && (!fact.RangeKnown || fact.Min != fact.Max) || fact.RangeKnown && fact.Min > fact.Max || fact.KnownOne&fact.KnownZero != 0 || fact.RangeKnown && (fact.Min < fact.KnownOne || fact.Max > (^fact.KnownZero&integerWidthMask(fact.Width))) {
			return fmt.Errorf("railssa: value %d has inconsistent integer fact", id)
		}
		if id == 0 || fact.Known || !fact.RangeKnown {
			continue
		}
		value := flow.Values[id]
		switch value.Kind {
		case FlowValueBlockParam:
			expected, ok := maskedInductionFact(f, flow, semantic, result.Aliases, FlowValueID(id))
			if !ok || fact != expected {
				return fmt.Errorf("railssa: block parameter v%d has unproved range fact %#v", id, fact)
			}
		case FlowValueInstruction:
			instruction, args, ok := definingSemantic(flow, semantic, FlowValueID(id))
			if !ok {
				return fmt.Errorf("railssa: instruction value v%d has no semantic definition", id)
			}
			var local [3]FlowValueID
			resolved := args
			if len(args) <= len(local) {
				resolved = local[:len(args)]
				for index, argument := range args {
					resolved[index] = resolveAlias(result.Aliases, argument)
				}
			}
			expected, ok := inferIntegerFact(instruction.Op, resolved, result, value.Type)
			if !ok || fact != expected {
				return fmt.Errorf("railssa: instruction value v%d has unproved range fact %#v", id, fact)
			}
		default:
			return fmt.Errorf("railssa: value v%d has range without a verified derivation", id)
		}
	}
	for _, certificate := range result.Bounds {
		if int(certificate.Instruction) >= len(semantic.Insts) || certificate.MemoryBytes != f.MemoryMinBytes || certificate.End > certificate.MemoryBytes || semantic.Insts[certificate.Instruction].Source >= uint32(len(metadata.Instructions)) {
			return fmt.Errorf("railssa: invalid bounds certificate %#v", certificate)
		}
		instruction := semantic.Insts[certificate.Instruction]
		args := semantic.Operands(certificate.Instruction)
		if len(args) == 0 || resolveAlias(result.Aliases, args[0]) != certificate.Address {
			return fmt.Errorf("railssa: bounds certificate address does not match instruction %#v", certificate)
		}
		fact := result.IntegerFactAt(certificate.Address)
		width, offset := memoryAccessBytes(instruction.Op), uint64(uint32(instruction.Aux))
		end := fact.Max + offset + width
		if !fact.RangeKnown || width == 0 || end < fact.Max || end < offset || end != certificate.End {
			return fmt.Errorf("railssa: bounds certificate lacks range evidence %#v", certificate)
		}
	}
	for instructionID, instruction := range semantic.Insts {
		meta := metadata.Instructions[instruction.Source]
		expected := meta.Obligations
		args := semantic.Operands(uint32(instructionID))
		if expected&ObligationMemoryBounds != 0 {
			for _, certificate := range result.Bounds {
				if certificate.Instruction == uint32(instructionID) {
					expected &^= ObligationMemoryBounds
					break
				}
			}
		}
		if len(args) >= 2 && expected&ObligationNonzeroDivisor != 0 {
			divisor := resolveAlias(result.Aliases, args[1])
			if fact := result.IntegerFactAt(divisor); fact.Known && fact.Min != 0 {
				expected &^= ObligationNonzeroDivisor
			}
		}
		if expected&ObligationFiniteConversion != 0 && finiteConversionProven(flow, semantic, result, uint32(instructionID)) {
			expected &^= ObligationFiniteConversion
		}
		if result.Remaining[instructionID] != expected {
			return fmt.Errorf("railssa: instruction %d obligations %#x, want %#x", instructionID, result.Remaining[instructionID], expected)
		}
	}
	if len(result.Uses) != int(result.UseOffsets[len(result.UseOffsets)-1]) {
		return fmt.Errorf("railssa: use CSR length mismatch")
	}
	return nil
}

func gvnBlockDominates(cfg *CFG, candidate, use BlockID) bool {
	if candidate == use {
		return true
	}
	for steps := 0; steps < len(cfg.Blocks) && int(use) < len(cfg.Blocks); steps++ {
		block := cfg.Blocks[use]
		if block.PredCount != 1 {
			return false
		}
		use = cfg.Preds[block.PredStart]
		if use == candidate {
			return true
		}
	}
	return false
}

func semanticInstructionBlock(semantic *SemanticFunc, instruction uint32) BlockID {
	for blockID, block := range semantic.Blocks {
		if instruction >= block.InstStart && instruction < block.InstStart+block.InstCount {
			return BlockID(blockID)
		}
	}
	return ^BlockID(0)
}

func integerWidthMask(width uint8) uint64 {
	if width == 32 {
		return math.MaxUint32
	}
	if width == 64 {
		return math.MaxUint64
	}
	return 0
}
