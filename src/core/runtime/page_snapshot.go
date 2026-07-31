package runtime

import "errors"

// ErrPageSnapshotSizeChanged means linear memory grew after the snapshot was
// bound. WebAssembly memory cannot shrink, so callers should discard that
// instance rather than retain its enlarged reservation.
var ErrPageSnapshotSizeChanged = errors.New("wago: linear memory size changed after page snapshot")

// PageSnapshot is an immutable linear-memory image. On supported systems it is
// held in an unlinked file and mapped MAP_PRIVATE into every bound JobMemory,
// allowing clean pages to be shared and dirty pages to be discarded in place.
type PageSnapshot struct {
	backing pageSnapshotBacking
	size    int
}

// PageSnapshotDirtyTracker records writes to one private snapshot mapping so
// reset can discard only pages that differ from the immutable image.
type PageSnapshotDirtyTracker struct {
	snapshot *PageSnapshot
	job      *JobMemory
	impl     pageSnapshotDirtyTracker
}

// NewPageSnapshot captures data in an immutable page-backed image.
func NewPageSnapshot(data []byte) (*PageSnapshot, error) {
	backing, err := newPageSnapshotBacking(data)
	if err != nil {
		return nil, err
	}
	return &PageSnapshot{backing: backing, size: len(data)}, nil
}

// PageBacked reports whether reset uses private file-backed pages on this target.
func (s *PageSnapshot) PageBacked() bool {
	return s != nil && s.backing != nil && s.backing.pageBacked()
}

// Close releases the snapshot backing. Existing private mappings remain valid,
// but no subsequent reset may use the closed snapshot.
func (s *PageSnapshot) Close() error {
	if s == nil || s.backing == nil {
		return nil
	}
	return s.backing.close()
}

// ResetToPageSnapshot replaces the current linear-memory pages with a fresh
// private view of s. The stable linear-memory base required by native code does
// not change.
func (j *JobMemory) ResetToPageSnapshot(s *PageSnapshot) error {
	if err := j.validatePageSnapshot(s); err != nil {
		return err
	}
	if err := s.backing.reset(j.LinMemBase(), s.size); err != nil {
		return err
	}
	j.restorePageSnapshotSize(s.size)
	return nil
}

// DiscardToPageSnapshot restores a JobMemory that is already privately mapped
// to s. Linux can discard the mapping's private COW pages in place, avoiding
// mmap/MAP_FIXED and VMA replacement on every request. Other targets fall back
// to copying the immutable image.
func (j *JobMemory) DiscardToPageSnapshot(s *PageSnapshot) error {
	if err := j.validatePageSnapshot(s); err != nil {
		return err
	}
	if err := s.backing.discard(j.LinMemBase(), s.size); err != nil {
		return err
	}
	j.restorePageSnapshotSize(s.size)
	return nil
}

// TrackPageSnapshotWrites starts per-page write tracking on a JobMemory already
// bound to s. prefixBytes identifies a static prefix requiring selective dirty
// restoration; the remaining heap is discarded wholesale. Pass zero to track
// the entire image selectively. The tracker belongs to this JobMemory and must
// be closed before the mapping is released.
func (j *JobMemory) TrackPageSnapshotWrites(
	s *PageSnapshot,
	prefixBytes uint64,
) (*PageSnapshotDirtyTracker, error) {
	if err := j.validatePageSnapshot(s); err != nil {
		return nil, err
	}
	if prefixBytes > uint64(s.size) {
		return nil, ErrPageSnapshotSizeChanged
	}
	prefix := int(prefixBytes)
	if prefix == 0 {
		prefix = s.size
	}
	prefix = roundUpPage(prefix)
	if prefix > s.size {
		prefix = s.size
	}
	impl, err := s.backing.track(j.LinMemBase(), s.size, prefix)
	if err != nil {
		return nil, err
	}
	return &PageSnapshotDirtyTracker{snapshot: s, job: j, impl: impl}, nil
}

// DiscardDirtyToPageSnapshot restores only pages written since tracking began.
// Globals and other instance state are restored by the higher-level binding.
func (j *JobMemory) DiscardDirtyToPageSnapshot(t *PageSnapshotDirtyTracker) error {
	if t == nil || t.snapshot == nil || t.impl == nil {
		return errors.New("wago: nil page snapshot dirty tracker")
	}
	if t.job != j {
		return errors.New("wago: page snapshot dirty tracker belongs to a different memory")
	}
	if err := j.validatePageSnapshot(t.snapshot); err != nil {
		return err
	}
	if err := t.impl.reset(); err != nil {
		return err
	}
	j.restorePageSnapshotSize(t.snapshot.size)
	return nil
}

// Close releases per-mapping dirty tracking resources.
func (t *PageSnapshotDirtyTracker) Close() error {
	if t == nil || t.impl == nil {
		return nil
	}
	err := t.impl.close()
	t.impl = nil
	return err
}

// Selective reports whether the target can identify and discard only pages
// written since the previous reset. False means reset safely falls back to
// discarding the complete snapshot mapping.
func (t *PageSnapshotDirtyTracker) Selective() bool {
	return t != nil && t.impl != nil && t.impl.selective()
}

func (j *JobMemory) validatePageSnapshot(s *PageSnapshot) error {
	if s == nil || s.backing == nil {
		return errors.New("wago: nil page snapshot")
	}
	if j.curBytes() != s.size {
		return ErrPageSnapshotSizeChanged
	}
	if s.size > j.linLen {
		return ErrPageSnapshotSizeChanged
	}
	return nil
}

func (j *JobMemory) restorePageSnapshotSize(size int) {
	j.putU32(offActualLinMemByteSize, uint32(size))
	j.putU32(offLinMemWasmSize, uint32(size/65536))
}

type pageSnapshotBacking interface {
	reset(addr uintptr, size int) error
	discard(addr uintptr, size int) error
	track(addr uintptr, size, prefix int) (pageSnapshotDirtyTracker, error)
	pageBacked() bool
	close() error
}

type pageSnapshotDirtyTracker interface {
	reset() error
	close() error
	selective() bool
}
