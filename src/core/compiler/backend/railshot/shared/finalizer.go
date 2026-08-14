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

// WideOffsetMap is the AMD64 Size/Embedded map. Large x86 functions retain
// substantially more five-byte branch-fold holes than ARM64 functions, so the
// wider backend pays for a larger bounded map without imposing that stack cost
// on ARM64 or the shared identity path.
type WideOffsetMap struct {
	oldLen      uint32
	finalLen    uint32
	deletionN   uint8
	deletionOff [MaxWideOffsetMapDeletions]uint32
	deleted     [MaxWideOffsetMapDeletions]uint32
}

// MaxOffsetMapDeletions is the fixed per-function deletion budget. Backends may
// retain later candidates in their maximal-safe form; correctness never depends
// on maximizing relaxation.
const MaxOffsetMapDeletions = 128

// MaxWideOffsetMapDeletions fits the immutable uint8 policy field while nearly
// doubling AMD64's deletion inventory. Correctness never depends on filling it.
const MaxWideOffsetMapDeletions = 255

func (m *OffsetMap) Map(off int) (int, bool) {
	return mapOffset(m.oldLen, m.deletionOff[:m.deletionN], m.deleted[:m.deletionN], off)
}

func (m *WideOffsetMap) Map(off int) (int, bool) {
	return mapOffset(m.oldLen, m.deletionOff[:m.deletionN], m.deleted[:m.deletionN], off)
}

func mapOffset(oldLen uint32, deletionOff, deleted []uint32, off int) (int, bool) {
	if off < 0 || uint64(off) > uint64(oldLen) {
		return 0, false
	}
	// Find the last deletion whose start is at or before off. The inventory is
	// small and fixed-capacity, but branch-heavy functions map enough labels and
	// relocation sites that a linear walk here becomes the finalizer's dominant
	// cost as the deletion budget grows.
	lo, hi := 0, len(deletionOff)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if int(deletionOff[mid]) <= off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	i := lo - 1
	if i < 0 {
		return off, true
	}
	start := int(deletionOff[i])
	previousDeleted := uint32(0)
	if i > 0 {
		previousDeleted = deleted[i-1]
	}
	length := deleted[i] - previousDeleted
	end := start + int(length)
	if off > start && off < end {
		return 0, false
	}
	delta := deleted[i]
	if off == start {
		delta -= length
	}
	return off - int(delta), true
}

func (m *OffsetMap) FinalLen() int     { return int(m.finalLen) }
func (m *WideOffsetMap) FinalLen() int { return int(m.finalLen) }

// DeletedRange is one half-open maximal-encoding byte range removed by
// compaction. Ranges passed to NewOffsetMap must be sorted and non-overlapping.
type DeletedRange struct {
	Off uint32
	Len uint32
}

// NewOffsetMap validates a monotonic shrink plan and returns its old-to-new
// mapping. The fixed-capacity result owns a copy of the deletion records.
func NewOffsetMap(oldLen int, deletions []DeletedRange) (OffsetMap, error) {
	var result OffsetMap
	if err := result.Reset(oldLen, deletions); err != nil {
		return OffsetMap{}, err
	}
	return result, nil
}

// Reset replaces m with the bounded mapping for oldLen and deletions. Backends
// use this form with reusable worker scratch so large fixed maps are neither
// allocated nor returned by value for every compiled function.
func (m *OffsetMap) Reset(oldLen int, deletions []DeletedRange) error {
	if err := validateOffsetMap(oldLen, deletions, MaxOffsetMapDeletions); err != nil {
		return err
	}
	m.oldLen = uint32(oldLen)
	m.deletionN = uint8(len(deletions))
	m.finalLen = fillOffsetMap(oldLen, deletions, m.deletionOff[:], m.deleted[:])
	return nil
}

// NewWideOffsetMap constructs the AMD64-only wider bounded mapping.
func NewWideOffsetMap(oldLen int, deletions []DeletedRange) (WideOffsetMap, error) {
	var result WideOffsetMap
	if err := result.Reset(oldLen, deletions); err != nil {
		return WideOffsetMap{}, err
	}
	return result, nil
}

// Reset is the reusable-storage form of NewWideOffsetMap.
func (m *WideOffsetMap) Reset(oldLen int, deletions []DeletedRange) error {
	if err := validateOffsetMap(oldLen, deletions, MaxWideOffsetMapDeletions); err != nil {
		return err
	}
	m.oldLen = uint32(oldLen)
	m.deletionN = uint8(len(deletions))
	m.finalLen = fillOffsetMap(oldLen, deletions, m.deletionOff[:], m.deleted[:])
	return nil
}

func validateOffsetMap(oldLen int, deletions []DeletedRange, maxDeletions int) error {
	if oldLen < 0 || uint64(oldLen) > uint64(^uint32(0)) {
		return fmt.Errorf("finalizer: invalid %d-byte function length", oldLen)
	}
	if len(deletions) > maxDeletions {
		return fmt.Errorf("finalizer: %d deletions exceed fixed budget %d", len(deletions), maxDeletions)
	}
	previousEnd := uint64(0)
	for i, deletion := range deletions {
		start := uint64(deletion.Off)
		end := start + uint64(deletion.Len)
		if deletion.Len == 0 || end > uint64(oldLen) || i != 0 && start < previousEnd {
			return fmt.Errorf("finalizer: invalid deletion %d at %d+%d for %d-byte function", i, deletion.Off, deletion.Len, oldLen)
		}
		previousEnd = end
	}
	return nil
}

func fillOffsetMap(oldLen int, deletions []DeletedRange, deletionOff, deletedPrefix []uint32) uint32 {
	deleted := uint64(0)
	for i, deletion := range deletions {
		deletionOff[i] = deletion.Off
		deleted += uint64(deletion.Len)
		deletedPrefix[i] = uint32(deleted)
	}
	return uint32(uint64(oldLen) - deleted)
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
