package main

import (
	"fmt"
	"strings"
)

const maxRawRegions = 128
const maxNativeBytes = 64 << 10

// RawRegion is opaque byte ownership, never an instruction or Wasm source map.
// Function -1 denotes module/shared ownership. Anchors are within the owner.
type RawRegion struct {
	ID          string `json:"id"`
	Owner       string `json:"owner"`
	Kind        string `json:"kind"`
	Function    int    `json:"function"`
	LeftAnchor  string `json:"left_anchor"`
	RightAnchor string `json:"right_anchor"`
	Offset      uint64 `json:"offset"`
	Hex         string `json:"hex"`
}
type RawChange struct {
	Category string    `json:"category"`
	Before   RawRegion `json:"before"`
	After    RawRegion `json:"after"`
}

func regionEnd(r Region) uint64 {
	in := r.Instructions[len(r.Instructions)-1]
	return in.Offset + uint64(len(in.Hex)/2)
}
func validateRaw(s Snapshot) error {
	if len(s.RawRegions) > maxRawRegions {
		return fmt.Errorf("raw region budget")
	}
	if s.Capture == nil || !s.Capture.RawCoverage {
		if len(s.RawRegions) != 0 {
			return fmt.Errorf("raw coverage metadata missing")
		}
		return nil
	}
	if s.Capture.NativeBytes > maxNativeBytes {
		return fmt.Errorf("raw native byte budget")
	}
	var sum, end uint64
	ids := make(map[string]bool, len(s.RawRegions))
	anchors := make(map[string]int, len(s.Regions))
	for i, r := range s.Regions {
		anchors[r.ID] = i
	}
	for i, r := range s.RawRegions {
		if r.ID == "" || len(r.ID) > 128 || ids[r.ID] || r.Owner == "" || len(r.Owner) > 128 || r.Kind == "" || len(r.Kind) > 64 || r.Function < -1 || int64(r.Function) > int64(^uint32(0)) {
			return fmt.Errorf("invalid raw ownership identity")
		}
		ids[r.ID] = true
		if r.LeftAnchor == "" || len(r.LeftAnchor) > 128 || r.RightAnchor == "" || len(r.RightAnchor) > 128 {
			return fmt.Errorf("invalid raw anchors")
		}
		n := uint64(len(r.Hex) / 2)
		if n == 0 || len(r.Hex)%2 != 0 || r.Offset > s.Capture.NativeBytes || n > s.Capture.NativeBytes-r.Offset || i > 0 && r.Offset < end {
			return fmt.Errorf("invalid raw byte range")
		}
		for j := 0; j < len(r.Hex); j++ {
			c := r.Hex[j]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return fmt.Errorf("invalid raw hex")
			}
		}
		if r.LeftAnchor != "owner-start" {
			j, ok := anchors[r.LeftAnchor]
			if !ok || r.Function < 0 || s.Regions[j].Function != uint32(r.Function) || regionEnd(s.Regions[j]) != r.Offset {
				return fmt.Errorf("invalid raw left source anchor")
			}
		}
		if r.RightAnchor != "owner-end" {
			j, ok := anchors[r.RightAnchor]
			if !ok || r.Function < 0 || s.Regions[j].Function != uint32(r.Function) || s.Regions[j].Instructions[0].Offset != r.Offset+n {
				return fmt.Errorf("invalid raw right source anchor")
			}
		}
		sum += n
		end = r.Offset + n
	}
	if sum != s.Capture.UnmappedBytes {
		return fmt.Errorf("raw bytes disagree with unmapped count")
	}
	// Merge two ordered lists without allocating a combined interval list.
	mi, ri := 0, 0
	end = 0
	for mi < len(s.Regions) || ri < len(s.RawRegions) {
		var start, next uint64
		if ri == len(s.RawRegions) || mi < len(s.Regions) && s.Regions[mi].Instructions[0].Offset <= s.RawRegions[ri].Offset {
			r := s.Regions[mi]
			start = r.Instructions[0].Offset
			next = regionEnd(r)
			mi++
		} else {
			r := s.RawRegions[ri]
			start = r.Offset
			next = start + uint64(len(r.Hex)/2)
			ri++
		}
		if start != end {
			return fmt.Errorf("mapped/raw partition has gap or overlap")
		}
		end = next
	}
	if end != s.Capture.NativeBytes {
		return fmt.Errorf("mapped/raw partition incomplete")
	}
	return nil
}
func compareRaw(a, b Snapshot, out *Report) {
	hasCoverage := a.Capture != nil && b.Capture != nil && a.Capture.RawCoverage && b.Capture.RawCoverage
	out.RawComplete = out.Complete && hasCoverage
	if !hasCoverage {
		return
	}
	if len(a.RawRegions) != len(b.RawRegions) {
		out.RawComplete = false
		return
	}
	for i, ar := range a.RawRegions {
		br := b.RawRegions[i]
		if ar.ID != br.ID || ar.Owner != br.Owner || ar.Kind != br.Kind || ar.Function != br.Function || ar.LeftAnchor != br.LeftAnchor || ar.RightAnchor != br.RightAnchor {
			out.RawComplete = false
			continue
		}
		out.RawCompared++
		if !strings.EqualFold(ar.Hex, br.Hex) {
			if len(out.RawChanges) == maxChanges {
				out.Limit = true
				out.RawComplete = false
				out.Reasons = append(out.Reasons, "raw change budget")
				return
			}
			out.RawChanges = append(out.RawChanges, RawChange{"raw-bytes", ar, br})
		}
	}
}
