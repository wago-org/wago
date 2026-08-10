package wago

import (
	"fmt"
	goruntime "runtime"
	"strings"
	"sync"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type compiledCodeCacheFlags uint8
type compiledGCMetadataFlags uint8

const (
	compiledCacheDynamicFuncRefTest compiledCodeCacheFlags = 1 << iota
	compiledCacheGuardMemory
	compiledCacheAtomicWaitHelpers
	compiledCacheWritableCode

	compiledCacheGCRootFailureShift = 4
	compiledCacheGCRootFailureMask  = compiledCodeCacheFlags(0xf0)
)

const (
	compiledGCMetadataCollectorFreeStructural compiledGCMetadataFlags = 1 << iota
	compiledGCMetadataCollectorFreeArray
	compiledGCMetadataNativeABIShift = 2
	compiledGCMetadataNativeABIMask  = compiledGCMetadataFlags(0xfc)
)

type compiledCodeCache struct {
	mu                     sync.Mutex
	mem                    []byte
	base                   uintptr
	refs                   int
	closed                 bool
	gcMetadataFlags        compiledGCMetadataFlags      // collector-free markers plus native-GC ABI version
	gcTypeSubtypingProduct stagedGCTypeSubtypingProduct // exact first gc/type-subtyping no-object product; never serialized
	gcStructProduct        stagedGCStructProduct        // exact products stay compile-only; codec v30 may restore generic helper admission
	gcArrayProduct         stagedGCArrayProduct         // exact products stay compile-only; codec v30 may restore generic helper admission
	gcI31Product           stagedGCI31Product           // exact non-allocating i31 boundary; never serialized
	flags                  compiledCodeCacheFlags       // compact compile-only native dispatch and memory preferences
	stagedFeatures         CoreFeatures                 // exact admission is compile-only; codec v30 restores generic GC requirements
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

func (c *Compiled) setGCRootAdmissionFailure(diagnostic string) {
	if c == nil || c.codeCache == nil || diagnostic == "" {
		return
	}
	code := compiledCodeCacheFlags(15)
	switch {
	case strings.Contains(diagnostic, "no local function bodies"):
		code = 1
	case strings.Contains(diagnostic, "imported start"):
		code = 2
	case strings.Contains(diagnostic, "table or element ownership"), strings.Contains(diagnostic, "global import"), strings.Contains(diagnostic, "table import"):
		code = 3
	case strings.Contains(diagnostic, "function import"), strings.Contains(diagnostic, "caller ABI"):
		code = 4
	case strings.Contains(diagnostic, "exception root"):
		code = 5
	case strings.Contains(diagnostic, "unsupported native call or frame"):
		code = 6
	case strings.Contains(diagnostic, "exceeds 1024 collector roots"):
		code = 7
	case strings.Contains(diagnostic, "local liveness"):
		code = 8
	case strings.Contains(diagnostic, "safepoint ID"):
		code = 9
	case strings.Contains(diagnostic, "unavailable on this build target"):
		code = 10
	}
	c.codeCache.flags = c.codeCache.flags&^compiledCacheGCRootFailureMask | code<<compiledCacheGCRootFailureShift
}

func (c *Compiled) gcRootAdmissionFailure() string {
	if c == nil || c.codeCache == nil {
		return "native root admission metadata is unavailable"
	}
	switch (c.codeCache.flags & compiledCacheGCRootFailureMask) >> compiledCacheGCRootFailureShift {
	case 1:
		return "generic GC module has no local function bodies"
	case 2:
		return "imported start function has an unknown host ownership graph"
	case 3:
		return "global, table, or element ownership is outside the exact native-root model"
	case 4:
		return "a function import or caller exceeds the exact native call ABI"
	case 5:
		return "exception payload roots could not be represented exactly"
	case 6:
		return "a collecting function contains an unsupported native call or frame shape"
	case 7:
		return "a collecting function exceeds 1024 collector roots or the frame-offset bound"
	case 8:
		return "exact structured-CFG local liveness could not be constructed"
	case 9:
		return "the module exceeds the dense native GC safepoint ID bound"
	case 10:
		return "exact native GC root maps are unavailable on this build target"
	case 15:
		return "native backend did not produce complete exact root maps"
	default:
		return "artifact contains no exact native root maps"
	}
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
	return c != nil && c.codeCache != nil && c.codeCache.gcMetadataFlags&compiledGCMetadataCollectorFreeStructural != 0
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
	return c != nil && c.codeCache != nil && c.codeCache.gcMetadataFlags&compiledGCMetadataCollectorFreeArray != 0
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

func (c *Compiled) nativeGCABIRequirement() uint32 {
	if c == nil || c.codeCache == nil {
		return 0
	}
	return uint32(c.codeCache.gcMetadataFlags&compiledGCMetadataNativeABIMask) >> compiledGCMetadataNativeABIShift
}

func (c *compiledCodeCache) setNativeGCABIVersion(version uint32) {
	if c == nil || version > uint32(compiledGCMetadataNativeABIMask>>compiledGCMetadataNativeABIShift) {
		panic("wago: native GC ABI version exceeds compact metadata field")
	}
	c.gcMetadataFlags = c.gcMetadataFlags&^compiledGCMetadataNativeABIMask |
		compiledGCMetadataFlags(version<<compiledGCMetadataNativeABIShift)
}

// compiledGCFrameRoots is the immutable codec-v33 admission sidecar for bounded
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

type gcFrameOffsetInterner struct {
	firstHash uint64
	first     []uint32
	others    map[uint64][]uint32
}

func gcFrameOffsetsHash(offsets []uint32) uint64 {
	h := uint64(1469598103934665603) ^ uint64(len(offsets))
	for _, off := range offsets {
		h ^= uint64(off)
		h *= 1099511628211
	}
	return h
}

// intern retains one immutable offset vector for repeated stack maps. Hash
// collisions only forgo sharing; equality is always checked before reuse.
func (i *gcFrameOffsetInterner) intern(offsets []uint32, clone bool) []uint32 {
	if len(offsets) == 0 {
		return nil
	}
	h := gcFrameOffsetsHash(offsets)
	equal := func(previous []uint32) bool {
		if len(previous) != len(offsets) {
			return false
		}
		for j := range offsets {
			if offsets[j] != previous[j] {
				return false
			}
		}
		return true
	}
	if h == i.firstHash && equal(i.first) {
		return i.first
	}
	if previous := i.others[h]; equal(previous) {
		return previous
	}
	if clone {
		offsets = append([]uint32(nil), offsets...)
	}
	if len(i.first) == 0 {
		i.firstHash, i.first = h, offsets
	} else if i.others == nil {
		i.others = map[uint64][]uint32{h: offsets}
	} else if _, exists := i.others[h]; !exists {
		i.others[h] = offsets
	}
	return offsets
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

func (c *Compiled) checkOpen() error {
	c.ensureCodeCache()
	c.codeCache.mu.Lock()
	defer c.codeCache.mu.Unlock()
	if c.codeCache.closed {
		return fmt.Errorf("compiled module is closed")
	}
	return nil
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
		codeLen := len(c.code)
		mem, base, err := coreruntime.MapCode(c.code)
		if err != nil {
			return 0, err
		}
		cc.mem, cc.base = mem, base
		// Keep one exact-length readable view of the machine code by moving Code
		// onto the RX mapping. MapCode's mmap slice is page-rounded; exposing that
		// padding would change codec bytes and binding-independent declarations.
		// The original Go-heap backing can now be reclaimed.
		c.code = mem[:codeLen:codeLen]
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
		c.code = nil
	}
}

// replaceDecoded installs decoded state without orphaning an executable image.
// Reuse is allowed before instantiation, but a receiver with live instances
// remains their code owner and cannot be replaced.
func (c *Compiled) replaceDecoded(decoded Compiled) error {
	if cc := c.codeCache; cc != nil {
		cc.mu.Lock()
		if cc.refs != 0 {
			cc.mu.Unlock()
			return fmt.Errorf("wago: cannot replace compiled module with %d live instance(s)", cc.refs)
		}
		mem := cc.mem
		cc.mem = nil
		cc.base = 0
		cc.closed = true
		c.code = nil
		cc.mu.Unlock()
		goruntime.SetFinalizer(c, nil)
		if mem != nil {
			if err := coreruntime.Unmap(mem); err != nil {
				return fmt.Errorf("release replaced compiled code: %w", err)
			}
		}
	}
	*c = decoded
	installCompiledFinalizer(c)
	return nil
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
	c.code = nil
	return coreruntime.Unmap(mem)
}
