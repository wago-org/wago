package profilecmd

import (
	"fmt"
	"sort"

	"github.com/wago-org/wago/profile"
)

type siteSamples struct {
	Category string `json:"category"`
	Kind     string `json:"kind,omitempty"`
	Samples  uint64 `json:"samples"`
	Weight   uint64 `json:"weight"`
}

// Sparse compiler labels explain only the PCs they cover. Other guest samples
// and non-guest/unknown samples remain distinct, with no inferred explanation.
// The summary uses the full report before the display's function-row limit.
func summarizeSites(r profile.Report) ([]siteSamples, error) {
	byKind := make(map[string]siteSamples)
	unclassified := siteSamples{Category: "unclassified-guest"}
	remainingSamples, remainingWeight := r.Samples, r.Weight
	if r.UnknownSamples > remainingSamples || r.UnknownWeight > remainingWeight {
		return nil, fmt.Errorf("inconsistent unknown sample totals")
	}
	remainingSamples -= r.UnknownSamples
	remainingWeight -= r.UnknownWeight
	for _, row := range r.Rows {
		for _, pc := range row.PCs {
			if pc.Samples > remainingSamples || pc.Weight > remainingWeight {
				return nil, fmt.Errorf("compiler-site samples exceed report totals")
			}
			remainingSamples -= pc.Samples
			remainingWeight -= pc.Weight
			if pc.CompilerSite == nil {
				unclassified.Samples += pc.Samples
				unclassified.Weight += pc.Weight
				continue
			}
			kind := pc.CompilerSite.Kind
			v := byKind[kind]
			v.Category, v.Kind = "compiler-site", kind
			v.Samples += pc.Samples
			v.Weight += pc.Weight
			byKind[kind] = v
		}
	}
	// Older reports can omit per-PC detail. Those samples have no site evidence.
	unclassified.Samples += remainingSamples
	unclassified.Weight += remainingWeight
	out := make([]siteSamples, 0, len(byKind)+2)
	for _, v := range byKind {
		out = append(out, v)
	}
	if unclassified.Samples != 0 || unclassified.Weight != 0 {
		out = append(out, unclassified)
	}
	if r.UnknownSamples != 0 || r.UnknownWeight != 0 {
		out = append(out, siteSamples{Category: "unknown-or-non-guest", Samples: r.UnknownSamples, Weight: r.UnknownWeight})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}
