package profile

import (
	"fmt"
	"slices"
)

// PlanNativeClones selects profile-hot roots with an explicit native
// opportunity and then closes each admitted root transitively over local direct
// calls. The complete clone remains bounded by both function count and original
// body bytes; a root whose closure does not fit is skipped atomically.
func PlanNativeClones(p Module, importedFunctions uint32, directCalls [][]uint32, opportunities []FunctionOpportunity, policy SelectionPolicy) (TierPlan, error) {
	if err := p.Validate(); err != nil {
		return TierPlan{}, err
	}
	if policy.MaxFunctions == 0 || policy.MaxBodyBytes == 0 || policy.MinimumCount == 0 || policy.MinimumGainPermille == 0 {
		return TierPlan{}, fmt.Errorf("profile: native clone policy requires positive function, byte, count, and gain bounds")
	}
	if policy.MaxFunctions > MaxRailshotFunctionCounters {
		return TierPlan{}, fmt.Errorf("profile: native clone function limit %d exceeds %d", policy.MaxFunctions, MaxRailshotFunctionCounters)
	}
	if len(directCalls) != len(p.FunctionCounts) || len(opportunities) != len(p.FunctionCounts) {
		return TierPlan{}, fmt.Errorf("profile: native clone rows calls=%d opportunities=%d counts=%d", len(directCalls), len(opportunities), len(p.FunctionCounts))
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
	rankingPolicy := policy
	rankingPolicy.MaxFunctions = 0
	rankingPolicy.MaxBodyBytes = 0
	ranked, err := SelectFunctions(p, opportunities, rankingPolicy)
	if err != nil {
		return TierPlan{}, err
	}
	selected := make([]bool, len(p.FunctionCounts))
	seen := make([]uint32, len(p.FunctionCounts))
	stack := make([]uint32, 0, min(len(p.FunctionCounts), int(policy.MaxFunctions)))
	closure := make([]uint32, 0, cap(stack))
	var generation uint32
	var selectedBytes uint64
	plan := TierPlan{}
	for _, candidate := range ranked {
		if candidate.Function < importedFunctions || selected[candidate.Function] {
			continue
		}
		generation++
		stack = append(stack[:0], candidate.Function)
		closure = closure[:0]
		additionalBytes := uint64(0)
		tooLarge := false
		for len(stack) != 0 {
			last := len(stack) - 1
			function := stack[last]
			stack = stack[:last]
			if function < importedFunctions || selected[function] || seen[function] == generation {
				continue
			}
			seen[function] = generation
			closure = append(closure, function)
			additionalBytes = saturatingSum(additionalBytes, uint64(opportunities[function].BodyBytes))
			if uint64(len(plan.Functions)+len(closure)) > uint64(policy.MaxFunctions) || saturatingSum(selectedBytes, additionalBytes) > policy.MaxBodyBytes {
				tooLarge = true
				break
			}
			for _, target := range directCalls[function] {
				if target >= importedFunctions && !selected[target] && seen[target] != generation {
					stack = append(stack, target)
				}
			}
		}
		if tooLarge {
			plan.Truncated = true
			continue
		}
		plan.Roots = append(plan.Roots, candidate.Function)
		for _, function := range closure {
			if !selected[function] {
				selected[function] = true
				plan.Functions = append(plan.Functions, function)
			}
		}
		selectedBytes += additionalBytes
	}
	slices.Sort(plan.Functions)
	return plan, nil
}

func saturatingSum(a, b uint64) uint64 {
	if b > ^uint64(0)-a {
		return ^uint64(0)
	}
	return a + b
}
