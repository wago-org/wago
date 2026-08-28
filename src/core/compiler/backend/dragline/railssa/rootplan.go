package railssa

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const maxRootLivenessWords = 1 << 20

// RootSiteRange names the exact roots visible while one source instruction may
// collect. Roots live in RootPlan.Roots[Start:Start+Count].
type RootSiteRange struct {
	Source uint32
	Start  uint32
	Count  uint16
}

// RootUse maps one typed SSA identity to its reusable native root slot.
type RootUse struct {
	Value FlowValueID
	Slot  uint16
	Flags uint8
	_     uint8
}

const RootUseReload uint8 = 1 << iota

// RootPlan is the target-neutral root contract between RailSSA liveness and
// target frame composition. Only values live at a collecting instruction are
// listed; disjoint identities reuse slots.
type RootPlan struct {
	SlotCount uint16
	Sites     []RootSiteRange
	Roots     []RootUse
}

func (p *RootPlan) RootsAtSource(source uint32) []RootUse {
	if p == nil {
		return nil
	}
	index, ok := slices.BinarySearchFunc(p.Sites, source, func(site RootSiteRange, source uint32) int {
		switch {
		case site.Source < source:
			return -1
		case site.Source > source:
			return 1
		default:
			return 0
		}
	})
	if !ok {
		return nil
	}
	site := p.Sites[index]
	return p.Roots[site.Start : site.Start+uint32(site.Count)]
}

// BuildRootPlan derives exact collector-reference liveness at collecting
// instructions and colors the resulting interference graph into bounded frame
// slots. The plan contains SSA identities, never architecture frame offsets.
func BuildRootPlan(m *wasm.Module, f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata) (*RootPlan, error) {
	plan, err := deriveRootPlan(m, f, cfg, flow, semantic, metadata)
	if err != nil {
		return nil, err
	}
	if err := VerifyRootPlan(m, f, cfg, flow, semantic, metadata, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// VerifyRootPlan independently rebuilds source liveness and rejects missing,
// extra, reordered, or incorrectly colored roots.
func VerifyRootPlan(m *wasm.Module, f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, plan *RootPlan) error {
	if plan == nil {
		return fmt.Errorf("railssa: root plan is nil")
	}
	want, err := deriveRootPlan(m, f, cfg, flow, semantic, metadata)
	if err != nil {
		return err
	}
	if plan.SlotCount != want.SlotCount || !slices.Equal(plan.Sites, want.Sites) || !slices.Equal(plan.Roots, want.Roots) {
		return fmt.Errorf("railssa: root plan disagrees with exact typed liveness")
	}
	return nil
}

type rootSiteBits struct {
	source     uint32
	bits       []uint64
	reloadBits []uint64
}

func deriveRootPlan(m *wasm.Module, f *StackFunc, cfg *CFG, flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata) (*RootPlan, error) {
	if f == nil || cfg == nil || flow == nil || semantic == nil || metadata == nil || len(metadata.Instructions) != len(f.Instrs) || len(semantic.Blocks) != len(cfg.Blocks) {
		return nil, fmt.Errorf("railssa: root planning requires a complete typed function")
	}
	rootIndex := make([]uint32, len(flow.Values))
	rootValues := make([]FlowValueID, 0)
	for value := FlowValueID(1); int(value) < len(flow.Values); value++ {
		if codegen.IsCollectorReferenceType(m, flow.Values[value].Type) {
			rootIndex[value] = uint32(len(rootValues)) + 1
			rootValues = append(rootValues, value)
		}
	}
	if len(rootValues) == 0 {
		return &RootPlan{}, nil
	}
	words := (len(rootValues) + 63) / 64
	collectingSites := uint64(0)
	for _, instruction := range semantic.Insts {
		if metadata.Instructions[instruction.Source].Flags&EffectMayCollect != 0 {
			collectingSites++
		}
	}
	dataflowWords := uint64(len(cfg.Blocks))*uint64(words)*5 + collectingSites*uint64(words)*2
	if dataflowWords > maxRootLivenessWords {
		return nil, &BudgetError{Resource: "collector-reference liveness words", Required: dataflowWords, Limit: maxRootLivenessWords}
	}
	makeMatrix := func() []uint64 { return make([]uint64, len(cfg.Blocks)*words) }
	uses, defs, edgeUses := makeMatrix(), makeMatrix(), makeMatrix()
	liveIn, liveOut := makeMatrix(), makeMatrix()
	blockWords := func(matrix []uint64, block BlockID) []uint64 {
		start := int(block) * words
		return matrix[start : start+words]
	}
	bitFor := func(value FlowValueID) (int, uint64, bool) {
		if int(value) >= len(rootIndex) || rootIndex[value] == 0 {
			return 0, 0, false
		}
		index := int(rootIndex[value] - 1)
		return index / 64, uint64(1) << uint(index%64), true
	}
	setValue := func(bits []uint64, value FlowValueID) {
		if word, mask, ok := bitFor(value); ok {
			bits[word] |= mask
		}
	}
	for value := FlowValueID(1); int(value) < len(flow.Values); value++ {
		if rootIndex[value] == 0 {
			continue
		}
		definition := flow.Values[value]
		switch definition.Kind {
		case FlowValueInitialLocal:
			setValue(blockWords(defs, 0), value)
		case FlowValueBlockParam:
			if int(definition.Block) < len(cfg.Blocks) {
				setValue(blockWords(defs, definition.Block), value)
			}
		}
	}
	for blockIndex, semanticBlock := range semantic.Blocks {
		block := BlockID(blockIndex)
		use, def := blockWords(uses, block), blockWords(defs, block)
		// The synthetic exit block has no semantic return instruction. Its entry
		// stack is nevertheless an observable function result use.
		if cfg.Blocks[block].Flags&BlockExit != 0 {
			for _, value := range flow.BlockEntry(block) {
				if word, mask, ok := bitFor(value); ok && def[word]&mask == 0 {
					use[word] |= mask
				}
			}
		}
		for id := semanticBlock.InstStart; id < semanticBlock.InstStart+semanticBlock.InstCount; id++ {
			instruction := semantic.Insts[id]
			for _, value := range semantic.Operands(id) {
				if word, mask, ok := bitFor(value); ok && def[word]&mask == 0 {
					use[word] |= mask
				}
			}
			for result := uint32(0); result < instruction.ResultCount(); result++ {
				setValue(def, instruction.Result+FlowValueID(result))
			}
		}
	}
	for _, argument := range flow.EdgeArgs {
		if int(argument.Edge) >= len(cfg.Edges) {
			return nil, fmt.Errorf("railssa: root plan edge argument %d is out of range", argument.Edge)
		}
		setValue(blockWords(edgeUses, cfg.Edges[argument.Edge].From), argument.Argument)
	}
	for pass := 0; pass <= len(cfg.Blocks)*2; pass++ {
		changed := false
		for blockIndex := len(cfg.Blocks) - 1; blockIndex >= 0; blockIndex-- {
			block := BlockID(blockIndex)
			out := blockWords(liveOut, block)
			record := cfg.Blocks[block]
			in, use, def := blockWords(liveIn, block), blockWords(uses, block), blockWords(defs, block)
			for word := range out {
				nextOut := blockWords(edgeUses, block)[word]
				for _, successor := range cfg.Succs[record.SuccStart : uint32(record.SuccStart)+uint32(record.SuccCount)] {
					nextOut |= blockWords(liveIn, successor)[word]
				}
				if out[word] != nextOut {
					out[word] = nextOut
					changed = true
				}
				nextIn := use[word] | nextOut&^def[word]
				if in[word] != nextIn {
					in[word] = nextIn
					changed = true
				}
			}
		}
		if !changed {
			break
		}
		if pass == len(cfg.Blocks)*2 {
			return nil, fmt.Errorf("railssa: root liveness did not converge after %d passes", pass+1)
		}
	}
	sites := make([]rootSiteBits, 0)
	for blockIndex, semanticBlock := range semantic.Blocks {
		block := BlockID(blockIndex)
		if !flow.Reachable[block] {
			continue
		}
		live := append([]uint64(nil), blockWords(liveOut, block)...)
		for id := semanticBlock.InstStart + semanticBlock.InstCount; id > semanticBlock.InstStart; id-- {
			instruction := semantic.Insts[id-1]
			for result := uint32(0); result < instruction.ResultCount(); result++ {
				if word, mask, ok := bitFor(instruction.Result + FlowValueID(result)); ok {
					live[word] &^= mask
				}
			}
			if metadata.Instructions[instruction.Source].Flags&EffectMayCollect != 0 {
				reload := append([]uint64(nil), live...)
				atSite := append([]uint64(nil), reload...)
				for _, value := range semantic.Operands(id - 1) {
					setValue(atSite, value)
				}
				sites = append(sites, rootSiteBits{source: instruction.Source, bits: atSite, reloadBits: reload})
			}
			for _, value := range semantic.Operands(id - 1) {
				setValue(live, value)
			}
		}
	}
	slices.SortFunc(sites, func(a, b rootSiteBits) int { return int(a.source) - int(b.source) })
	return colorRootSites(rootValues, sites)
}

func colorRootSites(values []FlowValueID, sites []rootSiteBits) (*RootPlan, error) {
	counts := make([]uint32, len(values)+1)
	total := 0
	for _, site := range sites {
		for index := range values {
			if site.bits[index/64]&(uint64(1)<<uint(index%64)) != 0 {
				counts[index+1]++
				total++
			}
		}
	}
	if total > maxStackSSACells {
		return nil, &BudgetError{Resource: "collector-reference safepoint uses", Required: uint64(total), Limit: maxStackSSACells}
	}
	for index := 1; index < len(counts); index++ {
		counts[index] += counts[index-1]
	}
	occurrences := make([]uint32, total)
	cursors := append([]uint32(nil), counts[:len(counts)-1]...)
	for siteIndex, site := range sites {
		for index := range values {
			if site.bits[index/64]&(uint64(1)<<uint(index%64)) != 0 {
				occurrences[cursors[index]] = uint32(siteIndex)
				cursors[index]++
			}
		}
	}
	assigned := make([]uint16, len(values))
	assignedOK := make([]bool, len(values))
	forbidden := make([]bool, codegen.GCFrameRootLimit)
	slotCount := 0
	for valueIndex := range values {
		if counts[valueIndex] == counts[valueIndex+1] {
			continue
		}
		clear(forbidden)
		for _, siteIndex := range occurrences[counts[valueIndex]:counts[valueIndex+1]] {
			site := sites[siteIndex]
			for other := 0; other < valueIndex; other++ {
				if assignedOK[other] && site.bits[other/64]&(uint64(1)<<uint(other%64)) != 0 {
					forbidden[assigned[other]] = true
				}
			}
		}
		slot := 0
		for slot < slotCount && forbidden[slot] {
			slot++
		}
		if slot == codegen.GCFrameRootLimit {
			return nil, &BudgetError{Resource: "simultaneous collector roots", Required: uint64(slot + 1), Limit: codegen.GCFrameRootLimit}
		}
		if slot == slotCount {
			slotCount++
		}
		assigned[valueIndex], assignedOK[valueIndex] = uint16(slot), true
	}
	plan := &RootPlan{SlotCount: uint16(slotCount), Sites: make([]RootSiteRange, 0, len(sites)), Roots: make([]RootUse, 0, total)}
	for _, site := range sites {
		start := uint32(len(plan.Roots))
		for index, value := range values {
			if site.bits[index/64]&(uint64(1)<<uint(index%64)) != 0 {
				flags := uint8(0)
				if site.reloadBits[index/64]&(uint64(1)<<uint(index%64)) != 0 {
					flags |= RootUseReload
				}
				plan.Roots = append(plan.Roots, RootUse{Value: value, Slot: assigned[index], Flags: flags})
			}
		}
		slices.SortFunc(plan.Roots[start:], func(a, b RootUse) int {
			if a.Slot < b.Slot {
				return -1
			}
			if a.Slot > b.Slot {
				return 1
			}
			if a.Value < b.Value {
				return -1
			}
			if a.Value > b.Value {
				return 1
			}
			return 0
		})
		count := len(plan.Roots) - int(start)
		plan.Sites = append(plan.Sites, RootSiteRange{Source: site.source, Start: start, Count: uint16(count)})
	}
	return plan, nil
}
