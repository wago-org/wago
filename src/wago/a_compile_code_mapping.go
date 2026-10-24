package wago

import (
	"sync/atomic"

	"errors"

	goruntime "runtime"

	"unsafe"
)

func newCompilerCompiled() *Compiled {
	owner := &compilerCompiledOwner{}
	owner.Compiled.nativeContextTrapABI = railshotNativeContextTrapABI()
	owner.Compiled.nativeTableContextABI = railshotNativeContextTrapABI()
	owner.Compiled.nativeTableReferenceABI = railshotNativeContextTrapABI()
	owner.Compiled.codeCache = &owner.state.codeCache
	owner.Compiled.validateMemo = &owner.state.validateMemo
	owner.Compiled.memoryDir = &owner.state.memoryDir
	return &owner.Compiled
}

// installCompilerCompiledFinalizer installs ownership on a freshly allocated
// compiler result whose zeroed cache and validation memo are already part of
// compilerCompiledState.
func installCompilerCompiledFinalizer(c *Compiled) *Compiled {
	c.ensureCodeCache()
	if c.validateMemo == nil {
		c.validateMemo = &validateMemo{}
	}
	goruntime.SetFinalizer(c, func(c *Compiled) {
		_ = c.Close()
	})
	return c
}

func (c *compiledCodeCache) setNativeStackBytes(stackBytes uint64) {
	if c == nil || stackBytes > uint64(^uint32(0)) {
		panic("wago: native stack capacity exceeds compact compile policy")
	}
	c.stagedFeatures = c.stagedFeatures&compiledStagedFeatureMask | CoreFeatures(stackBytes)<<32
}

func nativeGCMetadataABIMask() compiledGCMetadataFlags {
	if compiledArchitecture == 4 {
		return compiledGCMetadataNativeABIMask
	}
	return 0xfc
}

func (c *compiledCodeCache) setNativeGCABIVersion(version uint32) {
	mask := nativeGCMetadataABIMask()
	if c == nil || version > uint32(mask>>compiledGCMetadataNativeABIShift) {
		panic("wago: native GC ABI version exceeds compact metadata field")
	}
	c.gcMetadataFlags = c.gcMetadataFlags&^mask |
		compiledGCMetadataFlags(version<<compiledGCMetadataNativeABIShift)
}

// Raw pointer fields keep Compiled copyable; all lazy publication uses atomics.
// Compiler and codec construction may write them directly before publication.
func (c *Compiled) loadCodeCache() *compiledCodeCache {
	if c == nil {
		return nil
	}
	return (*compiledCodeCache)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&c.codeCache))))
}

func (c *Compiled) loadCompileIndexes() *compiledCacheIndexes {
	if c == nil {
		return nil
	}
	memo := c.loadValidateMemo()
	if memo == nil {
		return nil
	}
	return (*compiledCacheIndexes)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&memo.compileIndexes))))
}

func (c *Compiled) loadValidateMemo() *validateMemo {
	if c == nil {
		return nil
	}
	return (*validateMemo)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&c.validateMemo))))
}

func (c *Compiled) ensureCodeCache() {
	if c != nil && c.loadCodeCache() == nil {
		c.initializeCodeCache()
	}
}

// publishCompilerCompiled maps heap-backed parallel output before snapshotting.
// Both published views share one RW image; activation seals and registers it.
// Clear the embedded staging value:
// its allocation remains live through pointers to the grouped private state.
func publishCompilerCompiled(c *Compiled) (*Compiled, error) {
	// Exact-value interning is complete at publication. Its index is scratch,
	// not runtime metadata; retaining it would duplicate the immutable pool.
	if indexes := c.loadCompileIndexes(); indexes != nil {
		indexes.valueTypeIndex = nil
	}
	goruntime.SetFinalizer(c, nil)
	published := new(Compiled)
	*published = *c
	*c = Compiled{}
	// Keep the finalizer outside the cache/memo owner to avoid a finalizer cycle.
	goruntime.SetFinalizer(published, func(c *Compiled) { _ = c.Close() })
	if _, err := published.freezeExecution(0); err != nil {
		return nil, joinPrimary(err, published.Close())
	}
	return published, nil
}

func (c *Compiled) releaseHostThunksLocked() error {
	memo := c.loadValidateMemo()
	if memo == nil {
		return nil
	}
	var err error
	for i := range memo.hostThunks {
		cache := &memo.hostThunks[i]
		if cache.mem != nil {
			if cleanup := c.unmapProfileCode(cache.mem); cleanup != nil {
				err = errors.Join(err, cleanup)
				cache.base, cache.offsets = 0, nil
			} else {
				*cache = compiledHostThunkCache{}
			}
		}
	}
	return err
}

func (c *Compiled) releaseFailedMappingsLocked() error {
	indexes := c.loadCompileIndexes()
	if indexes == nil || len(indexes.failedMappings) == 0 {
		return nil
	}
	var err error
	retained := indexes.failedMappings[:0]
	for _, mem := range indexes.failedMappings {
		if cleanup := c.unmapProfileCode(mem); cleanup != nil {
			err = errors.Join(err, cleanup)
			retained = append(retained, mem)
		}
	}
	clear(indexes.failedMappings[len(retained):])
	indexes.failedMappings = retained
	return err
}

// clearCodeViewsLocked drops every slice header that can retain the staged or
// mapped code image. Callers hold codeCache.mu and have established refs == 0.
func (c *Compiled) clearCodeViewsLocked() {
	c.code = nil
	if memo := c.loadValidateMemo(); memo != nil {
		if snapshot := memo.executionView(); snapshot != nil {
			snapshot.code = nil
		}
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
	c.code = nil
	goruntime.SetFinalizer(c, nil)
	if cc.refs != 0 {
		return nil
	}
	c.clearCodeViewsLocked()
	if c.validateMemo != nil {
		c.validateMemo.structuralCallIdentities.Store(nil)
	}
	cc.base = 0
	var err error
	if cc.mem != nil {
		if cleanup := c.unmapProfileCode(cc.mem); cleanup != nil {
			err = cleanup
		} else {
			cc.mem = nil
		}
	}
	return errors.Join(err, c.releaseHostThunksLocked(), c.releaseFailedMappingsLocked(), railshotRetryNativeTail(c), c.releaseCompactDataLocked())
}
