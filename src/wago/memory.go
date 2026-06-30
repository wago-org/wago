package wago

import (
	"fmt"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Memory is a linear-memory object the host can create and import into a module,
// mirroring JS WebAssembly.Memory. The host owns it: read and write Bytes(), and
// Close() it when no instance importing it is still in use.
type Memory struct {
	jm    *coreruntime.JobMemory
	inUse bool // an instance is using it; its basedata is per-instance, so no sharing yet
}

// NewMemory creates a host-owned linear memory. minPages/maxPages are in 64 KiB
// wasm pages; the current runtime backs a single page, so minPages must be ≤ 1.
func NewMemory(minPages, maxPages uint32) (*Memory, error) {
	if minPages > 1 {
		return nil, fmt.Errorf("wago: memory minimum %d pages exceeds the current 1-page limit", minPages)
	}
	if maxPages != 0 && maxPages < minPages {
		return nil, fmt.Errorf("wago: memory maximum %d < minimum %d", maxPages, minPages)
	}
	jm, err := coreruntime.NewJobMemory(1 << 16)
	if err != nil {
		return nil, err
	}
	return &Memory{jm: jm}, nil
}

// Bytes returns the zero-copy linear-memory view shared with wasm.
func (m *Memory) Bytes() []byte { return m.jm.LinearMemory() }

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
