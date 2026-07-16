package wago

import (
	"fmt"
	goruntime "runtime"
	"sync"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type compiledCodeCache struct {
	mu                              sync.Mutex
	mem                             []byte
	base                            uintptr
	refs                            int
	closed                          bool
	stagedFeatures                  CoreFeatures // compile-only admission; never serialized or publicly loaded
	collectorFreeStructuralMetadata bool         // exact staged products use struct descriptors only for function identity
}

func installCompiledFinalizer(c *Compiled) *Compiled {
	c.ensureCodeCache()
	// Give this compiler/deserialize-produced module its own validation memo so
	// Instantiate validates immutable compiler-produced metadata once. Hand-built
	// Compiled values leave the memo nil and are validated on every use.
	c.validateMemo = &validateMemo{}
	goruntime.SetFinalizer(c, func(c *Compiled) {
		_ = c.Close()
	})
	return c
}

func (c *Compiled) stagedFeatures() CoreFeatures {
	if c == nil || c.codeCache == nil {
		return 0
	}
	return c.codeCache.stagedFeatures
}

func (c *Compiled) collectorFreeStructuralMetadata() bool {
	return c != nil && c.codeCache != nil && c.codeCache.collectorFreeStructuralMetadata
}

func (c *Compiled) ensureCodeCache() {
	if c != nil && c.codeCache == nil {
		c.codeCache = &compiledCodeCache{}
	}
}

func (c *Compiled) acquireCode() (uintptr, error) {
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed {
		return 0, fmt.Errorf("compiled module is closed")
	}
	if cc.mem == nil {
		mem, base, err := coreruntime.MapCode(c.Code)
		if err != nil {
			return 0, err
		}
		cc.mem, cc.base = mem, base
	}
	cc.refs++
	return cc.base, nil
}

func (c *Compiled) releaseCode() {
	cc := c.codeCache
	if cc == nil {
		return
	}
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.refs > 0 {
		cc.refs--
	}
	if cc.refs == 0 && cc.closed && cc.mem != nil {
		_ = coreruntime.Unmap(cc.mem)
		cc.mem = nil
		cc.base = 0
	}
}

// Close releases the executable code mapping cached for this compiled module.
// Existing instances keep the mapping alive until they are closed; subsequent
// Instantiate calls fail. Closing is optional, but long-running hosts that create
// many Compiled modules should call it when the module is no longer needed.
func (c *Compiled) Close() error {
	if c == nil {
		return nil
	}
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.closed = true
	goruntime.SetFinalizer(c, nil)
	if cc.refs != 0 || cc.mem == nil {
		return nil
	}
	mem := cc.mem
	cc.mem = nil
	cc.base = 0
	return coreruntime.Unmap(mem)
}
