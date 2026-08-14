package shared

import "fmt"

// RelaxKind classifies the small subset of emitted fragments whose final size
// may differ from their maximal safe encoding. Fixed instructions are not
// represented, keeping finalizer state proportional to variable sites rather
// than native instruction count.
type RelaxKind uint8

const (
	RelaxBranch RelaxKind = iota
	RelaxFrameSub
	RelaxFrameAdd
	RelaxDeadHole
	RelaxAlignment
	RelaxStackRef
	RelaxPoolRef
	RelaxJumpTableRef
)

// RelaxSite describes one variable-size fragment in maximal-encoding offsets.
// Target and Aux are backend-defined label/slot/condition data. Choice remains
// zero on the identity path and is reserved for the bounded shrink solver.
type RelaxSite struct {
	Off      uint32
	Target   uint32
	Aux      uint32
	Kind     RelaxKind
	LongLen  uint8
	ShortLen uint8
	Choice   uint8
}

// CodeLabel is a symbolic function-relative code position.
type CodeLabel struct {
	Off uint32
}

// MarkKind identifies metadata whose native offset must move with compacted
// code. Index identifies the owning relocation/callsite when several marks have
// the same kind.
type MarkKind uint8

const (
	MarkEntry MarkKind = iota
	MarkInternalEntry
	MarkCallReloc
	MarkCallReturn
	MarkAdapterReturn
	MarkTrapPC
	MarkSafepoint
	MarkEHHandler
	MarkJumpTable
	MarkLiteral
	MarkPluginReloc
	MarkSource
)

type CodeMark struct {
	Off   uint32
	Index uint32
	Kind  MarkKind
}

// OffsetMap maps maximal-encoding offsets to final offsets. The identity form
// intentionally stores only the old length; later compacting forms can add a
// bounded deletion/prefix-delta representation without changing callers.
type OffsetMap struct {
	oldLen      uint32
	finalLen    uint32
	deletionN   uint8
	deletionOff [MaxOffsetMapDeletions]uint32
	deleted     [MaxOffsetMapDeletions]uint32
}

// MaxOffsetMapDeletions is the fixed per-function deletion budget. Backends may
// retain later candidates in their maximal-safe form; correctness never depends
// on maximizing relaxation.
const MaxOffsetMapDeletions = 128

func (m *OffsetMap) Map(off int) (int, bool) {
	if off < 0 || uint64(off) > uint64(m.oldLen) {
		return 0, false
	}
	// Find the last deletion whose start is at or before off. The inventory is
	// small and fixed-capacity, but branch-heavy functions map enough labels and
	// relocation sites that a linear walk here becomes the finalizer's dominant
	// cost as the deletion budget grows.
	lo, hi := 0, int(m.deletionN)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if int(m.deletionOff[mid]) <= off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	i := lo - 1
	if i < 0 {
		return off, true
	}
	start := int(m.deletionOff[i])
	previousDeleted := uint32(0)
	if i > 0 {
		previousDeleted = m.deleted[i-1]
	}
	length := m.deleted[i] - previousDeleted
	end := start + int(length)
	if off > start && off < end {
		return 0, false
	}
	delta := m.deleted[i]
	if off == start {
		delta -= length
	}
	return off - int(delta), true
}

func (m *OffsetMap) FinalLen() int { return int(m.finalLen) }

// DeletedRange is one half-open maximal-encoding byte range removed by
// compaction. Ranges passed to NewOffsetMap must be sorted and non-overlapping.
type DeletedRange struct {
	Off uint32
	Len uint32
}

// NewOffsetMap validates a monotonic shrink plan and returns its old-to-new
// mapping. The fixed-capacity result owns a copy of the deletion records.
func NewOffsetMap(oldLen int, deletions []DeletedRange) (OffsetMap, error) {
	if oldLen < 0 || uint64(oldLen) > uint64(^uint32(0)) {
		return OffsetMap{}, fmt.Errorf("finalizer: invalid %d-byte function length", oldLen)
	}
	if len(deletions) > MaxOffsetMapDeletions {
		return OffsetMap{}, fmt.Errorf("finalizer: %d deletions exceed fixed budget %d", len(deletions), MaxOffsetMapDeletions)
	}
	previousEnd := uint64(0)
	deleted := uint64(0)
	for i, deletion := range deletions {
		start := uint64(deletion.Off)
		end := start + uint64(deletion.Len)
		if deletion.Len == 0 || end > uint64(oldLen) || i != 0 && start < previousEnd {
			return OffsetMap{}, fmt.Errorf("finalizer: invalid deletion %d at %d+%d for %d-byte function", i, deletion.Off, deletion.Len, oldLen)
		}
		previousEnd = end
		deleted += uint64(deletion.Len)
	}
	result := OffsetMap{oldLen: uint32(oldLen), finalLen: uint32(uint64(oldLen) - deleted), deletionN: uint8(len(deletions))}
	deleted = 0
	for i, deletion := range deletions {
		result.deletionOff[i] = deletion.Off
		deleted += uint64(deletion.Len)
		result.deleted[i] = uint32(deleted)
	}
	return result, nil
}

type FinalizeResult struct {
	Code    []byte
	Offsets OffsetMap
}

// FinalizeIdentity validates the symbolic finalization inventory, preserves the
// exact byte slice, and returns an identity old-to-new map. This is the rollout
// oracle: backends must route every offset-bearing metadata path through it
// before any relaxation choice is allowed to shrink code.
func FinalizeIdentity(code []byte, sites []RelaxSite, labels []CodeLabel, marks []CodeMark) (FinalizeResult, error) {
	codeLen := uint64(len(code))
	if codeLen > uint64(^uint32(0)) {
		return FinalizeResult{}, fmt.Errorf("finalizer: %d-byte function exceeds 32-bit offset space", len(code))
	}
	for i, site := range sites {
		if err := ValidateRelaxSite(len(code), site); err != nil {
			return FinalizeResult{}, fmt.Errorf("finalizer: invalid relax site %d: %w", i, err)
		}
	}
	for i, label := range labels {
		if uint64(label.Off) > codeLen {
			return FinalizeResult{}, fmt.Errorf("finalizer: label %d at %d exceeds %d-byte function", i, label.Off, len(code))
		}
	}
	for i, mark := range marks {
		if uint64(mark.Off) > codeLen {
			return FinalizeResult{}, fmt.Errorf("finalizer: mark %d at %d exceeds %d-byte function", i, mark.Off, len(code))
		}
	}
	offsets, err := NewOffsetMap(len(code), nil)
	if err != nil {
		return FinalizeResult{}, err
	}
	return FinalizeResult{Code: code, Offsets: offsets}, nil
}

// ValidateRelaxSite validates one record without requiring callers to allocate
// a temporary site slice. Backends use this for variable sites already retained
// in compact architecture-owned scratch structures.
func ValidateRelaxSite(codeLen int, site RelaxSite) error {
	end := uint64(site.Off) + uint64(site.LongLen)
	if site.LongLen == 0 || site.ShortLen > site.LongLen || end > uint64(codeLen) {
		return fmt.Errorf("site at %d with lengths %d/%d exceeds %d-byte function", site.Off, site.LongLen, site.ShortLen, codeLen)
	}
	return nil
}
