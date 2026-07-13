package wago

import (
	"fmt"
	goruntime "runtime"
	"sync"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type compiledCodeCache struct {
	mu      sync.Mutex
	mem     []byte
	base    uintptr
	codeLen int // used code bytes; mem may include page rounding on Linux
	refs    int
	closed  bool
	sealed  bool
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
		cc.mem, cc.base, cc.codeLen = mem, base, len(c.Code)
	}
	cc.refs++
	return cc.base, nil
}

// SealCode adopts an immutable RX executable image and releases the public
// mutable Code backing slice. This is an explicit memory-saving transition:
// callers that inspect or mutate Compiled.Code, or serialize it, should keep
// the default representation or call MaterializeCode first. Existing and later
// instances execute directly from the sealed mapping without another code copy.
func (c *Compiled) SealCode() error {
	if c == nil {
		return fmt.Errorf("compiled module is nil")
	}
	if c.needsLink {
		return fmt.Errorf("cannot seal link-deferred compiled module")
	}
	if err := c.validateCached(); err != nil {
		return err
	}
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed {
		return fmt.Errorf("compiled module is closed")
	}
	if cc.mem == nil {
		mem, base, err := coreruntime.MapCode(c.Code)
		if err != nil {
			return err
		}
		cc.mem, cc.base, cc.codeLen = mem, base, len(c.Code)
	}
	c.Code = nil
	cc.sealed = true
	return nil
}

// adoptSealedCode transfers an already-RX arena into c. The arena mapping is
// page-rounded, while used is the exact machine-code length exposed by
// Footprint and serialization.
func (c *Compiled) adoptSealedCode(mem []byte, base uintptr, used int) {
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	cc.mem, cc.base, cc.codeLen, cc.sealed = mem, base, used, true
	cc.mu.Unlock()
	c.Code = nil
}

// MaterializeCode restores a mutable heap Code slice from a sealed executable
// image. MarshalBinary calls it automatically so sealing never removes the
// serialization compatibility path.
func (c *Compiled) MaterializeCode() error {
	if c == nil {
		return fmt.Errorf("compiled module is nil")
	}
	if c.Code != nil {
		return nil
	}
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if !cc.sealed {
		return fmt.Errorf("compiled module has no materialized code")
	}
	if cc.closed || cc.mem == nil {
		return fmt.Errorf("sealed compiled code is unavailable")
	}
	c.Code = append([]byte(nil), cc.mem[:cc.codeLen]...)
	cc.sealed = false
	return nil
}

func (c *Compiled) codeLen() int {
	if c == nil {
		return 0
	}
	if c.Code != nil {
		return len(c.Code)
	}
	cc := c.codeCache
	if cc == nil {
		return 0
	}
	cc.mu.Lock()
	n := 0
	if cc.sealed {
		n = cc.codeLen
	}
	cc.mu.Unlock()
	return n
}

func (c *Compiled) codeSealed() bool {
	if c == nil || c.codeCache == nil {
		return false
	}
	c.codeCache.mu.Lock()
	sealed := c.codeCache.sealed
	c.codeCache.mu.Unlock()
	return sealed
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
		cc.codeLen = 0
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
	if hl := c.hostLink; hl != nil {
		if hl.c != nil {
			_ = hl.c.Close()
		}
		if hl.syncC != nil && hl.syncC != hl.c {
			_ = hl.syncC.Close()
		}
		if hl.crossC != nil && hl.crossC != hl.c && hl.crossC != hl.syncC {
			_ = hl.crossC.Close()
		}
		hl.bodyStore.Close()
		hl.bodyStore = nil
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
	cc.codeLen = 0
	return coreruntime.Unmap(mem)
}
