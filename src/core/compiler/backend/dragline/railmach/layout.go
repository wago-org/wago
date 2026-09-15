package railmach

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

// BlockLayout is a deterministic function-local ExtTSP-like chain layout. The
// edge-weight slab is aligned with Func.Edges and comes from original-Wasm
// profile edges or static weights. Order and Position are inverse permutations.
type BlockLayout struct {
	Order     []railssa.BlockID
	Position  []uint32
	HotBytes  uint64
	ColdStart uint32
	Score     uint64
}

type layoutChain struct {
	blocks []railssa.BlockID
	weight uint64
}

type weightedLayoutEdge struct {
	index  uint32
	weight uint64
}

// BuildBlockLayout greedily joins the heaviest legal tail-to-head chains, then
// keeps the entry chain first and orders remaining chains hot-first. It uses
// finalized or estimated block byte sizes and performs no control-flow rewrite.
func BuildBlockLayout(f *Func, edgeWeights []uint64, blockBytes []uint32, reuse *BlockLayout) (*BlockLayout, error) {
	if err := Verify(f); err != nil {
		return nil, err
	}
	if len(edgeWeights) != len(f.Edges) || len(blockBytes) != len(f.Blocks) {
		return nil, fmt.Errorf("railmach: layout inputs do not match %d blocks and %d edges", len(f.Blocks), len(f.Edges))
	}
	if reuse == nil {
		reuse = new(BlockLayout)
	}
	order := reuse.Order[:0]
	position := resize(reuse.Position, len(f.Blocks))
	*reuse = BlockLayout{Order: order, Position: position, ColdStart: uint32(len(f.Blocks))}
	if len(f.Blocks) == 0 {
		return reuse, nil
	}

	chains := make([]layoutChain, len(f.Blocks))
	owner := make([]uint32, len(f.Blocks))
	for block := range f.Blocks {
		chains[block] = layoutChain{blocks: []railssa.BlockID{railssa.BlockID(block)}, weight: uint64(f.Blocks[block].Weight)}
		owner[block] = uint32(block)
	}
	edges := make([]weightedLayoutEdge, len(f.Edges))
	for index, weight := range edgeWeights {
		edges[index] = weightedLayoutEdge{index: uint32(index), weight: weight}
	}
	slices.SortFunc(edges, func(a, b weightedLayoutEdge) int {
		if a.weight != b.weight {
			if a.weight > b.weight {
				return -1
			}
			return 1
		}
		aEdge, bEdge := f.Edges[a.index], f.Edges[b.index]
		if aEdge.From != bEdge.From {
			return int(aEdge.From) - int(bEdge.From)
		}
		return int(aEdge.To) - int(bEdge.To)
	})
	for _, candidate := range edges {
		if candidate.weight == 0 {
			break
		}
		edge := f.Edges[candidate.index]
		fromOwner, toOwner := owner[edge.From], owner[edge.To]
		if fromOwner == toOwner || len(chains[fromOwner].blocks) == 0 || len(chains[toOwner].blocks) == 0 {
			continue
		}
		from, to := &chains[fromOwner], &chains[toOwner]
		if from.blocks[len(from.blocks)-1] != edge.From || to.blocks[0] != edge.To || edge.To == 0 {
			continue
		}
		from.blocks = append(from.blocks, to.blocks...)
		from.weight = saturatingAdd(from.weight, to.weight)
		for _, block := range to.blocks {
			owner[block] = fromOwner
		}
		to.blocks = to.blocks[:0]
	}

	chainOrder := make([]uint32, 0, len(chains))
	entryOwner := owner[0]
	chainOrder = append(chainOrder, entryOwner)
	for index, chain := range chains {
		if uint32(index) != entryOwner && len(chain.blocks) != 0 {
			chainOrder = append(chainOrder, uint32(index))
		}
	}
	slices.SortStableFunc(chainOrder[1:], func(a, b uint32) int {
		if chains[a].weight != chains[b].weight {
			if chains[a].weight > chains[b].weight {
				return -1
			}
			return 1
		}
		return int(chains[a].blocks[0]) - int(chains[b].blocks[0])
	})
	for _, chainID := range chainOrder {
		reuse.Order = append(reuse.Order, chains[chainID].blocks...)
	}
	for position, block := range reuse.Order {
		reuse.Position[block] = uint32(position)
		if f.Blocks[block].Weight != 0 {
			reuse.HotBytes = saturatingAdd(reuse.HotBytes, uint64(blockBytes[block]))
		} else if reuse.ColdStart == uint32(len(f.Blocks)) {
			reuse.ColdStart = uint32(position)
		}
	}
	for edgeIndex, edge := range f.Edges {
		if reuse.Position[edge.To] == reuse.Position[edge.From]+1 {
			reuse.Score = saturatingAdd(reuse.Score, edgeWeights[edgeIndex])
		}
	}
	if err := VerifyBlockLayout(f, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func VerifyBlockLayout(f *Func, layout *BlockLayout) error {
	if layout == nil || len(layout.Order) != len(f.Blocks) || len(layout.Position) != len(f.Blocks) || layout.ColdStart > uint32(len(f.Blocks)) {
		return fmt.Errorf("railmach: malformed block layout")
	}
	seen := make([]bool, len(f.Blocks))
	for position, block := range layout.Order {
		if int(block) >= len(f.Blocks) || seen[block] || layout.Position[block] != uint32(position) {
			return fmt.Errorf("railmach: invalid block layout entry %d at %d", block, position)
		}
		seen[block] = true
	}
	if len(layout.Order) != 0 && layout.Order[0] != 0 {
		return fmt.Errorf("railmach: entry block is not first")
	}
	return nil
}

func saturatingAdd(a, b uint64) uint64 {
	if ^uint64(0)-a < b {
		return ^uint64(0)
	}
	return a + b
}
