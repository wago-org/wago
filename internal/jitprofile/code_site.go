package jitprofile

import (
	"fmt"
	"sort"
)

// CodeSite identifies a compiler-authored operation in finalized native bytes.
// It is static evidence about emitted instructions, not an execution count or
// an estimate of time saved. Gaps carry no inferred compiler explanation.
type CodeSite struct {
	Offset uint64 `json:"offset"`
	Size   uint64 `json:"size"`
	Kind   string `json:"kind"`
}

func ValidateCodeSites(sites []CodeSite, size uint64) error {
	var end uint64
	for i, site := range sites {
		if site.Kind == "" || site.Size == 0 || site.Offset < end || site.Offset > size || site.Size > size-site.Offset {
			return fmt.Errorf("invalid native compiler site %d", i)
		}
		end = site.Offset + site.Size
	}
	return nil
}

func LookupCodeSite(sites []CodeSite, offset uint64) (CodeSite, bool) {
	i := sort.Search(len(sites), func(i int) bool { return sites[i].Offset > offset }) - 1
	if i < 0 {
		return CodeSite{}, false
	}
	site := sites[i]
	return site, offset-site.Offset < site.Size
}

// ValidateCodeSiteRegions requires each operation to stay within one executable
// owner. Otherwise a site could accidentally explain bytes in another function.
func ValidateCodeSiteRegions(sites []CodeSite, regions []Region) error {
	at := 0
	for i, site := range sites {
		for at < len(regions) && regions[at].Offset+regions[at].Size <= site.Offset {
			at++
		}
		if at == len(regions) {
			return fmt.Errorf("native compiler site %d has no region", i)
		}
		r := regions[at]
		if site.Offset < r.Offset || site.Size > r.Size-(site.Offset-r.Offset) || r.Kind == "padding" || r.Kind == "literal-data" || r.Kind == "unknown" {
			return fmt.Errorf("native compiler site %d crosses an owner or covers non-executable bytes", i)
		}
	}
	return nil
}
