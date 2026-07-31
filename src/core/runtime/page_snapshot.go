package runtime

import "errors"

// ErrPageSnapshotSizeChanged means linear memory grew after the snapshot was
// bound. WebAssembly memory cannot shrink, so callers should discard that
// instance rather than retain its enlarged reservation.
var ErrPageSnapshotSizeChanged = errors.New("wago: linear memory size changed after page snapshot")

// PageSnapshot is an immutable linear-memory image. On supported systems it is
// held in an unlinked file and mapped MAP_PRIVATE into every bound JobMemory,
// allowing clean pages to be shared and dirty pages to be discarded by remap.
type PageSnapshot struct {
	backing pageSnapshotBacking
	size    int
}

// NewPageSnapshot captures data in an immutable page-backed image.
func NewPageSnapshot(data []byte) (*PageSnapshot, error) {
	backing, err := newPageSnapshotBacking(data)
	if err != nil {
		return nil, err
	}
	return &PageSnapshot{backing: backing, size: len(data)}, nil
}

// PageBacked reports whether reset uses private fixed mappings on this target.
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
	if s == nil || s.backing == nil {
		return errors.New("wago: nil page snapshot")
	}
	if j.curBytes() != s.size {
		return ErrPageSnapshotSizeChanged
	}
	if s.size > j.linLen {
		return ErrPageSnapshotSizeChanged
	}
	if err := s.backing.reset(j.LinMemBase(), s.size); err != nil {
		return err
	}
	j.putU32(offActualLinMemByteSize, uint32(s.size))
	j.putU32(offLinMemWasmSize, uint32(s.size/65536))
	return nil
}

type pageSnapshotBacking interface {
	reset(addr uintptr, size int) error
	pageBacked() bool
	close() error
}
