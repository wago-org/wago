package shared

import (
	"fmt"
	"github.com/wago-org/wago/internal/jitprofile"
)

// RemapNativeUnwind projects recovery rules through the same deletion map as
// native bytes. A rule changes after its stack-adjust instruction, so projection
// of that instruction's end preserves the exact asynchronous transition point.
func RemapNativeUnwind(ranges []jitprofile.UnwindRange, mapper sourceRangeMapper) ([]jitprofile.UnwindRange, error) {
	if err := jitprofile.ValidateUnwind(ranges, ^uint64(0)); err != nil {
		return nil, err
	}
	out := make([]jitprofile.UnwindRange, 0, len(ranges))
	for i, r := range ranges {
		end := r.Offset + r.Size
		if uint64(int(r.Offset)) != r.Offset || uint64(int(end)) != end || int(end) < 0 {
			return nil, fmt.Errorf("native unwind range %d exceeds compiler offset domain", i)
		}
		start, finish, ok := mapper.MapRange(int(r.Offset), int(end))
		if !ok || start < 0 || finish < start {
			return nil, fmt.Errorf("unmappable native unwind range %d", i)
		}
		if start == finish {
			continue
		}
		r.Offset, r.Size = uint64(start), uint64(finish-start)
		if len(out) != 0 {
			last := &out[len(out)-1]
			if last.Offset+last.Size > r.Offset {
				return nil, fmt.Errorf("overlapping remapped native unwind range %d", i)
			}
			if last.Offset+last.Size == r.Offset && last.CFARegister == r.CFARegister && last.CFAOffset == r.CFAOffset && last.ReturnOffset == r.ReturnOffset {
				last.Size += r.Size
				continue
			}
		}
		out = append(out, r)
	}
	return out, nil
}
