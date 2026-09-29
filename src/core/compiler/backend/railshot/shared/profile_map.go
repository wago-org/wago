package shared

import (
	"fmt"
	"github.com/wago-org/wago/internal/jitprofile"
)

// NativeSourceRange relates final executable bytes to a compiler-recorded Wasm
// location. WasmOffset is relative to the function's local declarations; Function
// is the full module function index including imports. Gaps are explicitly
// unmapped. This is not an asynchronous unwind description.
type NativeSourceRange = jitprofile.SourceRange
type NativeInlineFrame = jitprofile.InlineFrame

// MapRange projects a half-open range through native compaction. Unlike Map,
// range boundaries may lie inside deleted bytes: they collapse to the deletion
// boundary. A zero-length result means no bytes from the range survived. The
// point-mapping contract remains strict for relocations and branch targets.
func (m *OffsetMap) MapRange(start, end int) (int, int, bool) {
	return mapNativeRange(m.oldLen, m.deletionOff[:m.deletionN], m.deleted[:m.deletionN], start, end)
}
func (m *WideOffsetMap) MapRange(start, end int) (int, int, bool) {
	return mapNativeRange(m.oldLen, m.deletionOff[:m.deletionN], m.deleted[:m.deletionN], start, end)
}
func mapNativeRange(oldLen uint32, offsets, deleted []uint32, start, end int) (int, int, bool) {
	if start < 0 || end < start || uint64(end) > uint64(oldLen) {
		return 0, 0, false
	}
	return mapNativeBoundary(offsets, deleted, start), mapNativeBoundary(offsets, deleted, end), true
}
func mapNativeBoundary(offsets, deleted []uint32, off int) int {
	lo, hi := 0, len(offsets)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if int(offsets[mid]) <= off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	i := lo - 1
	if i < 0 {
		return off
	}
	previous := uint32(0)
	if i > 0 {
		previous = deleted[i-1]
	}
	end := uint64(offsets[i]) + uint64(deleted[i]-previous)
	if uint64(off) < end {
		return int(offsets[i] - previous)
	}
	return off - int(deleted[i])
}

type sourceRangeMapper interface {
	MapRange(int, int) (int, int, bool)
}

// RemapNativeSources returns a new ordered directory after a deletion-only
// finalizer pass. It drops vanished ranges and coalesces adjacent surviving
// ranges only when their source identities match. It never fills unknown gaps.
func RemapNativeSources(source []NativeSourceRange, mapper sourceRangeMapper) ([]NativeSourceRange, error) {
	out := make([]NativeSourceRange, 0, len(source))
	var previousEnd uint64
	for i, r := range source {
		end := r.Offset + r.Size
		if r.Size == 0 || end < r.Offset || i > 0 && r.Offset < previousEnd || uint64(int(r.Offset)) != r.Offset || uint64(int(end)) != end || int(end) < 0 {
			return nil, fmt.Errorf("invalid native source range %d", i)
		}
		previousEnd = end
		start, finish, ok := mapper.MapRange(int(r.Offset), int(end))
		if !ok || start < 0 || finish < start {
			return nil, fmt.Errorf("unmappable native source range %d", i)
		}
		if start == finish {
			continue
		}
		r.Offset, r.Size = uint64(start), uint64(finish-start)
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.Offset+last.Size > r.Offset {
				return nil, fmt.Errorf("overlapping remapped native source range %d", i)
			}
			if last.Offset+last.Size == r.Offset && last.Function == r.Function && last.WasmOffset == r.WasmOffset && last.InlineParent == r.InlineParent {
				last.Size += r.Size
				continue
			}
		}
		out = append(out, r)
	}
	return out, nil
}
