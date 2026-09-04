//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

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
	c := moduleTypeCache{valid: true}
	if n := m.MemCount(); n != 0 {
		c.memories = make([]wasm.MemType, n)
		for i := range c.memories {
			c.memories[i], _ = m.MemoryType(uint32(i))
		}
	}
	if n := m.GlobalCount(); n != 0 {
		c.globals = make([]wasm.GlobalType, n)
		for i := range c.globals {
			c.globals[i], _ = m.GlobalTypeByIndex(uint32(i))
		}
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
