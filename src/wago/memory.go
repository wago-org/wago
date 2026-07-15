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
	mu    sync.Mutex
	owner *Instance // non-nil for an instance-owned exported memory
	meta  uint64    // declared max u32 | importer count u26 | flags u6
}

const (
	memoryStateShared uint8 = 1 << iota
	memoryStateAddr64
	memoryStateAddrKnown
	memoryStateLimitsKnown
	memoryStateDeclaredHasMax
	memoryStateClosed

	memoryStateImporterShift = 32
	memoryStateImporterMask  = uint64(1<<26 - 1)
	memoryStateFlagsShift    = 58
)

func (s *memoryState) has(flag uint8) bool {
	return uint8(s.meta>>memoryStateFlagsShift)&flag != 0
}

func (s *memoryState) set(flag uint8, enabled bool) {
	bits := uint64(flag) << memoryStateFlagsShift
	if enabled {
		s.meta |= bits
	} else {
		s.meta &^= bits
	}
}

func (s *memoryState) importerCount() uint32 {
	return uint32(s.meta>>memoryStateImporterShift) & uint32(memoryStateImporterMask)
}

func (s *memoryState) setImporterCount(count uint32) {
	s.meta = s.meta&^(memoryStateImporterMask<<memoryStateImporterShift) |
		(uint64(count)&memoryStateImporterMask)<<memoryStateImporterShift
}

func (s *memoryState) declaredMaximum() uint32 { return uint32(s.meta) }

func (s *memoryState) setDeclaredMaximum(max uint32) {
	s.meta = s.meta&^uint64(^uint32(0)) | uint64(max)
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
	declaredMax := maxPages
	if maxPages == 0 {
		declaredMax = minPages
	}
	state := &memoryState{meta: uint64(declaredMax)}
	state.set(memoryStateAddrKnown|memoryStateLimitsKnown|memoryStateDeclaredHasMax, true)
	state.set(memoryStateShared, shared)
	m.state.Store(state)
	return m, nil
}

// Bytes returns the zero-copy linear-memory view shared with wasm, at the
// current (possibly grown) size. It uses the host-facing accessor so it stays
// valid after a memory.grow in guard-page mode — where the Go-side j.mem slice is
// capped at the initial commit while the grown pages live in the reservation.
// CurrentBytes would panic there (slice bounds beyond the initial commit); this
// mirrors what Instance.Read/Write already use via mem().
func (m *Memory) Bytes() []byte {
	jm := m.jobMemory()
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
	s := m.state.Load()
	if s == nil {
		return fmt.Errorf("wago: instance-owned memory must be released by closing its producer")
	}
	s.mu.Lock()
	if s.has(memoryStateClosed) || m.jm == nil {
		s.mu.Unlock()
		return nil
	}
	if s.owner != nil {
		s.mu.Unlock()
		return fmt.Errorf("wago: instance-owned memory must be released by closing its producer")
	}
	if count := s.importerCount(); count != 0 {
		s.mu.Unlock()
		return fmt.Errorf("wago: memory has %d live importer(s); close consumers before the memory", count)
	}
	s.set(memoryStateClosed, true)
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
	if s.has(memoryStateClosed) || m.jm == nil {
		return fmt.Errorf("memory owner is closed")
	}
	count := s.importerCount()
	if !s.has(memoryStateShared) && count != 0 {
		return fmt.Errorf("memory is already used by another instance")
	}
	if count == uint32(memoryStateImporterMask) {
		return fmt.Errorf("memory has too many live importers")
	}
	if s.owner != nil && !s.owner.retainResourceRoot() {
		return fmt.Errorf("memory owner instance is closed")
	}
	s.setImporterCount(count + 1)
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
	if count := s.importerCount(); count > 0 {
		s.setImporterCount(count - 1)
		owner = s.owner
	}
	s.mu.Unlock()
	if owner != nil {
		owner.releaseResourceRoot()
	}
}

func (m *Memory) share(owner *Instance, def memoryDef) error {
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
	if s.has(memoryStateClosed) || m.jm == nil {
		return fmt.Errorf("memory owner is closed")
	}
	if s.has(memoryStateAddrKnown) && s.has(memoryStateAddr64) != def.Addr64 {
		return fmt.Errorf("memory address form does not match prior export")
	}
	s.set(memoryStateAddr64, def.Addr64)
	s.set(memoryStateAddrKnown, true)
	// The original local owner defines the provider's exact external type. A
	// re-exported import forwards that type rather than replacing it with the
	// consumer's possibly weaker import declaration.
	if !s.has(memoryStateLimitsKnown) {
		s.set(memoryStateLimitsKnown, true)
		s.set(memoryStateDeclaredHasMax, def.HasMax)
		s.setDeclaredMaximum(uint32(def.Max))
	}
	if owner != nil {
		if s.owner != nil && s.owner != owner {
			return fmt.Errorf("memory already has a different producer owner")
		}
		s.owner = owner
	}
	s.set(memoryStateShared, true)
	return nil
}

func (m *Memory) validateLimits(min, max uint64, hasMax, addr64 bool) error {
	s := m.state.Load()
	if s == nil {
		return fmt.Errorf("memory has not been exported for import")
	}
	s.mu.Lock()
	providerAddr64, addrKnown := s.has(memoryStateAddr64), s.has(memoryStateAddrKnown)
	limitsKnown, providerHasMax, providerMax := s.has(memoryStateLimitsKnown), s.has(memoryStateDeclaredHasMax), uint64(s.declaredMaximum())
	s.mu.Unlock()
	if addrKnown && providerAddr64 != addr64 {
		providerBits, importBits := 32, 32
		if providerAddr64 {
			providerBits = 64
		}
		if addr64 {
			importBits = 64
		}
		return fmt.Errorf("address form mismatch: provider is memory%d, import requires memory%d", providerBits, importBits)
	}
	jm := m.jobMemory()
	if jm == nil {
		return fmt.Errorf("memory owner is closed")
	}
	actualMin, actualMax := uint64(jm.CurrentPages()), uint64(jm.MaxPages())
	if actualMin < min {
		return fmt.Errorf("memory current minimum %d pages is below required %d", actualMin, min)
	}
	if hasMax {
		if limitsKnown && !providerHasMax {
			return fmt.Errorf("memory has no declared maximum but a maximum of %d pages is required", max)
		}
		if limitsKnown && providerHasMax {
			actualMax = providerMax
		}
		if actualMax > max {
			return fmt.Errorf("memory maximum %d pages exceeds required %d", actualMax, max)
		}
	}
	return nil
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
		shared = s.has(memoryStateShared)
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
	s.set(memoryStateClosed, true)
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
