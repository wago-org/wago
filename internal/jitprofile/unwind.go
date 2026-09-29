package jitprofile

import (
	"fmt"
	"sort"
)

// UnwindRange describes stack-based recovery at each instruction in a sparse
// final native range. CFA = register[CFARegister] + CFAOffset, caller SP = CFA,
// and the return PC is loaded from CFA + ReturnOffset. Register numbers use the
// target's DWARF numbering. All other caller registers are unknown, not preserved.
// This representation deliberately does not cover register-held return addresses,
// expression-based CFAs, or arbitrary host transitions. Absent ranges are unknown.
type UnwindRange struct {
	Offset       uint64 `json:"offset"`
	Size         uint64 `json:"size"`
	CFARegister  uint16 `json:"cfa_register"`
	CFAOffset    int64  `json:"cfa_offset"`
	ReturnOffset int64  `json:"return_offset"`
}

func (s *Session) IncludeUnwind() bool { return s != nil && s.opts.UnwindMaps }

// ValidateUnwind checks directory framing, not the compiler's recovery proof.
func ValidateUnwind(ranges []UnwindRange, size uint64) error {
	var end uint64
	for i, r := range ranges {
		if r.Size == 0 || r.Offset < end || r.Offset > size || r.Size > size-r.Offset || r.CFARegister > 255 || r.CFAOffset <= 0 || r.ReturnOffset >= 0 || r.ReturnOffset < -r.CFAOffset {
			return fmt.Errorf("invalid native unwind range %d", i)
		}
		end = r.Offset + r.Size
	}
	return nil
}

// LookupUnwind never extends a rule across an unknown gap or an end boundary.
func LookupUnwind(ranges []UnwindRange, offset uint64) (UnwindRange, bool) {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].Offset > offset }) - 1
	if i < 0 {
		return UnwindRange{}, false
	}
	r := ranges[i]
	return r, offset-r.Offset < r.Size
}
