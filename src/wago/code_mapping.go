package wago

import (
	"fmt"
	goruntime "runtime"
	"sync"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type compiledCodeCacheFlags uint8

const (
	compiledCacheDynamicFuncRefTest compiledCodeCacheFlags = 1 << iota
	compiledCacheGuardMemory
	compiledCacheAtomicWaitHelpers
	compiledCacheWritableCode
)

type compiledCodeCache struct {
	mu                              sync.Mutex
	mem                             []byte
	base                            uintptr
	refs                            int
	closed                          bool
	collectorFreeStructuralMetadata bool                         // exact staged products use struct descriptors only for function identity
	collectorFreeGCArrayMetadata    bool                         // exact staged array declaration/binding products allocate no collector
	gcTypeSubtypingProduct          stagedGCTypeSubtypingProduct // exact first gc/type-subtyping no-object product; never serialized
	gcStructProduct                 stagedGCStructProduct        // exact products stay compile-only; codec v30 may restore generic helper admission
	gcArrayProduct                  stagedGCArrayProduct         // exact products stay compile-only; codec v30 may restore generic helper admission
	gcI31Product                    stagedGCI31Product           // exact non-allocating i31 boundary; never serialized
	flags                           compiledCodeCacheFlags       // compact compile-only native dispatch and memory preferences
	stagedFeatures                  CoreFeatures                 // exact admission is compile-only; codec v30 restores generic GC requirements
}

func installCompiledFinalizer(c *Compiled) *Compiled {
	c.ensureCodeCache()
	// Give this compiler/deserialize-produced module its own validation memo so
	// Instantiate validates immutable compiler-produced metadata once. Preserve
	// a codec-decoded immutable native root map while resetting validation state.
	var gcFrameRoots *compiledGCFrameRoots
	if c.validateMemo != nil {
		gcFrameRoots = c.validateMemo.gcFrameRoots
	}
	c.validateMemo = &validateMemo{gcFrameRoots: gcFrameRoots}
	goruntime.SetFinalizer(c, func(c *Compiled) {
		_ = c.Close()
	})
	return c
}

func (c *Compiled) usesDynamicFuncRefTest() bool {
	return c != nil && c.codeCache != nil && c.codeCache.flags&compiledCacheDynamicFuncRefTest != 0
}

func (c *Compiled) usesAtomicWaitHelpers() bool {
	return c != nil && c.codeCache != nil && c.codeCache.flags&compiledCacheAtomicWaitHelpers != 0
}

func (c *Compiled) prefersGuardMemory() bool {
	return c != nil && c.codeCache != nil && c.codeCache.flags&compiledCacheGuardMemory != 0
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

func (c *Compiled) stagedGCTypeSubtypingProduct() stagedGCTypeSubtypingProduct {
	if c == nil || c.codeCache == nil {
		return 0
	}
	return c.codeCache.gcTypeSubtypingProduct
}

func (c *Compiled) usesGCStructHelpers() bool {
	return c != nil && c.stagedGCStructProduct().requiresHelpers()
}

func (c *Compiled) usesGCArrayHelpers() bool {
	return c != nil && (c.stagedGCStructProduct().requiresArrayHelpers() || c.stagedGCArrayProduct().requiresHelpers())
}

func (c *Compiled) collectorFreeGCArrayMetadata() bool {
	return c != nil && c.codeCache != nil && c.codeCache.collectorFreeGCArrayMetadata
}

func (c *Compiled) stagedGCStructProduct() stagedGCStructProduct {
	if c == nil || c.codeCache == nil {
		return 0
	}
	return c.codeCache.gcStructProduct
}

func (c *Compiled) stagedGCArrayProduct() stagedGCArrayProduct {
	if c == nil || c.codeCache == nil {
		return 0
	}
	return c.codeCache.gcArrayProduct
}

func (c *Compiled) usesGenericGCExecution() bool {
	if c == nil {
		return false
	}
	arrayProduct := c.stagedGCArrayProduct()
	return c.stagedGCStructProduct() == stagedGCStructGeneric || arrayProduct == stagedGCArrayProductNewData || arrayProduct == stagedGCArrayProductNewElem || arrayProduct == stagedGCArrayProductGeneric
}

// compiledGCFrameRoots is the immutable codec-v31 admission sidecar for bounded
// per-site roots, local call graphs, and suspended host activations. Generic modules without
// it remain collection-disabled during native execution.
type compiledGCFrameSafepoint struct {
	id         uint32
	frameBytes uint32
	offsets    []uint32
}

type compiledGCFrameCallsite struct {
	returnOffset uint32
	frameBytes   uint32
	stackAdjust  uint32
	offsets      []uint32
}

type compiledGCFrameRoots struct {
	adapterReturnOffsets []uint32
	safepoints           []compiledGCFrameSafepoint
	callsites            []compiledGCFrameCallsite
}

// safepointByID keeps the allocation-helper hot path O(1) for compiler-produced
// dense IDs while retaining a bounded binary-search fallback for valid sparse
// metadata loaded from older or externally produced codecs.
func (r *compiledGCFrameRoots) safepointByID(id uint32) *compiledGCFrameSafepoint {
	if r == nil || id == 0 {
		return nil
	}
	if index := uint64(id - 1); index < uint64(len(r.safepoints)) {
		safepoint := &r.safepoints[index]
		if safepoint.id == id {
			return safepoint
		}
	}
	low, high := 0, len(r.safepoints)
	for low < high {
		middle := low + (high-low)/2
		if r.safepoints[middle].id < id {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if low < len(r.safepoints) && r.safepoints[low].id == id {
		return &r.safepoints[low]
	}
	return nil
}

func (c *Compiled) genericGCFrameRoots() *compiledGCFrameRoots {
	if c == nil || c.validateMemo == nil {
		return nil
	}
	return c.validateMemo.gcFrameRoots
}

//lint:ignore U1000 retained for feature-gated GC global admission checks
func (c *Compiled) hasGCRefGlobals() bool {
	if c == nil {
		return false
	}
	for _, global := range c.Globals {
		if isGCRefValType(global.Type) {
			return true
		}
	}
	return false
}

// sharedGCPersistentDomainSafe admits same-Runtime collector-reference state and
// function-reference containers. Collector tables are scanned directly; funcref
// containers carry no compact GC roots, while descriptor-driven GC-bearing calls
// validate their immutable native domain identities before transfer.
func (c *Compiled) sharedGCPersistentDomainSafe() bool {
	if c == nil {
		return false
	}
	for i := 0; i < c.tableCount(); i++ {
		typ := c.tableElementType(i)
		if !isGCRefValType(typ) && typ != ValFuncRef {
			return false
		}
	}
	return true
}

func (c *Compiled) genericGCBoundaryCollectionSafe() bool {
	if c == nil || !c.usesGenericGCExecution() || c.HasTable || len(c.Elems) != 0 || len(c.passiveElems) != 0 {
		return false
	}
	return true
}

func (c *Compiled) stagedGCI31Product() stagedGCI31Product {
	if c == nil || c.codeCache == nil {
		return 0
	}
	return c.codeCache.gcI31Product
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
		codeLen := len(c.Code)
		mem, base, err := coreruntime.MapCode(c.Code)
		if err != nil {
			return 0, err
		}
		cc.mem, cc.base = mem, base
		// Keep one exact-length readable view of the machine code by moving Code
		// onto the RX mapping. MapCode's mmap slice is page-rounded; exposing that
		// padding would change codec bytes and binding-independent declarations.
		// The original Go-heap backing can now be reclaimed.
		c.Code = mem[:codeLen:codeLen]
	}
	if cc.flags&compiledCacheWritableCode != 0 {
		if err := coreruntime.SealCode(cc.mem); err != nil {
			return 0, err
		}
		cc.flags &^= compiledCacheWritableCode
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
		c.Code = nil
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
	if cc.refs != 0 {
		return nil
	}
	if cc.mem == nil {
		// Preserve compiler-produced metadata so a later Instantiate reaches the
		// authoritative closed check instead of failing earlier as malformed.
		return nil
	}
	mem := cc.mem
	cc.mem = nil
	cc.base = 0
	c.Code = nil
	return coreruntime.Unmap(mem)
}
