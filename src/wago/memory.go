package wago

import (
	"fmt"
	"sync"
	"sync/atomic"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Memory is a linear-memory object the host can create and import into a module,
// mirroring JS WebAssembly.Memory. The host owns it: read and write Bytes(), and
// Close() it when no instance importing it is still in use.
//
// The handle stays two pointers wide. Ordinary instance-owned memories keep the
// lifecycle sidecar nil; it is allocated only for host-created or exported/shared
// memories, so scalar instantiation retains its existing allocation footprint.
type Memory struct {
	jm    *coreruntime.JobMemory
	state atomic.Pointer[memoryState]
}

type memoryState struct {
	mu        sync.Mutex
	owner     *Instance // non-nil for an instance-owned exported memory
	importers int32
	hasMax    bool // whether the JobMemory reservation is the declared Wasm maximum
	shared    bool // true when multiple compatible instances may import this memory
	closed    bool
}

// NewMemory creates a host-owned linear memory. minPages/maxPages are in 64 KiB
// wasm pages. It is growable up to maxPages (via a memory.grow from wasm) without
// the base pointer moving; maxPages == 0 means a fixed memory pinned at minPages.
//
// In a signals-based (guard-page) build it is backed by a guard-page reservation,
// so it can be imported by modules compiled with either explicit or signals-based
// bounds checks; a default build produces an explicitly-bounded mapping usable
// only by explicit-bounds modules.
func NewMemory(minPages, maxPages uint32) (*Memory, error) {
	return newMemory(minPages, maxPages, false)
}

// NewSharedMemory creates a file-/runtime-scoped host memory that compatible
// modules may import concurrently. State and memory.grow effects are visible to
// every importer. Close rejects while any importer remains live.
func NewSharedMemory(minPages, maxPages uint32) (*Memory, error) {
	return newMemory(minPages, maxPages, true)
}

func newMemory(minPages, maxPages uint32, shared bool) (*Memory, error) {
	if maxPages != 0 && maxPages < minPages {
		return nil, fmt.Errorf("wago: memory maximum %d < minimum %d", maxPages, minPages)
	}
	const pageBytes = 1 << 16
	initial := int(minPages) * pageBytes
	max := initial
	if maxPages != 0 {
		max = int(maxPages) * pageBytes
	}
	// Prefer a guard-page reservation when this build supports it: it works for
	// explicit-bounds importers too (they read the size caches and check inline),
	// and it is the only layout a signals-based importer can safely elide checks
	// against, so one host memory serves modules compiled in either mode.
	var (
		jm  *coreruntime.JobMemory
		err error
	)
	if guardPageBuilt {
		jm, err = newGuardedJobMemory(initial, max)
	} else {
		jm, err = coreruntime.NewJobMemoryGrowable(initial, max)
	}
	if err != nil {
		return nil, err
	}
	m := &Memory{jm: jm}
	// The host API defines maxPages == 0 as fixed memory, so every host-created
	// memory has a declared maximum equal to the JobMemory reservation.
	m.state.Store(&memoryState{hasMax: true, shared: shared})
	return m, nil
}

// Bytes returns the zero-copy linear-memory view shared with wasm, at the
// current (possibly grown) size. It uses the host-facing accessor so it stays
// valid after a memory.grow in guard-page mode — where the Go-side j.mem slice is
// capped at the initial commit while the grown pages live in the reservation.
// CurrentBytes would panic there (slice bounds beyond the initial commit); this
// mirrors what Instance.Read/Write already use via mem().
//
// The returned slice borrows mmap-backed storage: it is valid only while this
// Memory and its owning Instance remain open. Bytes and every access through a
// previously returned slice must not run concurrently with Memory.Close or
// Instance.Close. A raw []byte cannot carry a lifetime-release callback, so
// callers are responsible for that synchronization. After close is observable,
// Bytes returns nil rather than exposing the stale mapping.
func (m *Memory) Bytes() []byte {
	if m == nil {
		return nil
	}
	s := m.state.Load()
	if s == nil {
		if m.jm == nil {
			return nil
		}
		return m.jm.HostBytes()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || m.jm == nil || (s.owner != nil && s.owner.isLogicallyClosed()) {
		return nil
	}
	return m.jm.HostBytes()
}

// Close releases a host-created memory after every importer closes. An exported
// instance-owned memory is released by closing its producer instance instead.
func (m *Memory) Close() error {
	if m == nil {
		return nil
	}
	s := m.state.Load()
	if s == nil {
		return fmt.Errorf("wago: instance-owned memory must be released by closing its producer")
	}
	s.mu.Lock()
	if s.closed || m.jm == nil {
		s.mu.Unlock()
		return nil
	}
	if s.owner != nil {
		s.mu.Unlock()
		return fmt.Errorf("wago: instance-owned memory must be released by closing its producer")
	}
	if s.importers != 0 {
		count := s.importers
		s.mu.Unlock()
		return fmt.Errorf("wago: memory has %d live importer(s); close consumers before the memory", count)
	}
	s.closed = true
	jm := m.jm
	m.jm = nil
	s.mu.Unlock()
	return jm.Close()
}

func (m *Memory) attachImporter() error {
	if m == nil {
		return fmt.Errorf("memory is nil")
	}
	s := m.state.Load()
	if s == nil {
		return fmt.Errorf("memory has not been exported for import")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || m.jm == nil {
		return fmt.Errorf("memory owner is closed")
	}
	if s.owner != nil && !s.shared {
		return fmt.Errorf("memory has not been exported for import")
	}
	if !s.shared && s.importers != 0 {
		return fmt.Errorf("memory is already used by another instance")
	}
	if s.owner != nil && !s.owner.retainResourceRoot() {
		return fmt.Errorf("memory owner instance is closed")
	}
	s.importers++
	return nil
}

func (m *Memory) detachImporter() {
	if m == nil {
		return
	}
	s := m.state.Load()
	if s == nil {
		return
	}
	s.mu.Lock()
	var owner *Instance
	if s.importers > 0 {
		s.importers--
		owner = s.owner
	}
	s.mu.Unlock()
	if owner != nil {
		owner.releaseResourceRoot()
	}
}

func (m *Memory) observeOwner(owner *Instance) error {
	if m == nil || owner == nil {
		return fmt.Errorf("memory owner is nil")
	}
	s := m.state.Load()
	if s == nil {
		fresh := &memoryState{}
		if m.state.CompareAndSwap(nil, fresh) {
			s = fresh
		} else {
			s = m.state.Load()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || m.jm == nil {
		return fmt.Errorf("memory owner is closed")
	}
	if s.owner != nil && s.owner != owner {
		return fmt.Errorf("memory already has a different producer owner")
	}
	s.owner = owner
	if owner.c != nil {
		s.hasMax = owner.c.MemHasMax
	}
	return nil
}

func (m *Memory) share(owner *Instance) error {
	if m == nil {
		return fmt.Errorf("memory is nil")
	}
	s := m.state.Load()
	if s == nil {
		fresh := &memoryState{}
		if m.state.CompareAndSwap(nil, fresh) {
			s = fresh
		} else {
			s = m.state.Load()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || m.jm == nil {
		return fmt.Errorf("memory owner is closed")
	}
	if owner != nil {
		if s.owner != nil && s.owner != owner {
			return fmt.Errorf("memory already has a different producer owner")
		}
		s.owner = owner
		if owner.c != nil {
			s.hasMax = owner.c.MemHasMax
		}
	}
	s.shared = true
	return nil
}

func formatMemoryMaximum(maxPages uint32, hasMax bool) string {
	if !hasMax {
		return "unbounded"
	}
	return fmt.Sprintf("%d", maxPages)
}

func (m *Memory) importLimits() (minPages, maxPages uint32, hasMax bool, ok bool) {
	if m == nil {
		return 0, 0, false, false
	}
	s := m.state.Load()
	if s == nil {
		return 0, 0, false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || m.jm == nil {
		return 0, 0, false, false
	}
	const pageBytes = 1 << 16
	currentPages := uint32(len(m.jm.HostBytes()) / pageBytes)
	maxPages = 0
	if s.hasMax {
		maxPages = m.jm.MaxPages()
	}
	return currentPages, maxPages, s.hasMax, true
}

func (m *Memory) importShape() (guarded, shared bool) {
	if m == nil {
		return false, false
	}
	jm := m.jobMemory()
	if jm != nil {
		base, _ := jm.ReserveRange()
		guarded = base != 0
	}
	if s := m.state.Load(); s != nil {
		s.mu.Lock()
		shared = s.shared
		s.mu.Unlock()
	}
	return guarded, shared
}

func (m *Memory) jobMemory() *coreruntime.JobMemory {
	if m == nil {
		return nil
	}
	s := m.state.Load()
	if s == nil {
		return m.jm
	}
	s.mu.Lock()
	jm := m.jm
	s.mu.Unlock()
	return jm
}

func (m *Memory) ownerClosed() {
	if m == nil {
		return
	}
	s := m.state.Load()
	if s == nil {
		m.jm = nil
		return
	}
	s.mu.Lock()
	s.closed = true
	m.jm = nil
	s.mu.Unlock()
}

// memory returns the *Memory provided for key, if any.
func (im Imports) memory(key string) (*Memory, bool) {
	m, ok := im[key].(*Memory)
	return m, ok
}

// table returns the *Table provided for key, if any.
func (im Imports) table(key string) (*Table, bool) {
	t, ok := im[key].(*Table)
	return t, ok
}
