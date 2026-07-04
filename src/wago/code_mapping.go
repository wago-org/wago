package wago

import (
	"fmt"
	goruntime "runtime"
	"sync"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type compiledCodeCache struct {
	mu     sync.Mutex
	mem    []byte
	base   uintptr
	refs   int
	closed bool
}

func installCompiledFinalizer(c *Compiled) *Compiled {
	c.ensureCodeCache()
	// Give this compiler/deserialize-produced module its own validation memo so
	// Instantiate validates it once. A fresh memo (not the source's) is essential
	// for the link-time `linked := *c` copy, whose Code/Entry differ from c.
	c.validateMemo = &validateMemo{}
	goruntime.SetFinalizer(c, func(c *Compiled) {
		_ = c.Close()
	})
	return c
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
	// Release the memoized host-only linked module's mapping too (its finalizer
	// would eventually, but a caller closing c wants the code freed promptly). Live
	// instances keep it mapped via the code refcount until they close.
	if hl := c.hostLink; hl != nil && hl.c != nil {
		_ = hl.c.Close()
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
