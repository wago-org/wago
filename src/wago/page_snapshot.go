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
// systems; numeric globals, local funcref tables, and passive segment state are
// restored alongside it.
//
// Tables are supported because each binding captures and restores its own
// instance-local table descriptors. This avoids copying one instance's native
// function-reference pointers into another.
type PageSnapshot struct {
	c               *Compiled
	memory          *coreruntime.PageSnapshot
	scalarGlobals   []scalarGlobalSnap
	vectorGlobals   []vectorGlobalSnap
	passiveDataLens []uint32
	trackDirty      bool
	dataEnd         uint64
	cursor          uint64

	mu      sync.Mutex
	refs    int
	closing bool
	closed  bool
}

type scalarGlobalSnap struct {
	index int
	bits  uint64
}

type vectorGlobalSnap struct {
	index int
	vec   V128
}

// PageSnapshotBinding resets one instance to a shared PageSnapshot. It is not
// safe to reset concurrently with calls into that instance.
type PageSnapshotBinding struct {
	snapshot        *PageSnapshot
	instance        *Instance
	tableLive       []byte
	table           []byte
	passiveElemLive []byte
	passiveElem     []byte
	passiveDataLive []byte
	passiveData     []byte
	memoryBound     bool
	dirtyTracker    *coreruntime.PageSnapshotDirtyTracker
	released        atomic.Bool
}

// CapturePageSnapshot captures the current state of an initialized instance,
// creates the shared page image, and binds the source instance to it.
func CapturePageSnapshot(in *Instance) (*PageSnapshot, *PageSnapshotBinding, error) {
	return capturePageSnapshot(in, false, 0, 0)
}

// CaptureStubPageSnapshot captures an initialized AssemblyScript stub module
// after validating its bump allocator cursor. Restoring its globals rewinds the
// cursor while dirty-page tracking restores every changed linear-memory page.
// The result is byte-exact even when the module mutates static data or leaves
// stale bytes above the rewound cursor.
func CaptureStubPageSnapshot(in *Instance) (*PageSnapshot, *PageSnapshotBinding, error) {
	globals, err := CaptureStubGlobals(in)
	if err != nil {
		return nil, nil, err
	}
	dataEnd, err := activeDataEnd(in)
	if err != nil {
		return nil, nil, err
	}
	if dataEnd > globals.Cursor() {
		return nil, nil, fmt.Errorf("wago: static data end %d exceeds AssemblyScript cursor %d", dataEnd, globals.Cursor())
	}
	return capturePageSnapshot(in, true, dataEnd, globals.Cursor())
}

func capturePageSnapshot(
	in *Instance,
	trackDirty bool,
	dataEnd uint64,
	cursor uint64,
) (*PageSnapshot, *PageSnapshotBinding, error) {
	if err := validatePageSnapshotInstance(in); err != nil {
		return nil, nil, err
	}
	live := in.memory.Bytes()
	memory, err := coreruntime.NewPageSnapshot(live)
	if err != nil {
		return nil, nil, err
	}
	scalarGlobals, vectorGlobals := captureMutablePageSnapshotGlobals(in)
	s := &PageSnapshot{
		c:               in.c,
		memory:          memory,
		scalarGlobals:   scalarGlobals,
		vectorGlobals:   vectorGlobals,
		passiveDataLens: capturePassiveDataLens(in),
		trackDirty:      trackDirty,
		dataEnd:         dataEnd,
		cursor:          cursor,
	}
	binding, err := s.Bind(in)
	if err != nil {
		_ = memory.Close()
		return nil, nil, err
	}
	return s, binding, nil
}

func captureMutablePageSnapshotGlobals(in *Instance) ([]scalarGlobalSnap, []vectorGlobalSnap) {
	localStart := len(in.c.GlobalImports)
	var scalarCount, vectorCount int
	for i := localStart; i < len(in.c.Globals); i++ {
		if in.c.Globals[i].Mutable {
			if in.c.Globals[i].Type == ValV128 {
				vectorCount++
			} else {
				scalarCount++
			}
		}
	}
	scalars := make([]scalarGlobalSnap, 0, scalarCount)
	vectors := make([]vectorGlobalSnap, 0, vectorCount)
	for i := localStart; i < len(in.c.Globals) && i < len(in.globalCells); i++ {
		def := in.c.Globals[i]
		g := in.globalCells[i]
		if !def.Mutable || g == nil {
			continue
		}
		if def.Type == ValV128 {
			vectors = append(vectors, vectorGlobalSnap{index: i, vec: readGlobalObjectV128(g)})
		} else {
			scalars = append(scalars, scalarGlobalSnap{index: i, bits: readGlobalObject(g, def.Type)})
		}
	}
	return scalars, vectors
}

// activeDataEnd returns AssemblyScript's __data_end without requiring the
// compiler-generated constant to be exported. AssemblyScript lays out static
// data in active segments and defines __data_end as their greatest end offset.
func activeDataEnd(in *Instance) (uint64, error) {
	var end uint64
	for i, data := range in.c.Data {
		offset := uint64(data.Offset.Base)
		if data.Offset.HasGlobal {
			idx := data.Offset.Global
			if idx < 0 || idx >= len(in.c.Globals) || idx >= len(in.globalCells) || in.globalCells[idx] == nil {
				return 0, fmt.Errorf("wago: data %d offset global %d out of range", i, idx)
			}
			offset = readGlobalObject(in.globalCells[idx], in.c.Globals[idx].Type)
		}
		dataEnd := offset + uint64(len(data.Bytes))
		if dataEnd < offset {
			return 0, fmt.Errorf("wago: data %d end overflows", i)
		}
		if dataEnd > end {
			end = dataEnd
		}
	}
	return end, nil
}

func validatePageSnapshotInstance(in *Instance) error {
	if in == nil || in.c == nil || in.memory == nil || in.jm == nil {
		return errors.New("wago: page snapshot requires a live instance")
	}
	if !in.ownsMem {
		return errors.New("wago: cannot page-snapshot imported or shared memory")
	}
	for i, g := range in.c.GlobalImports {
		if g.Mutable {
			return fmt.Errorf("wago: cannot page-snapshot mutable imported global %d (%s.%s)", i, g.Module, g.Name)
		}
	}
	if in.c.tableImportCount() != 0 {
		return errors.New("wago: cannot page-snapshot imported or shared tables")
	}
	if in.c.hasExternrefTable() {
		return errors.New("wago: cannot page-snapshot externref tables")
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

// PageBacked reports whether this target resets with MAP_PRIVATE file-backed
// pages.
func (s *PageSnapshot) PageBacked() bool {
	return s != nil && s.memory != nil && s.memory.PageBacked()
}

// Cursor returns the validated post-initialization AssemblyScript stub bump
// allocator cursor, or zero for snapshots without cursor tracking.
func (s *PageSnapshot) Cursor() uint64 {
	if s == nil {
		return 0
	}
	return s.cursor
}

// DataEnd returns the first byte after the module's static active data. Stub
// reset restores memory from this boundary through Cursor.
func (s *PageSnapshot) DataEnd() uint64 {
	if s == nil {
		return 0
	}
	return s.dataEnd
}

// SelectiveDirtyReset reports whether this binding can restore only pages
// written since its previous reset. False means it safely uses full discard.
func (b *PageSnapshotBinding) SelectiveDirtyReset() bool {
	return b != nil && b.dirtyTracker != nil && b.dirtyTracker.Selective()
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

	table := in.tableDescriptorBytes()
	passiveElem := in.passiveElementDescriptorBytes()
	b := &PageSnapshotBinding{
		snapshot:        s,
		instance:        in,
		tableLive:       table,
		table:           append([]byte(nil), table...),
		passiveElemLive: passiveElem,
		passiveElem:     append([]byte(nil), passiveElem...),
		passiveDataLive: in.passiveDataDesc,
		passiveData:     append([]byte(nil), in.passiveDataDesc...),
	}
	// Pointer fields are instance-local, but drop-state lengths belong to the
	// shared snapshot. Bake those lengths into this binding's descriptor image
	// once instead of rebuilding them on every reset.
	for i, n := range s.passiveDataLens {
		off := i*coreruntime.PassiveDataDescBytes + 8
		if off+4 <= len(b.passiveData) {
			binary.LittleEndian.PutUint32(b.passiveData[off:], n)
		}
	}
	if err := b.Reset(); err != nil {
		_ = b.Close()
		return nil, err
	}
	if s.trackDirty {
		var err error
		b.dirtyTracker, err = in.jm.TrackPageSnapshotWrites(s.memory, 0, uint64(len(in.memory.Bytes())))
		if err != nil {
			_ = b.Close()
			return nil, err
		}
	}
	return b, nil
}

func (in *Instance) tableDescriptorBytes() []byte {
	if in == nil || in.tableDescPtr == 0 || in.tableDescLen == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
}

func (in *Instance) passiveElementDescriptorBytes() []byte {
	if in == nil || in.c == nil || in.jm == nil || len(in.c.passiveElems) == 0 {
		return nil
	}
	ptr := in.jm.CaptureInstanceContext().PassiveElemPtr
	if ptr == 0 {
		return nil
	}
	return unsafe.Slice(
		(*byte)(offHeapPtr(ptr)),
		len(in.c.passiveElems)*coreruntime.PassiveElemDescBytes,
	)
}

// Reset discards dirty linear-memory pages in place and restores numeric
// globals, local funcref table descriptors, and passive element/data drop
// state. The first reset installs the shared private mapping; later stub resets
// selectively discard every private dirty page, while other snapshots discard
// the complete mapping.
func (b *PageSnapshotBinding) Reset() error {
	if b == nil || b.snapshot == nil || b.instance == nil || b.released.Load() {
		return errors.New("wago: page snapshot binding is closed")
	}
	s := b.snapshot
	// A live binding owns one snapshot reference, so Close cannot release the
	// backing while Reset is running. Avoid the snapshot lifecycle mutex here:
	// bindings reset independent instances and must not serialize on shared
	// snapshot state.
	var err error
	if b.dirtyTracker != nil {
		err = b.instance.jm.DiscardDirtyToPageSnapshot(b.dirtyTracker)
	} else if b.memoryBound {
		err = b.instance.jm.DiscardToPageSnapshot(s.memory)
	} else {
		err = b.instance.jm.ResetToPageSnapshot(s.memory)
		if err == nil {
			b.memoryBound = true
		}
	}
	if err != nil {
		return err
	}
	for _, snap := range s.scalarGlobals {
		binary.LittleEndian.PutUint64(b.instance.globalCells[snap.index].cell, snap.bits)
	}
	for _, snap := range s.vectorGlobals {
		copy(b.instance.globalCells[snap.index].cell, snap.vec[:])
	}
	copy(b.tableLive, b.table)
	copy(b.passiveElemLive, b.passiveElem)
	copy(b.passiveDataLive, b.passiveData)
	return nil
}

// Close releases this binding's reference to the shared snapshot.
func (b *PageSnapshotBinding) Close() error {
	if b == nil || b.snapshot == nil || !b.released.CompareAndSwap(false, true) {
		return nil
	}
	var trackerErr error
	if b.dirtyTracker != nil {
		trackerErr = b.dirtyTracker.Close()
		b.dirtyTracker = nil
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
		if err := s.memory.Close(); err != nil {
			return err
		}
	}
	return trackerErr
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
