package wago

import (
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Memory is a linear-memory object the host can create and import into a module,
// mirroring JS WebAssembly.Memory. The host owns it: read and write Bytes(), and
// Close() it when no instance importing it is still in use.
type Memory struct {
	jm      *coreruntime.JobMemory
	inUse   bool // a single instance is using it (host memories are single-use)
	shared  bool // cross-instance: several instances may reference it (Instance.ExportedMemory)
	guarded bool // backed by a guard-page reservation (usable by signals-based modules)
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
		return &Memory{jm: jm, guarded: true}, nil
	}
	jm, err := coreruntime.NewJobMemoryGrowable(initial, max)
	if err != nil {
		return nil, err
	}
	return &Memory{jm: jm}, nil
}

// Bytes returns the zero-copy linear-memory view shared with wasm, at the
// current (possibly grown) size. It uses the host-facing accessor so it stays
// valid after a memory.grow in guard-page mode — where the Go-side j.mem slice is
// capped at the initial commit while the grown pages live in the reservation.
// CurrentBytes would panic there (slice bounds beyond the initial commit); this
// mirrors what Instance.Read/Write already use via mem().
func (m *Memory) Bytes() []byte { return m.jm.HostBytes() }

// Close releases the memory. Only call it once every instance importing it is
// closed.
func (m *Memory) Close() error {
	if m == nil || m.jm == nil {
		return nil
	}
	err := m.jm.Close()
	m.jm = nil
	return err
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
