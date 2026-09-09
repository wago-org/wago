//go:build arm64

package arm64

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Optional compilation scratch must not grow without a fixed byte budget.
const maxModuleTypeCacheBytes = 1 << 20

// moduleTypeCache avoids repeatedly walking imports to resolve indexed memories
// and globals while compiling large modules. Tiny modules keep direct lookups so
// the cache never adds allocations to their fast path.
type moduleTypeCache struct {
	memories []wasm.MemType
	globals  []wasm.GlobalType
	valid    bool
}

func buildModuleTypeCache(m *wasm.Module, bodyBytes int) moduleTypeCache {
	if bodyBytes < minParallelHintBodyBytes {
		return moduleTypeCache{}
	}
	memories, globals := m.MemCount(), m.GlobalCount()
	bytes := uint64(memories)*uint64(unsafe.Sizeof(wasm.MemType{})) +
		uint64(globals)*uint64(unsafe.Sizeof(wasm.GlobalType{}))
	if bytes > maxModuleTypeCacheBytes {
		return moduleTypeCache{}
	}
	c := moduleTypeCache{valid: true}
	if memories != 0 {
		c.memories = make([]wasm.MemType, memories)
	}
	if globals != 0 {
		c.globals = make([]wasm.GlobalType, globals)
	}
	memAt, globalAt := 0, 0
	// Fill each index space in declaration order in one pass over imports.
	for i := range m.Imports {
		typ := m.Imports[i].Type
		switch typ.Kind {
		case wasm.ExternMem:
			c.memories[memAt] = typ.MemType()
			memAt++
		case wasm.ExternGlobal:
			c.globals[globalAt] = typ.GlobalType()
			globalAt++
		}
	}
	copy(c.memories[memAt:], m.Memories)
	for i := range m.Globals {
		c.globals[globalAt+i] = m.Globals[i].Type
	}
	return c
}

func (f *fn) memoryType(index uint32) (wasm.MemType, bool) {
	if f.sc != nil && f.sc.moduleTypes.valid {
		if int(index) >= len(f.sc.moduleTypes.memories) {
			return wasm.MemType{}, false
		}
		return f.sc.moduleTypes.memories[index], true
	}
	return f.m.MemoryType(index)
}

func (f *fn) globalType(index uint32) (wasm.GlobalType, bool) {
	if f.sc != nil && f.sc.moduleTypes.valid {
		if int(index) >= len(f.sc.moduleTypes.globals) {
			return wasm.GlobalType{}, false
		}
		return f.sc.moduleTypes.globals[index], true
	}
	return f.m.GlobalTypeByIndex(index)
}
