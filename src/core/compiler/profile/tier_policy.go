package profile

import (
	"fmt"
	"sort"
)

// TierPolicy bounds deterministic hot-function and direct-call-cluster
// selection. ClusterDepth zero selects only hot roots; larger values include
// local direct callees up to that many edges away.
type TierPolicy struct {
	MinCount     uint64
	MaxFunctions uint32
	ClusterDepth uint8
}

// TierPlan identifies original-Wasm function indexes. Roots are the observed
// hot functions; Functions is the bounded union to compile together.
type TierPlan struct {
	Roots     []uint32
	Functions []uint32
	Truncated bool
}

// PlanTier selects local hot functions and their bounded direct-call clusters.
// directCalls is indexed by the complete original function index space and must
// contain only in-range targets. Imported functions can be observed but are not
// compilation candidates.
func PlanTier(p Module, importedFunctions uint32, directCalls [][]uint32, policy TierPolicy) (TierPlan, error) {
	if err := p.Validate(); err != nil {
		return TierPlan{}, err
	}
	if policy.MinCount == 0 {
		return TierPlan{}, fmt.Errorf("profile: tier minimum count must be positive")
	}
	if policy.MaxFunctions == 0 || policy.MaxFunctions > MaxRailshotFunctionCounters {
		return TierPlan{}, fmt.Errorf("profile: tier function limit %d is outside 1..%d", policy.MaxFunctions, MaxRailshotFunctionCounters)
	}
	if policy.ClusterDepth > 8 {
		return TierPlan{}, fmt.Errorf("profile: tier cluster depth %d exceeds limit 8", policy.ClusterDepth)
	}
	if len(directCalls) != len(p.FunctionCounts) {
		return TierPlan{}, fmt.Errorf("profile: direct-call rows %d do not match function counters %d", len(directCalls), len(p.FunctionCounts))
	}
	if importedFunctions > uint32(len(p.FunctionCounts)) {
		return TierPlan{}, fmt.Errorf("profile: imported function count %d exceeds function counters %d", importedFunctions, len(p.FunctionCounts))
	}
	for caller, targets := range directCalls {
		for _, target := range targets {
			if target >= uint32(len(directCalls)) {
				return TierPlan{}, fmt.Errorf("profile: direct call %d -> %d is out of range", caller, target)
			}
		}
	}

	type candidate struct {
		index uint32
		count uint64
	}
	candidates := make([]candidate, 0, len(p.FunctionCounts)-int(importedFunctions))
	for index := importedFunctions; index < uint32(len(p.FunctionCounts)); index++ {
		if count := p.FunctionCounts[index]; count >= policy.MinCount {
			candidates = append(candidates, candidate{index: index, count: count})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].count != candidates[j].count {
			return candidates[i].count > candidates[j].count
		}
		return candidates[i].index < candidates[j].index
	})

	limit := int(policy.MaxFunctions)
	selected := make([]bool, len(p.FunctionCounts))
	seen := make([]uint32, len(p.FunctionCounts))
	queue := make([]tierFrontier, 0, min(len(p.FunctionCounts), limit))
	clusterScratch := make([]uint32, 0, min(len(p.FunctionCounts), limit))
	var generation uint32
	plan := TierPlan{}
	for _, root := range candidates {
		if len(plan.Functions) == limit {
			plan.Truncated = true
			break
		}
		plan.Roots = append(plan.Roots, root.index)
		generation++
		cluster := tierCluster(root.index, importedFunctions, directCalls, policy.ClusterDepth, limit-len(plan.Functions), selected, seen, generation, queue, clusterScratch)
		if len(plan.Functions)+len(cluster) > limit {
			plan.Truncated = true
			cluster = cluster[:0]
			if !selected[root.index] {
				cluster = append(cluster, root.index)
			}
		}
		for _, function := range cluster {
			if selected[function] {
				continue
			}
			selected[function] = true
			plan.Functions = append(plan.Functions, function)
		}
	}
	sort.Slice(plan.Functions, func(i, j int) bool { return plan.Functions[i] < plan.Functions[j] })
	return plan, nil
}

type tierFrontier struct {
	function uint32
	depth    uint8
}

func tierCluster(root, importedFunctions uint32, directCalls [][]uint32, depth uint8, maxFunctions int, selected []bool, seen []uint32, generation uint32, queue []tierFrontier, cluster []uint32) []uint32 {
	seen[root] = generation
	queue = append(queue[:0], tierFrontier{function: root})
	cluster = cluster[:0]
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		if !selected[current.function] {
			cluster = append(cluster, current.function)
			if len(cluster) > maxFunctions {
				return cluster
			}
		}
		if current.depth == depth {
			continue
		}
		for _, target := range directCalls[current.function] {
			if target < importedFunctions || seen[target] == generation {
				continue
			}
			seen[target] = generation
			queue = append(queue, tierFrontier{function: target, depth: current.depth + 1})
		}
	}
	return cluster
}
