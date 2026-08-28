package railssa

import (
	"fmt"
	"slices"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type BlockID uint32

type EdgeKind uint8

const (
	EdgeFallthrough EdgeKind = iota
	EdgeBranch
	EdgeTrue
	EdgeFalse
	EdgeTable
	EdgeReturn
)

const (
	BlockLoopHeader uint16 = 1 << iota
	BlockExit
)

// Block is the dense CFG record consumed by block-argument RailSSA building.
type Block struct {
	InstStart uint32
	InstCount uint32

	PredStart uint32
	PredCount uint16
	SuccStart uint32
	SuccCount uint16

	Region RegionID
	Flags  uint16
	Weight uint32
}

type Edge struct {
	From BlockID
	To   BlockID
	Kind EdgeKind
}

// EdgeRefinement gives one transferred stack slot its edge-specific reference
// type. Branch-cast values keep one native identity, while each successor gets
// a distinct typed block argument.
type EdgeRefinement struct {
	Edge uint32
	Slot uint32
	Type wasm.ValType
}

// EdgeStack is the compact operand-stack transfer contract aligned with
// CFG.Edges. Carry edges forward the complete source stack. Structured branch
// edges keep PrefixDepth values and append ResultArity branch results.
type EdgeStack uint64

const edgeStackCarry EdgeStack = 1 << 63

func carryEdgeStack() EdgeStack { return edgeStackCarry }

func prefixEdgeStack(depth uint32, resultArity uint16) EdgeStack {
	return EdgeStack(depth) | EdgeStack(resultArity)<<32
}

func (s EdgeStack) CarriesAll() bool    { return s&edgeStackCarry != 0 }
func (s EdgeStack) PrefixDepth() uint32 { return uint32(s) }
func (s EdgeStack) ResultArity() uint16 { return uint16(s >> 32) }

// CFG owns flat block, edge, predecessor, and successor slabs. Its zero value
// is reusable across functions.
type CFG struct {
	Blocks      []Block
	Edges       []Edge
	EdgeStacks  []EdgeStack
	Refinements []EdgeRefinement
	Preds       []BlockID
	Succs       []BlockID

	leaders       []bool
	regionAtStart []RegionID
	regionAtElse  []RegionID
	regionAtEnd   []RegionID
	raw           []rawEdge
	active        []RegionID
	starts        []uint32
	instrBlock    []BlockID
	planned       []plannedEdge
}

type rawEdge struct {
	from       uint32
	to         uint32
	kind       EdgeKind
	stack      EdgeStack
	refineSlot uint32
	refineType wasm.ValType
}

type plannedEdge struct {
	edge       Edge
	stack      EdgeStack
	refineSlot uint32
	refineType wasm.ValType
}

// BuildCFG constructs exact normal-control edges from the structured prepass.
// It does not create semantic instructions or block arguments.
func BuildCFG(f *StackFunc, reuse *CFG) (*CFG, error) {
	if f == nil || len(f.Instrs) == 0 {
		return nil, fmt.Errorf("railssa: cannot build CFG for empty function")
	}
	if reuse == nil {
		reuse = new(CFG)
	}
	blocks := reuse.Blocks[:0]
	edges := reuse.Edges[:0]
	edgeStacks := reuse.EdgeStacks[:0]
	refinements := reuse.Refinements[:0]
	preds := reuse.Preds[:0]
	succs := reuse.Succs[:0]
	leadersScratch, regionAtStartScratch := reuse.leaders, reuse.regionAtStart
	regionAtElseScratch, regionAtEndScratch := reuse.regionAtElse, reuse.regionAtEnd
	rawScratch, activeScratch := reuse.raw, reuse.active
	startsScratch, instrBlockScratch, plannedScratch := reuse.starts, reuse.instrBlock, reuse.planned
	*reuse = CFG{Blocks: blocks, Edges: edges, EdgeStacks: edgeStacks, Refinements: refinements, Preds: preds, Succs: succs}

	n := len(f.Instrs)
	leaders := resizeClear(leadersScratch, n+1)
	leaders[0], leaders[n] = true, true
	regionAtStart := resizeClear(regionAtStartScratch, n)
	regionAtElse := resizeClear(regionAtElseScratch, n)
	regionAtEnd := resizeClear(regionAtEndScratch, n)
	for i := range regionAtStart {
		regionAtStart[i], regionAtElse[i], regionAtEnd[i] = NoRegion, NoRegion, NoRegion
	}
	for id := range f.Regions {
		region := &f.Regions[id]
		if int(region.StartInstr) >= n || int(region.EndInstr) >= n || region.EndInstr < region.StartInstr {
			return nil, fmt.Errorf("railssa: region %d has invalid [%d,%d] bounds", id, region.StartInstr, region.EndInstr)
		}
		regionAtStart[region.StartInstr] = RegionID(id)
		regionAtEnd[region.EndInstr] = RegionID(id)
		leaders[region.StartInstr+1] = true
		leaders[region.EndInstr+1] = true
		if region.ElseInstr != ^uint32(0) {
			if region.Kind != wasm.InstrIf || region.ElseInstr <= region.StartInstr || region.ElseInstr >= region.EndInstr {
				return nil, fmt.Errorf("railssa: region %d has invalid else boundary %d", id, region.ElseInstr)
			}
			regionAtElse[region.ElseInstr] = RegionID(id)
			leaders[region.ElseInstr+1] = true
		}
	}

	raw := rawScratch[:0]
	active := activeScratch[:0]
	branchTarget := func(label uint32) (uint32, uint32, uint16, error) {
		if int(label) > len(active) {
			return 0, 0, 0, fmt.Errorf("branch label %d exceeds depth %d", label, len(active))
		}
		if int(label) == len(active) {
			return uint32(n), 0, uint16(len(f.Results)), nil
		}
		region := f.Regions[active[len(active)-1-int(label)]]
		if region.Kind == wasm.InstrLoop {
			return region.StartInstr + 1, region.StackDepth, region.ParamArity, nil
		}
		return region.EndInstr + 1, region.StackDepth, region.ResultArity, nil
	}
	add := func(from, to uint32, kind EdgeKind, stack EdgeStack) {
		raw = append(raw, rawEdge{from: from, to: to, kind: kind, stack: stack})
		leaders[to] = true
	}
	addRefined := func(from, to uint32, kind EdgeKind, stack EdgeStack, slot uint32, typ wasm.ValType) {
		raw = append(raw, rawEdge{from: from, to: to, kind: kind, stack: stack, refineSlot: slot, refineType: typ})
		leaders[to] = true
	}
	for i := range f.Instrs {
		if region := regionAtStart[i]; region != NoRegion {
			active = append(active, region)
		}
		instr := f.Instrs[i]
		next := uint32(i + 1)
		switch {
		case instr.Kind == wasm.InstrIf:
			region := f.Regions[regionAtStart[i]]
			falseTarget := region.EndInstr + 1
			if region.ElseInstr != ^uint32(0) {
				falseTarget = region.ElseInstr + 1
			}
			stack := prefixEdgeStack(region.StackDepth, region.ParamArity)
			add(uint32(i), next, EdgeTrue, stack)
			add(uint32(i), falseTarget, EdgeFalse, stack)
			leaders[next] = true
		case instr.IsElse():
			region := regionAtElse[i]
			if region == NoRegion {
				return nil, fmt.Errorf("railssa: else instruction %d has no region", i)
			}
			control := f.Regions[region]
			add(uint32(i), control.EndInstr+1, EdgeBranch, prefixEdgeStack(control.StackDepth, control.ResultArity))
			leaders[next] = true
		case instr.Kind == wasm.InstrBr:
			target, depth, arity, err := branchTarget(instr.U32())
			if err != nil {
				return nil, fmt.Errorf("railssa: instruction %d: %w", i, err)
			}
			add(uint32(i), target, EdgeBranch, prefixEdgeStack(depth, arity))
			leaders[next] = true
		case instr.Kind == wasm.InstrBrIf:
			target, depth, arity, err := branchTarget(instr.U32())
			if err != nil {
				return nil, fmt.Errorf("railssa: instruction %d: %w", i, err)
			}
			add(uint32(i), target, EdgeTrue, prefixEdgeStack(depth, arity))
			add(uint32(i), next, EdgeFalse, carryEdgeStack())
			leaders[next] = true
		case instr.Kind == wasm.InstrBrOnCast || instr.Kind == wasm.InstrBrOnCastFail:
			immediate, ok := f.BranchCastImmediateAt(uint32(i))
			if !ok {
				return nil, fmt.Errorf("railssa: instruction %d has no branch-cast immediate", i)
			}
			target, depth, arity, err := branchTarget(immediate.Label)
			if err != nil {
				return nil, fmt.Errorf("railssa: instruction %d: %w", i, err)
			}
			if arity == 0 {
				return nil, fmt.Errorf("railssa: instruction %d branch cast target has no reference result", i)
			}
			addRefined(uint32(i), target, EdgeTrue, prefixEdgeStack(depth, arity), depth+uint32(arity)-1, immediate.BranchType)
			addRefined(uint32(i), next, EdgeFalse, carryEdgeStack(), ^uint32(0), immediate.Fallthrough)
			leaders[next] = true
		case instr.Kind == wasm.InstrBrTable:
			for _, label := range instr.Labels(f) {
				target, depth, arity, err := branchTarget(label)
				if err != nil {
					return nil, fmt.Errorf("railssa: instruction %d: %w", i, err)
				}
				add(uint32(i), target, EdgeTable, prefixEdgeStack(depth, arity))
			}
			leaders[next] = true
		case instr.Kind == wasm.InstrReturn:
			add(uint32(i), uint32(n), EdgeReturn, prefixEdgeStack(0, uint16(len(f.Results))))
			leaders[next] = true
		case instr.Kind == wasm.InstrUnreachable:
			leaders[next] = true
		}
		if region := regionAtEnd[i]; region != NoRegion {
			if len(active) == 0 || active[len(active)-1] != region {
				return nil, fmt.Errorf("railssa: region %d ends out of nesting order at instruction %d", region, i)
			}
			active = active[:len(active)-1]
		}
	}
	if len(active) != 0 {
		return nil, fmt.Errorf("railssa: CFG scan leaves %d active regions", len(active))
	}

	starts := startsScratch[:0]
	for i, leader := range leaders {
		if leader {
			starts = append(starts, uint32(i))
		}
	}
	instrBlock := resizeClear(instrBlockScratch, n+1)
	for i, start := range starts {
		block := Block{InstStart: start, Region: NoRegion, Weight: 1}
		if int(start) == n {
			block.Flags |= BlockExit
		} else {
			block.InstCount = starts[i+1] - start
			for regionID := range f.Regions {
				region := f.Regions[regionID]
				if start > region.StartInstr && start <= region.EndInstr {
					block.Region = RegionID(regionID)
				}
				if region.Kind == wasm.InstrLoop && start == region.StartInstr+1 {
					block.Flags |= BlockLoopHeader
				}
			}
			if block.Region != NoRegion {
				for depth := f.Regions[block.Region].LoopDepth; depth != 0; depth-- {
					if block.Weight > ^uint32(0)/8 {
						block.Weight = ^uint32(0)
						break
					}
					block.Weight *= 8
				}
			}
			for instruction := start; instruction < start+block.InstCount; instruction++ {
				instrBlock[instruction] = BlockID(i)
			}
		}
		instrBlock[start] = BlockID(i)
		reuse.Blocks = append(reuse.Blocks, block)
	}

	planned := plannedScratch[:0]
	for _, edge := range raw {
		planned = append(planned, plannedEdge{edge: Edge{From: instrBlock[edge.from], To: instrBlock[edge.to], Kind: edge.kind}, stack: edge.stack, refineSlot: edge.refineSlot, refineType: edge.refineType})
	}
	for blockID := 0; blockID+1 < len(reuse.Blocks); blockID++ {
		block := reuse.Blocks[blockID]
		last := f.Instrs[block.InstStart+block.InstCount-1]
		if !cfgTerminator(last) {
			planned = append(planned, plannedEdge{edge: Edge{From: BlockID(blockID), To: BlockID(blockID + 1), Kind: EdgeFallthrough}, stack: carryEdgeStack()})
		}
	}
	slices.SortFunc(planned, func(a, b plannedEdge) int {
		if a.edge.From != b.edge.From {
			return int(a.edge.From) - int(b.edge.From)
		}
		if a.edge.To != b.edge.To {
			return int(a.edge.To) - int(b.edge.To)
		}
		return int(a.edge.Kind) - int(b.edge.Kind)
	})
	for _, item := range planned {
		if len(reuse.Edges) != 0 {
			last := reuse.Edges[len(reuse.Edges)-1]
			if last.From == item.edge.From && last.To == item.edge.To {
				if reuse.EdgeStacks[len(reuse.EdgeStacks)-1] != item.stack {
					return nil, fmt.Errorf("railssa: duplicate edge %d -> %d has inconsistent stack transfer", item.edge.From, item.edge.To)
				}
				if item.refineType != (wasm.ValType{}) {
					return nil, fmt.Errorf("railssa: duplicate refined edge %d -> %d", item.edge.From, item.edge.To)
				}
				continue
			}
		}
		reuse.Edges = append(reuse.Edges, item.edge)
		reuse.EdgeStacks = append(reuse.EdgeStacks, item.stack)
		if item.refineType != (wasm.ValType{}) {
			reuse.Refinements = append(reuse.Refinements, EdgeRefinement{Edge: uint32(len(reuse.Edges) - 1), Slot: item.refineSlot, Type: item.refineType})
		}
	}
	for blockID := range reuse.Blocks {
		block := &reuse.Blocks[blockID]
		block.SuccStart = uint32(len(reuse.Succs))
		for _, edge := range reuse.Edges {
			if edge.From == BlockID(blockID) {
				reuse.Succs = append(reuse.Succs, edge.To)
			}
		}
		if len(reuse.Succs)-int(block.SuccStart) > int(^uint16(0)) {
			return nil, fmt.Errorf("railssa: block %d exceeds successor limit", blockID)
		}
		block.SuccCount = uint16(len(reuse.Succs) - int(block.SuccStart))
		block.PredStart = uint32(len(reuse.Preds))
		for _, edge := range reuse.Edges {
			if edge.To == BlockID(blockID) {
				reuse.Preds = append(reuse.Preds, edge.From)
			}
		}
		if len(reuse.Preds)-int(block.PredStart) > int(^uint16(0)) {
			return nil, fmt.Errorf("railssa: block %d exceeds predecessor limit", blockID)
		}
		block.PredCount = uint16(len(reuse.Preds) - int(block.PredStart))
	}
	reuse.leaders, reuse.regionAtStart, reuse.regionAtElse, reuse.regionAtEnd = leaders, regionAtStart, regionAtElse, regionAtEnd
	reuse.raw, reuse.active, reuse.starts, reuse.instrBlock, reuse.planned = raw, active, starts, instrBlock, planned
	if err := VerifyCFG(f, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func cfgTerminator(instr StackInstr) bool {
	return instr.Kind == wasm.InstrIf || instr.IsElse() || instr.Kind == wasm.InstrBr || instr.Kind == wasm.InstrBrIf ||
		instr.Kind == wasm.InstrBrOnCast || instr.Kind == wasm.InstrBrOnCastFail || instr.Kind == wasm.InstrBrTable || instr.Kind == wasm.InstrReturn || instr.Kind == wasm.InstrUnreachable
}

func VerifyCFG(f *StackFunc, cfg *CFG) error {
	if f == nil || cfg == nil || len(cfg.Blocks) == 0 {
		return fmt.Errorf("railssa: empty CFG")
	}
	expectedStart := uint32(0)
	for id, block := range cfg.Blocks {
		if block.InstStart != expectedStart {
			return fmt.Errorf("railssa: block %d starts at %d, want %d", id, block.InstStart, expectedStart)
		}
		if block.Flags&BlockExit != 0 {
			if id != len(cfg.Blocks)-1 || block.InstCount != 0 || block.InstStart != uint32(len(f.Instrs)) {
				return fmt.Errorf("railssa: malformed exit block %d", id)
			}
		} else if block.InstCount == 0 {
			return fmt.Errorf("railssa: non-exit block %d is empty", id)
		}
		expectedStart += block.InstCount
		if int(block.PredStart)+int(block.PredCount) > len(cfg.Preds) || int(block.SuccStart)+int(block.SuccCount) > len(cfg.Succs) {
			return fmt.Errorf("railssa: block %d CSR range is invalid", id)
		}
		for i, succ := range cfg.Succs[block.SuccStart : block.SuccStart+uint32(block.SuccCount)] {
			if int(succ) >= len(cfg.Blocks) {
				return fmt.Errorf("railssa: block %d has invalid successor %d", id, succ)
			}
			if i > 0 && cfg.Succs[block.SuccStart+uint32(i)-1] >= succ {
				return fmt.Errorf("railssa: block %d successors are duplicate or unordered", id)
			}
		}
		for _, pred := range cfg.Preds[block.PredStart : block.PredStart+uint32(block.PredCount)] {
			if int(pred) >= len(cfg.Blocks) {
				return fmt.Errorf("railssa: block %d has invalid predecessor %d", id, pred)
			}
		}
		if block.Flags&BlockExit == 0 {
			last := f.Instrs[block.InstStart+block.InstCount-1]
			successors := block.SuccCount
			switch {
			case last.Kind == wasm.InstrUnreachable:
				if successors != 0 {
					return fmt.Errorf("railssa: unreachable block %d has %d successors", id, successors)
				}
			case last.Kind == wasm.InstrIf || last.Kind == wasm.InstrBrIf || last.Kind == wasm.InstrBrOnCast || last.Kind == wasm.InstrBrOnCastFail:
				if successors == 0 || successors > 2 {
					return fmt.Errorf("railssa: conditional block %d has %d successors", id, successors)
				}
			default:
				if successors == 0 {
					return fmt.Errorf("railssa: non-exit block %d has no successor", id)
				}
			}
		}
	}
	if expectedStart != uint32(len(f.Instrs)) {
		return fmt.Errorf("railssa: CFG covers %d instructions, want %d", expectedStart, len(f.Instrs))
	}
	if len(cfg.EdgeStacks) != len(cfg.Edges) {
		return fmt.Errorf("railssa: edge stack count %d, want %d", len(cfg.EdgeStacks), len(cfg.Edges))
	}
	for i, edge := range cfg.Edges {
		if int(edge.From) >= len(cfg.Blocks) || int(edge.To) >= len(cfg.Blocks) {
			return fmt.Errorf("railssa: edge %d has invalid endpoint %d -> %d", i, edge.From, edge.To)
		}
		stack := cfg.EdgeStacks[i]
		if !stack.CarriesAll() && (stack.PrefixDepth() > f.MaxStack || uint32(stack.ResultArity()) > f.MaxStack-stack.PrefixDepth()) {
			return fmt.Errorf("railssa: edge %d has invalid stack transfer depth=%d arity=%d", i, stack.PrefixDepth(), stack.ResultArity())
		}
		from := cfg.Blocks[edge.From]
		if !slices.Contains(cfg.Succs[from.SuccStart:from.SuccStart+uint32(from.SuccCount)], edge.To) {
			return fmt.Errorf("railssa: edge %d missing from successor CSR", i)
		}
		to := cfg.Blocks[edge.To]
		if !slices.Contains(cfg.Preds[to.PredStart:to.PredStart+uint32(to.PredCount)], edge.From) {
			return fmt.Errorf("railssa: edge %d missing from predecessor CSR", i)
		}
	}
	for index, refinement := range cfg.Refinements {
		if int(refinement.Edge) >= len(cfg.Edges) || refinement.Type.Kind() != wasm.ValRef {
			return fmt.Errorf("railssa: edge refinement %d is invalid", index)
		}
		stack := cfg.EdgeStacks[refinement.Edge]
		if refinement.Slot == ^uint32(0) && !stack.CarriesAll() || refinement.Slot != ^uint32(0) && !stack.CarriesAll() && refinement.Slot >= stack.PrefixDepth()+uint32(stack.ResultArity()) {
			return fmt.Errorf("railssa: edge refinement %d slot %d is outside transfer", index, refinement.Slot)
		}
	}
	return nil
}

// Dump renders a deterministic compact CFG for replay diagnostics and tests.
func (cfg *CFG) Dump() string {
	if cfg == nil {
		return ""
	}
	var out strings.Builder
	for id, block := range cfg.Blocks {
		fmt.Fprintf(&out, "b%d [%d,%d) region=%d flags=%#x", id, block.InstStart, block.InstStart+block.InstCount, block.Region, block.Flags)
		for _, succ := range cfg.Succs[block.SuccStart : block.SuccStart+uint32(block.SuccCount)] {
			fmt.Fprintf(&out, " b%d", succ)
		}
		out.WriteByte('\n')
	}
	return out.String()
}
