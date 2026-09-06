package wago

import (
	"fmt"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

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
	sealed                 bool                         // guarded by mu; immutable flags remain race-free after publication
	gcMetadataFlags        compiledGCMetadataFlags      // collector-free markers plus native-GC ABI version
	gcTypeSubtypingProduct stagedGCTypeSubtypingProduct // exact first gc/type-subtyping no-object product; never serialized
	gcStructProduct        stagedGCStructProduct        // exact products stay compile-only; codec reload may restore generic helper admission
	gcArrayProduct         stagedGCArrayProduct         // exact products stay compile-only; codec reload may restore generic helper admission
	gcI31Product           stagedGCI31Product           // exact non-allocating i31 boundary; persisted as a semantic execution bit
	flags                  compiledCodeCacheFlags       // compact compile-only native dispatch and memory preferences
	// The low 32 bits are compile-only CoreFeatures. The high 32 bits retain the
	// direct-Instantiation native stack capacity without growing this sidecar.
	// Neither half is serialized; codec reload restores only generic features.
	stagedFeatures CoreFeatures
}

// compilerCompiledState groups the fixed private state owned for the complete
// lifetime of a compiler-produced Compiled. Compiled keeps pointers to all
// three fields, so the owner cannot become unreachable before the module does.
// Decoded and hand-built Compiled values retain their existing independent,
// lazy sidecar behavior.
type compilerCompiledState struct {
	codeCache    compiledCodeCache
	validateMemo validateMemo
	memoryDir    compiledMemoryDirectory
}

// compilerCompiledOwner groups the staging value and fixed private state in
// one allocation. Compiled is first for staging finalizer ownership. Publication
// moves the public view out and clears the staging value; private state stays.
type compilerCompiledOwner struct {
	Compiled
	state compilerCompiledState
}

func newCompilerCompiled(initial Compiled) *Compiled {
	owner := &compilerCompiledOwner{Compiled: initial}
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

func installCompiledFinalizer(c *Compiled) *Compiled {
	c.ensureCodeCache()
	// Give this compiler/deserialize-produced module its own validation memo so
	// Instantiate validates immutable compiler-produced metadata once. Preserve
	// codec-decoded immutable root and import-name sidecars while resetting the
	// validation result.
	var gcFrameRoots *compiledGCFrameRoots
	var importModuleEnds []uint64
	var snapshotLimit uint64
	if c.validateMemo != nil {
		gcFrameRoots = c.validateMemo.gcFrameRoots
		importModuleEnds = c.validateMemo.importModuleEnds
		snapshotLimit = c.validateMemo.snapshotLimit
	}
	c.validateMemo = &validateMemo{gcFrameRoots: gcFrameRoots, importModuleEnds: importModuleEnds, snapshotLimit: snapshotLimit}
	goruntime.SetFinalizer(c, func(c *Compiled) {
		_ = c.Close()
	})
	return c
}

func (c *Compiled) setGCRootAdmissionFailure(diagnostic string) {
	cc := c.loadCodeCache()
	if cc == nil || diagnostic == "" {
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
	case strings.Contains(diagnostic, "simultaneously live collector locals"):
		code = 7
	case strings.Contains(diagnostic, "local liveness"):
		code = 8
	case strings.Contains(diagnostic, "safepoint ID"):
		code = 9
	case strings.Contains(diagnostic, "unavailable on this build target"):
		code = 10
	}
	cc.flags = cc.flags&^compiledCacheGCRootFailureMask | code<<compiledCacheGCRootFailureShift
}

func (c *Compiled) gcRootAdmissionFailure() string {
	cc := c.loadCodeCache()
	if cc == nil {
		return "native root admission metadata is unavailable"
	}
	switch (cc.flags & compiledCacheGCRootFailureMask) >> compiledCacheGCRootFailureShift {
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
		return "a collecting function exceeds 1024 simultaneously live collector roots"
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
	cc := c.loadCodeCache()
	return cc != nil && cc.flags&compiledCacheDynamicFuncRefTest != 0
}

func (c *Compiled) usesAtomicWaitHelpers() bool {
	cc := c.loadCodeCache()
	return cc != nil && cc.flags&compiledCacheAtomicWaitHelpers != 0
}

func (c *Compiled) prefersGuardMemory() bool {
	cc := c.loadCodeCache()
	return cc != nil && cc.flags&compiledCacheGuardMemory != 0
}

const compiledStagedFeatureMask CoreFeatures = 1<<32 - 1

func (c *Compiled) stagedFeatures() CoreFeatures {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return cc.stagedFeatures & compiledStagedFeatureMask
}

func (c *compiledCodeCache) setNativeStackBytes(stackBytes uint64) {
	if c == nil || stackBytes > uint64(^uint32(0)) {
		panic("wago: native stack capacity exceeds compact compile policy")
	}
	c.stagedFeatures = c.stagedFeatures&compiledStagedFeatureMask | CoreFeatures(stackBytes)<<32
}

func (c *Compiled) nativeStackBytes() uint64 {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return uint64(cc.stagedFeatures >> 32)
}

func (c *Compiled) collectorFreeStructuralMetadata() bool {
	cc := c.loadCodeCache()
	return cc != nil && cc.gcMetadataFlags&compiledGCMetadataCollectorFreeStructural != 0
}

func (c *Compiled) stagedGCTypeSubtypingProduct() stagedGCTypeSubtypingProduct {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return cc.gcTypeSubtypingProduct
}

func (c *Compiled) usesGCStructHelpers() bool {
	return c != nil && c.stagedGCStructProduct().requiresHelpers()
}

func (c *Compiled) usesGCArrayHelpers() bool {
	return c != nil && (c.stagedGCStructProduct().requiresArrayHelpers() || c.stagedGCArrayProduct().requiresHelpers())
}

func (c *Compiled) collectorFreeGCArrayMetadata() bool {
	cc := c.loadCodeCache()
	return cc != nil && cc.gcMetadataFlags&compiledGCMetadataCollectorFreeArray != 0
}

func (c *Compiled) stagedGCStructProduct() stagedGCStructProduct {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return cc.gcStructProduct
}

func (c *Compiled) stagedGCArrayProduct() stagedGCArrayProduct {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return cc.gcArrayProduct
}

func (c *Compiled) usesGenericGCExecution() bool {
	if c == nil {
		return false
	}
	arrayProduct := c.stagedGCArrayProduct()
	return c.stagedGCStructProduct() == stagedGCStructGeneric || arrayProduct == stagedGCArrayProductNewData || arrayProduct == stagedGCArrayProductNewElem || arrayProduct == stagedGCArrayProductGeneric
}

func valueTypeTransfersCollectorObject(t ValueTypeDescriptor, types []DefinedTypeDescriptor) bool {
	if t.Kind != ValueTypeReference {
		return false
	}
	if t.Ref.Heap.Defined {
		if int(t.Ref.Heap.TypeIndex) >= len(types) {
			return true
		}
		kind := types[t.Ref.Heap.TypeIndex].Kind
		return kind == CompositeTypeStruct || kind == CompositeTypeArray
	}
	switch t.Ref.Heap.Abstract {
	case AbstractHeapAny, AbstractHeapEq, AbstractHeapStruct, AbstractHeapArray:
		return true
	default:
		return false
	}
}

func (c *Compiled) importTransfersCollectorObjects(index int) bool {
	if c == nil || index < 0 || index >= len(c.importFuncSigs) {
		return false
	}
	sig := c.importFuncSigs[index]
	params, results, err := exactFuncSignatureView(sig, c.Types)
	if err != nil || !sig.HasTypeIndex {
		return funcSigHasGCRefs(sig)
	}
	for _, typ := range params {
		if valueTypeTransfersCollectorObject(typ, c.Types) {
			return true
		}
	}
	for _, typ := range results {
		if valueTypeTransfersCollectorObject(typ, c.Types) {
			return true
		}
	}
	return false
}

// hasCollectorReferenceCallBoundary reports whether an imported function can
// transfer collector objects. Exact descriptors resolve indexed and recursive
// heap types in the importing module; raw module-local indexes are never used as
// cross-module identities.
func (c *Compiled) hasCollectorReferenceCallBoundary() bool {
	if c == nil {
		return false
	}
	for i := range c.importFuncSigs {
		if c.importTransfersCollectorObjects(i) {
			return true
		}
	}
	return false
}

// needsExactNativeGCRoots is the compile/artifact predicate. Allocating generic
// GC instructions and collector-reference host/cross-instance boundaries can
// both collect while native Wasm frames remain live.
func (c *Compiled) needsExactNativeGCRoots() bool {
	return c != nil && (c.usesGenericGCExecution() || c.hasCollectorReferenceCallBoundary())
}

// needsRuntimeGCCollectorDomain is the instantiated-module predicate. A module
// may need a collector solely because a Runtime-owned host import allocates or
// inspects values selected by the caller's exact GC types.
func (c *Compiled) needsRuntimeGCCollectorDomain() bool {
	return c != nil && (c.usesGenericGCExecution() || c.hasCollectorReferenceCallBoundary())
}

func (c *Compiled) needsNativeGCABI() bool {
	return c != nil && (c.needsExactNativeGCRoots() || c.usesGCStructHelpers() || c.usesGCArrayHelpers())
}

func (c *Compiled) nativeGCABIRequirement() uint32 {
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return uint32(cc.gcMetadataFlags&compiledGCMetadataNativeABIMask) >> compiledGCMetadataNativeABIShift
}

func (c *compiledCodeCache) setNativeGCABIVersion(version uint32) {
	if c == nil || version > uint32(compiledGCMetadataNativeABIMask>>compiledGCMetadataNativeABIShift) {
		panic("wago: native GC ABI version exceeds compact metadata field")
	}
	c.gcMetadataFlags = c.gcMetadataFlags&^compiledGCMetadataNativeABIMask |
		compiledGCMetadataFlags(version<<compiledGCMetadataNativeABIShift)
}

// compiledGCFrameRoots is the immutable codec admission sidecar for bounded
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

var importOnlyGCFrameRoots = compiledGCFrameRoots{}

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
	if c == nil {
		return nil
	}
	if memo := c.loadValidateMemo(); memo != nil && memo.gcFrameRoots != nil {
		return memo.gcFrameRoots
	}
	// A module with no local functions cannot have a parked native Wasm frame.
	// Its exact root set is therefore empty. Keep the collector-domain admission
	// for GC-bearing Runtime plugin imports without pretending a failed backend
	// root-plan build is a safety error.
	if len(c.Funcs) == 0 && c.hasCollectorReferenceCallBoundary() {
		return &importOnlyGCFrameRoots
	}
	return nil
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
	cc := c.loadCodeCache()
	if cc == nil {
		return 0
	}
	return cc.gcI31Product
}

// Raw pointer fields keep Compiled copyable; all lazy publication uses atomics.
// Compiler and codec construction may write them directly before publication.
func (c *Compiled) loadCodeCache() *compiledCodeCache {
	if c == nil {
		return nil
	}
	return (*compiledCodeCache)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&c.codeCache))))
}

func (c *Compiled) loadValidateMemo() *validateMemo {
	if c == nil {
		return nil
	}
	return (*validateMemo)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&c.validateMemo))))
}

// The cold coordinator avoids competing CAS writes after the winner has
// published a cache. Fixed compiler-owned caches never acquire this lock.
var compiledPublicationMu sync.Mutex

func (c *Compiled) ensureCodeCache() {
	if c != nil && c.loadCodeCache() == nil {
		c.initializeCodeCache()
	}
}

func (c *Compiled) initializeCodeCache() {
	compiledPublicationMu.Lock()
	defer compiledPublicationMu.Unlock()
	if c.loadCodeCache() == nil {
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&c.codeCache)), unsafe.Pointer(&compiledCodeCache{}))
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

// mapCodeLocked installs one exact-length readable view. Callers hold the cache
// lock; the compiler also calls this before publishing either metadata view.
func (c *Compiled) mapCodeLocked() error {
	cc := c.codeCache
	if cc.mem != nil {
		return nil
	}
	codeLen := len(c.code)
	image, err := coreruntime.NewCodeBuffer(codeLen)
	if err != nil {
		return err
	}
	defer image.Close()
	if err := image.Append(c.code); err != nil {
		return err
	}
	mem, base, err := image.Take()
	if err != nil {
		return err
	}
	cc.mem, cc.base = mem, base
	// The mapping can be page-rounded; code and artifact sizes must stay exact.
	c.code = mem[:codeLen:codeLen]
	return nil
}

// publishCompilerCompiled maps heap-backed parallel output before snapshotting.
// Both published views share one RW image; activation seals and registers it.
// Clear the embedded staging value:
// its allocation remains live through pointers to the grouped private state.
func publishCompilerCompiled(c *Compiled) (*Compiled, error) {
	if len(c.code) != 0 {
		c.codeCache.mu.Lock()
		err := c.mapCodeLocked()
		c.codeCache.mu.Unlock()
		if err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("compile: map code image: %w", err)
		}
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

func (c *Compiled) acquireCode() (uintptr, error) {
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed {
		return 0, fmt.Errorf("compiled module is closed")
	}
	if err := c.mapCodeLocked(); err != nil {
		return 0, err
	}
	if !cc.sealed {
		if err := coreruntime.SealCode(cc.mem); err != nil {
			return 0, err
		}
		cc.sealed = true
	}
	cc.refs++
	return cc.base, nil
}

func (c *Compiled) releaseCode() {
	cc := c.loadCodeCache()
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
	if cc.refs == 0 && cc.closed {
		if c.validateMemo != nil {
			c.validateMemo.structuralCallIdentities = nil
		}
	}
}

// replaceDecoded installs decoded state without orphaning an executable image.
// Reuse is allowed before instantiation, but a receiver with live instances
// remains their code owner and cannot be replaced.
func (c *Compiled) replaceDecoded(decoded Compiled) error {
	if cc := c.loadCodeCache(); cc != nil {
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
	_, err := c.freezeExecution(0)
	return err
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
	if c.validateMemo != nil {
		c.validateMemo.structuralCallIdentities = nil
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
