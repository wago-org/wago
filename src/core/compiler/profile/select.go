package profile

import (
	"fmt"
	"slices"
)

// SelectionPolicy bounds profile-guided compilation or native cloning. A zero
// limit is unbounded. MinimumGainPermille is the required projected speed gain
// in tenths of a percent; 25 means 2.5 percent.
type SelectionPolicy struct {
	MaxFunctions        uint32
	MaxBodyBytes        uint64
	MinimumCount        uint64
	MinimumGainPermille uint16
}

// FunctionOpportunity contains static facts unavailable in an execution
// profile. BodyBytes prices compile and code-memory work; GainPermille and
// NativeOpportunity prevent cloning merely because a function is hot.
type FunctionOpportunity struct {
	BodyBytes         uint32
	GainPermille      uint16
	NativeOpportunity bool
}

type SelectedFunction struct {
	Function     uint32
	Count        uint64
	BodyBytes    uint32
	GainPermille uint16
}

// SelectFunctions returns a deterministic hot-first subset. It consumes only
// original-Wasm function counts and explicit static opportunities, and uses
// saturating arithmetic when ranking count times projected gain.
func SelectFunctions(p Module, opportunities []FunctionOpportunity, policy SelectionPolicy) ([]SelectedFunction, error) {
	if len(p.FunctionCounts) > len(opportunities) {
		return nil, fmt.Errorf("profile: %d function counts exceed %d opportunities", len(p.FunctionCounts), len(opportunities))
	}
	candidates := make([]SelectedFunction, 0, len(p.FunctionCounts))
	for function, count := range p.FunctionCounts {
		opportunity := opportunities[function]
		if count < policy.MinimumCount || !opportunity.NativeOpportunity || opportunity.GainPermille < policy.MinimumGainPermille {
			continue
		}
		candidates = append(candidates, SelectedFunction{
			Function: uint32(function), Count: count, BodyBytes: opportunity.BodyBytes, GainPermille: opportunity.GainPermille,
		})
	}
	slices.SortFunc(candidates, func(a, b SelectedFunction) int {
		aScore, bScore := saturatingProduct(a.Count, uint64(a.GainPermille)), saturatingProduct(b.Count, uint64(b.GainPermille))
		if aScore != bScore {
			if aScore > bScore {
				return -1
			}
			return 1
		}
		if a.BodyBytes != b.BodyBytes {
			if a.BodyBytes < b.BodyBytes {
				return -1
			}
			return 1
		}
		return int(a.Function) - int(b.Function)
	})
	selected := candidates[:0]
	var bytes uint64
	for _, candidate := range candidates {
		if policy.MaxFunctions != 0 && uint32(len(selected)) >= policy.MaxFunctions {
			break
		}
		next := bytes + uint64(candidate.BodyBytes)
		if next < bytes {
			next = ^uint64(0)
		}
		if policy.MaxBodyBytes != 0 && next > policy.MaxBodyBytes {
			continue
		}
		selected = append(selected, candidate)
		bytes = next
	}
	return selected, nil
}

func saturatingProduct(a, b uint64) uint64 {
	if a != 0 && b > ^uint64(0)/a {
		return ^uint64(0)
	}
	return a * b
}
