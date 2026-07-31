package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// IsPageSnapshotMemoryGrown reports whether an instance's linear memory grew
// since it was bound. Callers should discard the enlarged instance.
func IsPageSnapshotMemoryGrown(err error) bool {
	return errors.Is(err, coreruntime.ErrPageSnapshotSizeChanged)
}

// PageSnapshot is immutable post-initialization module state suitable for
// binding to multiple instances. Linear memory is file-backed on supported
// systems; numeric globals and passive-data state are restored alongside it.
//
// Tables are supported because each binding captures and restores its own
// instance-local table descriptors. This avoids copying one instance's native
// function-reference pointers into another.
type PageSnapshot struct {
	c               *Compiled
	memory          *coreruntime.PageSnapshot
	globals         []globalSnap
	passiveDataLens []uint32

	mu      sync.Mutex
	refs    int
	closing bool
	closed  bool
}

// PageSnapshotBinding resets one instance to a shared PageSnapshot. It is not
// safe to reset concurrently with calls into that instance.
type PageSnapshotBinding struct {
	snapshot    *PageSnapshot
	instance    *Instance
	table       []byte
	passiveData []byte
	released    atomic.Bool
}

// CapturePageSnapshot captures the current state of an initialized instance,
// creates the shared page image, and binds the source instance to it.
func CapturePageSnapshot(in *Instance) (*PageSnapshot, *PageSnapshotBinding, error) {
	if err := validatePageSnapshotInstance(in); err != nil {
		return nil, nil, err
	}
	live := in.memory.Bytes()
	memory, err := coreruntime.NewPageSnapshot(live)
	if err != nil {
		return nil, nil, err
	}
	s := &PageSnapshot{
		c:               in.c,
		memory:          memory,
		globals:         capturePageSnapshotGlobals(in),
		passiveDataLens: capturePassiveDataLens(in),
	}
	binding, err := s.Bind(in)
	if err != nil {
		_ = memory.Close()
		return nil, nil, err
	}
	return s, binding, nil
}

func validatePageSnapshotInstance(in *Instance) error {
	if in == nil || in.c == nil || in.memory == nil || in.jm == nil {
		return errors.New("wago: page snapshot requires a live instance")
	}
	if !in.ownsMem {
		return errors.New("wago: cannot page-snapshot imported or shared memory")
	}
	if err := in.c.validateSnapshotReferenceGlobals(); err != nil {
		return err
	}
	return nil
}

func capturePageSnapshotGlobals(in *Instance) []globalSnap {
	globals := make([]globalSnap, len(in.globalCells))
	for i, g := range in.globalCells {
		if g != nil {
			globals[i] = globalSnap{typ: g.Type, bits: readGlobalObject(g, g.Type), vec: readGlobalObjectV128(g)}
		}
	}
	return globals
}

// PageBacked reports whether this target resets with MAP_PRIVATE page remaps.
func (s *PageSnapshot) PageBacked() bool {
	return s != nil && s.memory != nil && s.memory.PageBacked()
}

// Bind attaches an initialized instance of the same compiled module to the
// snapshot and immediately restores it to the captured state.
func (s *PageSnapshot) Bind(in *Instance) (*PageSnapshotBinding, error) {
	if s == nil || s.c == nil || s.memory == nil {
		return nil, errors.New("wago: nil page snapshot")
	}
	if err := validatePageSnapshotInstance(in); err != nil {
		return nil, err
	}
	if in.c != s.c {
		return nil, errors.New("wago: page snapshot belongs to a different compiled module")
	}

	s.mu.Lock()
	if s.closing || s.closed {
		s.mu.Unlock()
		return nil, errors.New("wago: page snapshot is closed")
	}
	s.refs++
	s.mu.Unlock()

	b := &PageSnapshotBinding{
		snapshot:    s,
		instance:    in,
		table:       append([]byte(nil), in.tableDescriptorBytes()...),
		passiveData: append([]byte(nil), in.passiveDataDesc...),
	}
	if err := b.Reset(); err != nil {
		_ = b.Close()
		return nil, err
	}
	return b, nil
}

func (in *Instance) tableDescriptorBytes() []byte {
	if in == nil || in.tableDescPtr == 0 || in.tableDescLen == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
}

// Reset discards dirty linear-memory pages and restores globals, table
// descriptors, and passive-data drop state.
func (b *PageSnapshotBinding) Reset() error {
	if b == nil || b.snapshot == nil || b.instance == nil || b.released.Load() {
		return errors.New("wago: page snapshot binding is closed")
	}
	s := b.snapshot
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("wago: page snapshot is closed")
	}
	if err := b.instance.jm.ResetToPageSnapshot(s.memory); err != nil {
		return err
	}
	for i, snap := range s.globals {
		if i >= len(b.instance.globalCells) {
			break
		}
		if g := b.instance.globalCells[i]; g != nil {
			if snap.typ == ValV128 {
				writeGlobalObjectV128(g, snap.vec)
			} else {
				writeGlobalObject(g, snap.typ, snap.bits)
			}
		}
	}
	if table := b.instance.tableDescriptorBytes(); len(table) != len(b.table) {
		return fmt.Errorf("wago: table descriptor size changed: got %d, want %d", len(table), len(b.table))
	} else {
		copy(table, b.table)
	}
	if len(b.instance.passiveDataDesc) != len(b.passiveData) {
		return fmt.Errorf("wago: passive data descriptor size changed: got %d, want %d", len(b.instance.passiveDataDesc), len(b.passiveData))
	}
	copy(b.instance.passiveDataDesc, b.passiveData)
	// Preserve compatibility with older descriptors whose pointer fields are
	// rebuilt per instance but whose drop state is represented only by length.
	for i, n := range s.passiveDataLens {
		off := i*coreruntime.PassiveDataDescBytes + 8
		if off+4 <= len(b.instance.passiveDataDesc) {
			binary.LittleEndian.PutUint32(b.instance.passiveDataDesc[off:], n)
		}
	}
	return nil
}

// Close releases this binding's reference to the shared snapshot.
func (b *PageSnapshotBinding) Close() error {
	if b == nil || b.snapshot == nil || !b.released.CompareAndSwap(false, true) {
		return nil
	}
	s := b.snapshot
	s.mu.Lock()
	s.refs--
	closeNow := s.closing && s.refs == 0 && !s.closed
	if closeNow {
		s.closed = true
	}
	s.mu.Unlock()
	if closeNow {
		return s.memory.Close()
	}
	return nil
}

// Close releases the backing after all live bindings close. It is safe to call
// while old-generation instances are still draining.
func (s *PageSnapshot) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.closing = true
	closeNow := s.refs == 0 && !s.closed
	if closeNow {
		s.closed = true
	}
	s.mu.Unlock()
	if closeNow {
		return s.memory.Close()
	}
	return nil
}
