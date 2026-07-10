package wago

import (
	"fmt"
	"sync"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Memory is a linear-memory object the host can create and import into a module,
// mirroring JS WebAssembly.Memory. The host owns it: read and write Bytes(), and
// Close() it when no instance importing it is still in use.
type Memory struct {
	jm      *coreruntime.JobMemory
	guarded bool // backed by a guard-page reservation (usable by signals-based modules)

	mu        sync.Mutex
	owner     *Instance // non-nil for an instance-owned exported memory
	importers int
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
	if guardPageBuilt {
		jm, err := newGuardedJobMemory(initial, max)
		if err != nil {
			return nil, err
		}
		return &Memory{jm: jm, guarded: true, shared: shared}, nil
	}
	jm, err := coreruntime.NewJobMemoryGrowable(initial, max)
	if err != nil {
		return nil, err
	}
	return &Memory{jm: jm, shared: shared}, nil
}

// Bytes returns the zero-copy linear-memory view shared with wasm, at the
// current (possibly grown) size. It uses the host-facing accessor so it stays
// valid after a memory.grow in guard-page mode — where the Go-side j.mem slice is
// capped at the initial commit while the grown pages live in the reservation.
// CurrentBytes would panic there (slice bounds beyond the initial commit); this
// mirrors what Instance.Read/Write already use via mem().
func (m *Memory) Bytes() []byte {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	jm := m.jm
	m.mu.Unlock()
	if jm == nil {
		return nil
	}
	return jm.HostBytes()
}

// Close releases a host-created memory after every importer closes. An exported
// instance-owned memory is released by closing its producer instance instead.
func (m *Memory) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed || m.jm == nil {
		m.mu.Unlock()
		return nil
	}
	if m.owner != nil {
		m.mu.Unlock()
		return fmt.Errorf("wago: instance-owned memory must be released by closing its producer")
	}
	if m.importers != 0 {
		count := m.importers
		m.mu.Unlock()
		return fmt.Errorf("wago: memory has %d live importer(s); close consumers before the memory", count)
	}
	m.closed = true
	jm := m.jm
	m.jm = nil
	m.mu.Unlock()
	return jm.Close()
}

func (m *Memory) attachImporter() error {
	if m == nil {
		return fmt.Errorf("memory is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.jm == nil {
		return fmt.Errorf("memory owner is closed")
	}
	if !m.shared && m.importers != 0 {
		return fmt.Errorf("memory is already used by another instance")
	}
	if m.owner != nil && !m.owner.retainResourceRoot() {
		return fmt.Errorf("memory owner instance is closed")
	}
	m.importers++
	return nil
}

func (m *Memory) detachImporter() {
	if m == nil {
		return
	}
	m.mu.Lock()
	var owner *Instance
	if m.importers > 0 {
		m.importers--
		owner = m.owner
	}
	m.mu.Unlock()
	if owner != nil {
		owner.releaseResourceRoot()
	}
}

func (m *Memory) share(owner *Instance) error {
	if m == nil {
		return fmt.Errorf("memory is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.jm == nil {
		return fmt.Errorf("memory owner is closed")
	}
	if owner != nil {
		if m.owner != nil && m.owner != owner {
			return fmt.Errorf("memory already has a different producer owner")
		}
		m.owner = owner
	}
	m.shared = true
	return nil
}

func (m *Memory) ownerClosed() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	m.jm = nil
	m.mu.Unlock()
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
