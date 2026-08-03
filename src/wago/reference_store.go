package wago

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/bits"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc"
)

// referenceStore owns public reference tokens. Runtime-created instances share
// one store; package-level Instantiate creates a private store lazily on the
// first non-null reference boundary, so scalar/null-only instances pay no store
// allocation. Externref objects live only in the Go-owned slots below; native
// code and mmap-backed Wasm state carry the generation-checked uint64 handle.
type referenceStore struct {
	mu sync.Mutex

	private                 bool
	runtimeClosed           bool
	domainSnapshotRestoring bool
	liveInstances           uint32
	liveObjects             uint32
	instances               map[*Instance]*referenceStoreInstance
	typeKeys                map[uint64]structuralTypeRegistration
	instanceTypes           map[*Instance][]uint64
	byIdentity              map[funcrefIdentity]*funcrefTokenEntry
	byToken                 map[uint64]*funcrefTokenEntry
	gcByToken               map[uint64]gcRefTokenEntry
	externKey               uint64
	externSeed              uint32
	externrefs              []externrefSlot
	gcDomains               *gcStoreDomain
}

// gcStoreDomain gives Runtime-owned WasmGC instances one compact-reference
// address space. Module-local type indexes are translated through canonical
// recursive structural identities before they enter this collector.
type gcStoreDomain struct {
	mu        sync.Mutex
	id        uint64
	collector *gc.Collector
	config    gc.Config
	types     []gc.TypeDesc
	typeReps  []gcDomainTypeRepresentative
	refs      uint32
	claims    uint32 // prepared instantiations not yet registered
	next      *gcStoreDomain
}

var nextGCDomainIdentity atomic.Uint64

func newGCDomainIdentity() uint64 {
	id := nextGCDomainIdentity.Add(1)
	if id == 0 {
		panic("wago: GC domain identity space exhausted")
	}
	return id
}

type referenceStoreInstance struct {
	closeAccounted    bool
	quiesced          bool
	resourcesReleased bool
	gcDomain          *gcStoreDomain
}

type structuralTypeRegistration struct {
	canonical []byte
	refs      uint32
}

type funcrefIdentity struct {
	descriptor uint64
	instance   *Instance
	localIdx   int
}

type funcrefTokenEntry struct {
	token      uint64
	descriptor uint64
	owner      *Instance
}

type gcRefTokenEntry struct {
	token      uint64
	ref        gc.Ref
	slot       uint32
	ownerIndex uint8
	exact      ValueTypeDescriptor // owner-local diagnostic identity
	domainType gc.TypeID           // canonical collector-domain identity
	owner      *Instance
}

const (
	gcNativeFrameLayoutAMD64 uint8 = iota
	gcNativeFrameLayoutARM64
)

type gcNativeFrameRoots struct {
	owner                *Instance
	base                 uintptr
	offsets              []uint32
	frameBytes           uint32
	frameLayout          uint8
	allowExternalReturn  bool
	codeBase             uintptr
	codeBytes            uintptr
	adapterReturnOffsets []uint32
	callsites            []compiledGCFrameCallsite
	suspended            *gcPublicState
}

type gcHostActivation struct {
	base         uintptr
	ctrl         uintptr
	callsite     uint32
	noFrame      bool
	savedControl gcHostSavedControl
}

const gcHostActivationLimit = 8

func (r *gcNativeFrameRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	r.walk(fn, nil)
}

func (r *gcNativeFrameRoots) RangeRootRefs(sink gc.RootRefSink) {
	r.walk(nil, sink)
}

func (r *gcNativeFrameRoots) walk(fn func(gc.RootSlot) bool, sink gc.RootRefSink) {
	if !r.rangeChain(fn, sink) {
		return
	}
	state := r.suspended
	if state != nil && state.hostRootPlan != nil {
		for i := int(state.hostActivationCount) - 1; i >= 0; i-- {
			activation := &state.hostActivations[i]
			if activation.noFrame {
				continue
			}
			if int(activation.callsite) >= len(state.hostRootPlan.callsites) {
				panic(gcStructHelperError{err: fmt.Errorf("generic GC host activation callsite %d is unavailable", activation.callsite)})
			}
			callsite := &state.hostRootPlan.callsites[activation.callsite]
			chain := gcNativeFrameRoots{
				owner:                r.owner,
				base:                 activation.base,
				offsets:              callsite.offsets,
				frameBytes:           callsite.frameBytes,
				frameLayout:          r.frameLayout,
				allowExternalReturn:  r.allowExternalReturn,
				codeBase:             state.hostCodeBase,
				codeBytes:            state.hostCodeBytes,
				adapterReturnOffsets: state.hostRootPlan.adapterReturnOffsets,
				callsites:            state.hostRootPlan.callsites,
			}
			if !chain.rangeChain(fn, sink) {
				return
			}
		}
	}
	domainRoots := false
	if r.owner != nil && r.owner.refStore != nil && r.owner.refStore.ownsGCCollector(r.owner.gc) {
		domainRoots = true
		if !r.owner.refStore.rangeGCDomainPersistentRoots(r.owner.gc, fn, sink) {
			return
		}
	}
	if !domainRoots && r.owner != nil {
		_ = r.owner.rangeLocalGCTableRoots(fn, sink)
	}
}

type gcNativeTableRoots struct {
	desc  uintptr
	bytes uintptr
}

func (r *gcNativeTableRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	r.walk(fn, nil)
}

func (r *gcNativeTableRoots) RangeRootRefs(sink gc.RootRefSink) {
	r.walk(nil, sink)
}

func (r *gcNativeTableRoots) walk(fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	if r == nil || r.desc == 0 || r.bytes < 8 {
		return true
	}
	header := unsafe.Slice((*byte)(offHeapPtr(r.desc)), 8)
	length := uint64(binary.LittleEndian.Uint32(header))
	if length > uint64((r.bytes-8)/8) {
		panic(gcStructHelperError{err: fmt.Errorf("generic GC table length %d exceeds descriptor capacity", length)})
	}
	for i := uint64(0); i < length; i++ {
		addr := r.desc + 8 + uintptr(i*8)
		word := binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(addr)), 8))
		if word != uint64(gc.Ref(uint32(word))) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC table root %d contains non-compact reference %#x", i, word)})
		}
		slot := (*gc.Root)(offHeapPtr(addr))
		if sink != nil {
			if !sink.VisitRootRef(slot.GetRef()) {
				return false
			}
		} else if !fn(slot) {
			return false
		}
	}
	return true
}

func (in *Instance) rangeLocalGCTableRoots(fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	if in == nil || in.c == nil {
		return true
	}
	for tableIndex := 0; tableIndex < in.c.tableCount(); tableIndex++ {
		if !isGCRefValType(in.c.tableElementType(tableIndex)) {
			continue
		}
		desc := in.tableDescriptor(tableIndex)
		if len(desc) < 8 {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC table %d descriptor is unavailable", tableIndex)})
		}
		roots := gcNativeTableRoots{desc: uintptr(unsafe.Pointer(&desc[0])), bytes: uintptr(len(desc))}
		if !roots.walk(fn, sink) {
			return false
		}
	}
	passiveBase := in.jm.PassiveElemPtr()
	for i := range in.c.passiveElems {
		elem := &in.c.passiveElems[i]
		if elem.Mode != ElemModePassive || !isGCRefValType(normalizedElemRefType(elem.RefType)) {
			continue
		}
		if passiveBase == 0 {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC passive element descriptor %d is unavailable", i)})
		}
		descAddr := passiveBase + uintptr(i*coreruntime.PassiveElemDescBytes)
		desc := unsafe.Slice((*byte)(offHeapPtr(descAddr)), coreruntime.PassiveElemDescBytes)
		entries := uintptr(binary.LittleEndian.Uint64(desc))
		length := uint64(binary.LittleEndian.Uint32(desc[8:]))
		if length > uint64(len(elem.Values)) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC passive element %d length %d exceeds %d", i, length, len(elem.Values))})
		}
		if length != 0 && entries == 0 {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC passive element %d entries are unavailable", i)})
		}
		for j := uint64(0); j < length; j++ {
			slot := (*gc.Root)(offHeapPtr(entries + uintptr(j*8)))
			if sink != nil {
				if !sink.VisitRootRef(slot.GetRef()) {
					return false
				}
			} else if !fn(slot) {
				return false
			}
		}
	}
	return true
}

func (r *gcNativeFrameRoots) rangeChain(fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	owner := r.owner
	base, offsets, frameBytes := r.base, r.offsets, r.frameBytes
	codeBase, codeBytes := r.codeBase, r.codeBytes
	adapterReturnOffsets, callsites := r.adapterReturnOffsets, r.callsites
	for depth := 0; ; depth++ {
		if depth > 4096 {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC native frame chain exceeds 4096 frames")})
		}
		for _, off := range offsets {
			// gc.Ref is the low 32 bits of the validated little-endian native qword.
			// Expose the actual off-heap word so a moving/reference-rewriting policy
			// updates the parked frame rather than a copied scratch value.
			slot := (*gc.Root)(offHeapPtr(base + uintptr(off)))
			if sink != nil {
				if !sink.VisitRootRef(slot.GetRef()) {
					return false
				}
			} else if !fn(slot) {
				return false
			}
		}
		if len(callsites) == 0 {
			return true
		}
		returnPCBias, callerFrameBias := uintptr(0), uintptr(abi.AMD64CallReturnAddressBytes)
		if r.frameLayout == gcNativeFrameLayoutARM64 {
			returnPCBias = uintptr(shared.ARM64SavedLROffset)
			callerFrameBias = uintptr(shared.ARM64FrameRecordBytes)
		}
		if base > ^uintptr(0)-uintptr(frameBytes)-returnPCBias {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC native frame address overflows")})
		}
		retWord := unsafe.Slice((*byte)(offHeapPtr(base+uintptr(frameBytes)+returnPCBias)), 8)
		retPC := uintptr(binary.LittleEndian.Uint64(retWord))
		if retPC < codeBase || retPC-codeBase >= codeBytes {
			if owner == nil || owner.refStore == nil || !owner.refStore.ownsGCCollector(owner.gc) {
				return true
			}
			foreign := owner.refStore.gcFrameOwner(retPC, owner.gc)
			if foreign == nil {
				if r.allowExternalReturn {
					return true
				}
				panic(gcStructHelperError{err: fmt.Errorf("generic GC foreign return PC %#x has no Runtime GC-domain owner", retPC)})
			}
			owner = foreign
			plan := foreign.c.genericGCFrameRoots()
			codeBase, codeBytes = foreign.base, uintptr(len(foreign.c.Code))
			adapterReturnOffsets, callsites = plan.adapterReturnOffsets, plan.callsites
		}
		rel := uint32(retPC - codeBase)
		for _, adapterReturn := range adapterReturnOffsets {
			if rel == adapterReturn {
				return true
			}
		}
		if base > ^uintptr(0)-uintptr(frameBytes)-callerFrameBias {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC caller frame address overflows")})
		}
		returnBase := base + uintptr(frameBytes) + callerFrameBias
		found := false
		var stackAdjust uint32
		for i := range callsites {
			if callsites[i].returnOffset == rel {
				offsets = callsites[i].offsets
				frameBytes = callsites[i].frameBytes
				stackAdjust = callsites[i].stackAdjust
				found = true
				break
			}
		}
		if !found {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC native return offset %d has no callsite map", rel)})
		}
		if returnBase > ^uintptr(0)-uintptr(stackAdjust) {
			panic(gcStructHelperError{err: fmt.Errorf("generic GC caller stack adjustment overflows")})
		}
		base = returnBase + uintptr(stackAdjust)
	}
}

const gcPublicSlotLimit = 64

// gcPublicState serializes public-token, generic helper, and boundary-collection
// access. Fixed result and argument slots bound host-held references while keeping
// token egress and ingress allocation-free after first use.
type gcPublicState struct {
	mu                sync.Mutex
	resultTokenCount  uint8
	resultRootsMade   uint8
	resultTokens      [gcPublicSlotLimit]uint64
	resultRootSlots   [gcPublicSlotLimit]uint32
	argumentRootCount uint8
	argumentRootsMade uint8
	argumentRootSlots [gcPublicSlotLimit]uint32
	cloneRootSlot     uint32
	cloneRootMade     bool
	// values is the bounded synchronous-helper constructor scratch. Collector
	// access is serialized by mu, so struct.new and array.new_fixed reuse it
	// without per-allocation Go heap traffic.
	values                [63]gc.Value
	initializerRoots      gc.InitializerWordRootScratch
	frameRoots            gcNativeFrameRoots    // exact parked native-frame roots; reused under mu
	globalRoots           []gcGlobalRootMapping // generic-GC safe-boundary roots; allocated only when needed
	hostActivations       [gcHostActivationLimit]gcHostActivation
	hostActivationCount   uint8
	hostArgumentRootSlots [gcHostActivationLimit][7]uint32
	hostArgumentRootsMade [gcHostActivationLimit]uint8
	hostArgumentRootCount [gcHostActivationLimit]uint8
	hostResultRootSlots   [gcHostActivationLimit][2]uint32
	hostResultRootsMade   [gcHostActivationLimit]uint8
	hostResultRootCount   [gcHostActivationLimit]uint8
	hostRootPlan          *compiledGCFrameRoots
	hostCodeBase          uintptr
	hostCodeBytes         uintptr
}

type externrefSlot struct {
	value      any
	generation uint32
}

func newReferenceStore(private bool) *referenceStore {
	return &referenceStore{private: private, runtimeClosed: private}
}

func (s *referenceStore) ownsGCCollector(collector *gc.Collector) bool {
	if s == nil || collector == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for domain := s.gcDomains; domain != nil; domain = domain.next {
		if domain.collector == collector {
			return true
		}
	}
	return false
}

func (s *referenceStore) gcDomainIdentity(collector *gc.Collector) uint64 {
	if s == nil || collector == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for domain := s.gcDomains; domain != nil; domain = domain.next {
		if domain.collector == collector {
			return domain.id
		}
	}
	return 0
}

func (s *referenceStore) lockGCCollector(collector *gc.Collector) *gcStoreDomain {
	if s == nil || collector == nil {
		return nil
	}
	s.mu.Lock()
	var found *gcStoreDomain
	for domain := s.gcDomains; domain != nil; domain = domain.next {
		if domain.collector == collector {
			found = domain
			break
		}
	}
	s.mu.Unlock()
	if found != nil {
		found.mu.Lock()
	}
	return found
}

func (in *Instance) lockGCCollector() *gcStoreDomain {
	if in == nil || in.refStore == nil {
		return nil
	}
	return in.refStore.lockGCCollector(in.gc)
}

func unlockGCCollector(domain *gcStoreDomain) {
	if domain != nil {
		domain.mu.Unlock()
	}
}

func (s *referenceStore) rangeGCDomainPersistentRoots(collector *gc.Collector, fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	if s == nil || collector == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for candidate, state := range s.instances {
		if state == nil || state.resourcesReleased || candidate == nil || candidate.gc != collector || candidate.c == nil {
			continue
		}
		for i, global := range candidate.globalCells {
			if global == nil || i >= len(candidate.c.Globals) || !isGCRefValType(candidate.c.Globals[i].Type) || len(global.cell) < 8 {
				continue
			}
			bits := binary.LittleEndian.Uint64(global.cell)
			ref := gc.Ref(uint32(bits))
			if bits != uint64(ref) {
				panic(gcStructHelperError{err: fmt.Errorf("Runtime GC-domain global %d contains non-compact reference %#x", i, bits)})
			}
			slot := (*gc.Root)(unsafe.Pointer(&global.cell[0]))
			if sink != nil {
				if !sink.VisitRootRef(slot.GetRef()) {
					return false
				}
			} else if !fn(slot) {
				return false
			}
		}
		if !candidate.rangeLocalGCTableRoots(fn, sink) {
			return false
		}
	}
	return true
}

func (s *referenceStore) gcFrameOwner(pc uintptr, collector *gc.Collector) *Instance {
	if s == nil || collector == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for candidate, state := range s.instances {
		if state == nil || state.resourcesReleased || candidate == nil || candidate.gc != collector || candidate.c == nil || candidate.c.genericGCFrameRoots() == nil {
			continue
		}
		if pc >= candidate.base && pc-candidate.base < uintptr(len(candidate.c.Code)) {
			return candidate
		}
	}
	return nil
}

func (s *referenceStore) releaseUnclaimedGCCollector(collector *gc.Collector) {
	if s == nil || collector == nil {
		return
	}
	s.mu.Lock()
	var prev *gcStoreDomain
	for domain := s.gcDomains; domain != nil; domain = domain.next {
		if domain.collector == collector {
			if domain.claims > 0 {
				domain.claims--
			}
			if domain.refs != 0 || domain.claims != 0 {
				s.mu.Unlock()
				return
			}
			if prev == nil {
				s.gcDomains = domain.next
			} else {
				prev.next = domain.next
			}
			s.mu.Unlock()
			collector.Close()
			return
		}
		prev = domain
	}
	s.mu.Unlock()
}

func (s *referenceStore) acquireGCCollector(config gc.Config, c *Compiled, preferred *gc.Collector, domainRestore bool) (*gc.Collector, *gcTypeMapping, error) {
	if s == nil || s.private {
		return nil, nil, fmt.Errorf("wago: shared WasmGC ownership requires an explicit Runtime")
	}
	s.mu.Lock()
	if s.runtimeClosed {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("wago: reference store is closed")
	}
	if s.domainSnapshotRestoring && !domainRestore {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("wago: Runtime GC domain restore is in progress")
	}
	var selected *gcStoreDomain
	if preferred != nil {
		for domain := s.gcDomains; domain != nil; domain = domain.next {
			if domain.collector == preferred {
				selected = domain
				break
			}
		}
		if selected == nil {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("wago: imported WasmGC collector is not a live Runtime domain")
		}
		if !reflect.DeepEqual(selected.config, config) {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("wago: WasmGC collector configuration is incompatible with the imported Runtime GC domain")
		}
	} else {
		for domain := s.gcDomains; domain != nil; domain = domain.next {
			if !gcModuleFitsDomain(c, domain) {
				continue
			}
			if !reflect.DeepEqual(domain.config, config) {
				s.mu.Unlock()
				return nil, nil, fmt.Errorf("wago: WasmGC collector configuration is incompatible with the matching Runtime GC domain")
			}
			selected = domain
			break
		}
	}
	if selected == nil {
		mapping, types, reps, err := gcCanonicalTypePlan(c, nil, nil, true)
		if err != nil {
			s.mu.Unlock()
			return nil, nil, err
		}
		collector, err := gc.NewCollector(config, types)
		if err != nil {
			s.mu.Unlock()
			return nil, nil, err
		}
		selected = &gcStoreDomain{id: newGCDomainIdentity(), collector: collector, config: config, types: types, typeReps: reps, claims: 1, next: s.gcDomains}
		s.gcDomains = selected
		s.mu.Unlock()
		return collector, mapping, nil
	}
	if selected.claims == ^uint32(0) {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("wago: Runtime GC domain has too many pending instances")
	}
	selected.claims++
	s.mu.Unlock()

	selected.mu.Lock()
	mapping, types, reps, err := gcCanonicalTypePlan(c, selected.typeReps, selected.types, preferred != nil)
	if err == nil && len(types) > len(selected.types) {
		err = selected.collector.AddTypes(types[len(selected.types):])
	}
	if err == nil {
		selected.types, selected.typeReps = types, reps
	}
	selected.mu.Unlock()
	if err != nil {
		s.releaseUnclaimedGCCollector(selected.collector)
		return nil, nil, err
	}
	return selected.collector, mapping, nil
}

func (s *referenceStore) registerInstance(in *Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosed && !s.private {
		return fmt.Errorf("wago: reference store is closed")
	}
	if in == nil || in.c == nil {
		return fmt.Errorf("wago: reference store instance has no compiled metadata")
	}
	if s.instances == nil {
		s.instances = make(map[*Instance]*referenceStoreInstance)
	}
	if _, exists := s.instances[in]; exists {
		return nil
	}
	if s.liveInstances == ^uint32(0) {
		return fmt.Errorf("wago: reference store has too many live instances")
	}

	// Resolve every fast-key equality against exact structural descriptors before
	// publishing any descriptor from this instance. Native calls may then compare
	// the compact key authoritatively: the store invariant guarantees that one key
	// never denotes two distinct live structural types.
	candidate := make(map[uint64]structuralTypeRegistration)
	keys := make([]uint64, 0, len(in.c.FuncTypeID))
	for i, key := range in.c.FuncTypeID {
		canonical, err := compiledStructuralCallIdentity(in.c, i)
		if err != nil {
			return fmt.Errorf("wago: function %d exact type: %w", i, err)
		}
		exact := structuralTypeRegistration{canonical: canonical}
		if prior, ok := candidate[key]; ok {
			if !bytes.Equal(prior.canonical, exact.canonical) {
				return fmt.Errorf("wago: native structural type key %#x collides within module", key)
			}
			continue
		}
		candidate[key] = exact
		keys = append(keys, key)
		if registered, ok := s.typeKeys[key]; ok && !bytes.Equal(registered.canonical, exact.canonical) {
			return fmt.Errorf("wago: native structural type key %#x collides with a distinct store type", key)
		}
	}
	for _, key := range keys {
		if registered, ok := s.typeKeys[key]; ok && registered.refs == ^uint32(0) {
			return fmt.Errorf("wago: native structural type key %#x has too many owners", key)
		}
	}
	if s.typeKeys == nil {
		s.typeKeys = make(map[uint64]structuralTypeRegistration)
		s.instanceTypes = make(map[*Instance][]uint64)
	}
	for _, key := range keys {
		registered, ok := s.typeKeys[key]
		if !ok {
			registered = candidate[key]
		}
		registered.refs++
		s.typeKeys[key] = registered
	}
	var domain *gcStoreDomain
	for candidate := s.gcDomains; candidate != nil; candidate = candidate.next {
		if candidate.collector == in.gc {
			domain = candidate
			if domain.refs == ^uint32(0) {
				return fmt.Errorf("wago: Runtime GC domain has too many instances")
			}
			if domain.claims == 0 {
				return fmt.Errorf("wago: Runtime GC domain registration has no prepared claim")
			}
			domain.claims--
			domain.refs++
			break
		}
	}
	s.instanceTypes[in] = keys
	s.instances[in] = &referenceStoreInstance{gcDomain: domain}
	s.liveInstances++
	return nil
}

func compiledFunctionSignature(c *Compiled, index int) (FuncSig, bool) {
	if c == nil || index < 0 {
		return FuncSig{}, false
	}
	if index < c.NumImports {
		if index >= len(c.importFuncSigs) {
			return FuncSig{}, false
		}
		return c.importFuncSigs[index], true
	}
	local := index - c.NumImports
	if local < 0 || local >= len(c.Funcs) {
		return FuncSig{}, false
	}
	return c.Funcs[local], true
}

func (s *referenceStore) releaseGCDomainLocked(entry *referenceStoreInstance) *gc.Collector {
	if entry == nil || entry.gcDomain == nil {
		return nil
	}
	domain := entry.gcDomain
	entry.gcDomain = nil
	if domain.refs > 0 {
		domain.refs--
	}
	if domain.refs != 0 || domain.claims != 0 {
		return nil
	}
	var prev *gcStoreDomain
	for candidate := s.gcDomains; candidate != nil; candidate = candidate.next {
		if candidate == domain {
			if prev == nil {
				s.gcDomains = candidate.next
			} else {
				prev.next = candidate.next
			}
			return domain.collector
		}
		prev = candidate
	}
	return nil
}

// abortRegisteredInstance terminates store membership for an instance whose
// instantiation failed after registration but before publication.
func (s *referenceStore) abortRegisteredInstance(in *Instance) {
	var release referenceTokenEntries
	var collector *gc.Collector
	s.mu.Lock()
	if entry := s.instances[in]; entry != nil {
		if !entry.closeAccounted && s.liveInstances > 0 {
			s.liveInstances--
		}
		collector = s.releaseGCDomainLocked(entry)
		s.unregisterInstanceTypesLocked(in)
		delete(s.instances, in)
	}
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	if collector != nil {
		collector.Close()
	}
	releaseReferenceEntries(release)
}

func (s *referenceStore) instanceClosed(in *Instance) {
	var release referenceTokenEntries
	s.mu.Lock()
	if entry := s.instances[in]; entry != nil {
		if !entry.closeAccounted {
			entry.closeAccounted = true
			if s.liveInstances > 0 {
				s.liveInstances--
			}
		}
		if entry.quiesced && entry.resourcesReleased {
			s.unregisterInstanceTypesLocked(in)
			delete(s.instances, in)
		}
	}
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	releaseReferenceEntries(release)
}

func (s *referenceStore) instanceQuiesced(in *Instance) {
	var release referenceTokenEntries
	s.mu.Lock()
	if entry := s.instances[in]; entry != nil {
		entry.quiesced = true
		if entry.closeAccounted && entry.resourcesReleased {
			s.unregisterInstanceTypesLocked(in)
			delete(s.instances, in)
		}
	}
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	releaseReferenceEntries(release)
}

func (s *referenceStore) resourceOwnerReleased(in *Instance) {
	var release referenceTokenEntries
	var collector *gc.Collector
	s.mu.Lock()
	if entry := s.instances[in]; entry != nil {
		entry.resourcesReleased = true
		collector = s.releaseGCDomainLocked(entry)
		if entry.closeAccounted && entry.quiesced {
			s.unregisterInstanceTypesLocked(in)
			delete(s.instances, in)
		}
	}
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	if collector != nil {
		collector.Close()
	}
	releaseReferenceEntries(release)
}

func (s *referenceStore) allClosedInstancesQuiescedLocked() bool {
	for _, entry := range s.instances {
		if entry.closeAccounted && !entry.quiesced {
			return false
		}
	}
	return true
}

func (s *referenceStore) canReleaseEntriesLocked() bool {
	return s.runtimeClosed && s.liveInstances == 0 && s.liveObjects == 0 && len(s.gcByToken) == 0 && s.allClosedInstancesQuiescedLocked()
}

func (s *referenceStore) maybeReleaseEntriesLocked() referenceTokenEntries {
	if !s.canReleaseEntriesLocked() {
		return referenceTokenEntries{}
	}
	return s.releaseEntriesLocked()
}

func (s *referenceStore) unregisterInstanceTypesLocked(in *Instance) {
	for _, key := range s.instanceTypes[in] {
		registered, ok := s.typeKeys[key]
		if !ok {
			continue
		}
		if registered.refs <= 1 {
			delete(s.typeKeys, key)
		} else {
			registered.refs--
			s.typeKeys[key] = registered
		}
	}
	delete(s.instanceTypes, in)
}

func (s *referenceStore) registerStoreObject() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosed {
		return fmt.Errorf("wago: reference store is closed")
	}
	if s.liveObjects == ^uint32(0) {
		return fmt.Errorf("wago: reference store has too many live objects")
	}
	s.liveObjects++
	return nil
}

const hostFuncRefDispatchBit = uint32(1 << 31)

func (s *referenceStore) registerHostFuncRef(owner *HostFuncRef) (uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosed {
		return 0, fmt.Errorf("wago: reference store is closed")
	}
	if s.liveObjects == ^uint32(0) || len(s.externrefs) >= int(hostFuncRefDispatchBit-1) {
		return 0, fmt.Errorf("wago: reference store has too many live objects")
	}
	s.liveObjects++
	s.externrefs = append(s.externrefs, externrefSlot{value: owner})
	return uint32(len(s.externrefs)), nil
}

func (s *referenceStore) hostFuncRef(dispatch uint32) *HostFuncRef {
	index := dispatch &^ hostFuncRefDispatchBit
	if dispatch&hostFuncRefDispatchBit == 0 || index == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if uint64(index) > uint64(len(s.externrefs)) {
		return nil
	}
	owner, _ := s.externrefs[index-1].value.(*HostFuncRef)
	return owner
}

func (s *referenceStore) storeObjectClosed() {
	var release referenceTokenEntries
	s.mu.Lock()
	if s.liveObjects > 0 {
		s.liveObjects--
	}
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	releaseReferenceEntries(release)
}

func (s *referenceStore) closeRuntime() {
	var release referenceTokenEntries
	s.mu.Lock()
	s.runtimeClosed = true
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	releaseReferenceEntries(release)
}

func (s *referenceStore) issue(source *Instance, descriptor uint64) (uint64, error) {
	return s.issueMode(source, descriptor, false)
}

// issueAttachedResult is restricted to result egress from a function import
// attachment. The caller's live attachment proves that a logically closed
// producer is still physically retained, so first-time token issuance may take
// over that lifetime with a finalization root.
func (s *referenceStore) issueAttachedResult(source *Instance, descriptor uint64) (uint64, error) {
	return s.issueMode(source, descriptor, true)
}

func (s *referenceStore) issueMode(source *Instance, descriptor uint64, attachedResult bool) (uint64, error) {
	if descriptor == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.byIdentity[funcrefIdentity{descriptor: descriptor}]; entry != nil {
		return entry.token, nil
	}
	if source == nil {
		return 0, fmt.Errorf("invalid funcref result descriptor")
	}
	owner, canonical, ok := s.canonicalFuncrefOwnerLocked(source, descriptor)
	if !ok {
		return 0, fmt.Errorf("invalid funcref result descriptor")
	}
	identity, hasIdentity := source.funcrefFunctionIdentity(descriptor)
	if hasIdentity {
		if entry := s.byIdentity[identity]; entry != nil {
			return entry.token, nil
		}
	}
	if entry := s.byIdentity[funcrefIdentity{descriptor: canonical}]; entry != nil {
		return entry.token, nil
	}
	var retained bool
	if attachedResult {
		retained = owner.retainResourceRootForFinalization()
	} else {
		retained = owner.retainResourceRoot()
	}
	if !retained {
		return 0, fmt.Errorf("funcref producer is closed")
	}
	token, err := s.newTokenLocked()
	if err != nil {
		owner.releaseResourceRoot()
		return 0, err
	}
	entry := &funcrefTokenEntry{token: token, descriptor: canonical, owner: owner}
	if s.byIdentity == nil {
		s.byIdentity = make(map[funcrefIdentity]*funcrefTokenEntry)
		s.byToken = make(map[uint64]*funcrefTokenEntry)
	}
	s.byIdentity[funcrefIdentity{descriptor: canonical}] = entry
	if hasIdentity {
		s.byIdentity[identity] = entry
	}
	s.byToken[token] = entry
	if hostOwner := owner.hostFuncRefForDescriptor(canonical); hostOwner != nil && !hostOwner.markTokenLive(owner, canonical) {
		delete(s.byIdentity, funcrefIdentity{descriptor: canonical})
		if hasIdentity {
			delete(s.byIdentity, identity)
		}
		delete(s.byToken, token)
		owner.releaseResourceRoot()
		return 0, fmt.Errorf("host funcref owner closed during token issue")
	}
	return token, nil
}

func (in *Instance) publicGCState() *gcPublicState {
	plugin := in.ensurePluginState()
	state := plugin.gcPublic.Load()
	if state == nil {
		candidate := &gcPublicState{}
		if plugin.gcPublic.CompareAndSwap(nil, candidate) {
			state = candidate
		} else {
			state = plugin.gcPublic.Load()
		}
	}
	return state
}

func (in *Instance) existingPublicGCState() *gcPublicState {
	if in == nil {
		return nil
	}
	plugin := in.pluginState.Load()
	if plugin == nil {
		return nil
	}
	return plugin.gcPublic.Load()
}

func (s *referenceStore) issueGCRef(source *Instance, ref gc.Ref, required ValueTypeDescriptor) (uint64, error) {
	if source == nil || ref.IsNull() || !ref.IsObj() {
		return 0, fmt.Errorf("invalid non-null GC result")
	}
	if source.c == nil {
		return 0, fmt.Errorf("public GC result ownership is outside exact collector execution")
	}
	arrayProduct := source.c.stagedGCArrayProduct()
	admittedArray := arrayProduct == stagedGCArrayProductNumericDefault || arrayProduct == stagedGCArrayProductNumericFixed || arrayProduct == stagedGCArrayProductPackedData || arrayProduct == stagedGCArrayProductReferenceElements || arrayProduct == stagedGCArrayProductNewData || arrayProduct == stagedGCArrayProductNewElem
	legacyStruct := source.c.stagedGCStructProduct() == stagedGCStructBasic
	generic := source.c.usesGenericGCExecution() && source.c.genericGCFrameRoots() != nil
	if !legacyStruct && !admittedArray && !generic {
		return 0, fmt.Errorf("public GC result ownership is outside exact collector execution")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := source.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state := source.publicGCState()
	state.mu.Lock()
	defer state.mu.Unlock()
	if int(state.resultTokenCount) >= len(state.resultTokens) {
		return 0, fmt.Errorf("public GC result token count exceeds %d", len(state.resultTokens))
	}
	ownerIndex := uint8(0)
	for ownerIndex < state.resultRootsMade && state.resultTokens[ownerIndex] != 0 {
		ownerIndex++
	}
	if int(ownerIndex) >= len(state.resultTokens) {
		return 0, fmt.Errorf("public GC result token count exceeds %d", len(state.resultTokens))
	}
	if source.gc == nil {
		return 0, fmt.Errorf("public GC result has no live collector")
	}
	typeID, err := source.gc.ObjectType(ref)
	if err != nil {
		return 0, fmt.Errorf("public GC result object %#x: %w", uint32(ref), err)
	}
	localType, ok := source.gcLocalType(typeID)
	if !ok || int(localType) >= len(source.c.Types) {
		return 0, fmt.Errorf("public GC result canonical type %d has no producer-local identity", typeID)
	}
	kind := source.c.Types[localType].Kind
	if legacyStruct && kind != CompositeTypeStruct {
		return 0, fmt.Errorf("public GC result type %d is not a struct", typeID)
	}
	if admittedArray && !generic && kind != CompositeTypeArray {
		return 0, fmt.Errorf("public GC result type %d is not an array", typeID)
	}
	if generic && kind != CompositeTypeStruct && kind != CompositeTypeArray {
		return 0, fmt.Errorf("public GC result type %d is not a struct or array", typeID)
	}
	exact := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{
		Exact: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: localType},
	}}
	if required.Kind != ValueTypeReference || !source.gcRefMatchesValueType(ref, required) {
		return 0, fmt.Errorf("public GC result type %d does not match its exact structural result type", localType)
	}

	s.mu.Lock()
	_, registered := s.instances[source]
	s.mu.Unlock()
	if !registered || !source.retainResourceRoot() {
		return 0, fmt.Errorf("public GC result producer is closed")
	}
	rollbackRoot := true
	defer func() {
		if rollbackRoot {
			source.releaseResourceRoot()
		}
	}()

	var slot uint32
	if ownerIndex == state.resultRootsMade {
		var slotErr error
		slot, slotErr = source.gc.NewCheckedGlobalSlot(ref)
		if slotErr != nil {
			return 0, fmt.Errorf("root public GC result: %w", slotErr)
		}
		state.resultRootSlots[ownerIndex] = slot
		state.resultRootsMade++
	} else {
		slot = state.resultRootSlots[ownerIndex]
		if err := source.gc.SetGlobalSlot(slot, ref); err != nil {
			return 0, fmt.Errorf("root public GC result: %w", err)
		}
	}

	s.mu.Lock()
	if _, registered = s.instances[source]; !registered {
		s.mu.Unlock()
		_ = source.gc.SetGlobalSlot(slot, gc.Null())
		return 0, fmt.Errorf("public GC result producer is closed")
	}
	token, err := s.newTokenLocked()
	if err == nil {
		if s.gcByToken == nil {
			s.gcByToken = make(map[uint64]gcRefTokenEntry)
		}
		s.gcByToken[token] = gcRefTokenEntry{token: token, ref: ref, slot: slot, ownerIndex: ownerIndex, exact: exact, domainType: typeID, owner: source}
		state.resultTokens[ownerIndex] = token
		state.resultTokenCount++
	}
	s.mu.Unlock()
	if err != nil {
		_ = source.gc.SetGlobalSlot(slot, gc.Null())
		return 0, err
	}
	rollbackRoot = false
	return token, nil
}

func (s *referenceStore) releaseGCRef(source *Instance, token uint64) error {
	if token == 0 {
		return nil
	}
	s.mu.Lock()
	entry, ok := s.gcByToken[token]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("invalid or stale GC reference token")
	}
	if source == nil || entry.owner != source {
		return fmt.Errorf("GC reference token belongs to a different producer or store")
	}
	state := source.existingPublicGCState()
	if state == nil {
		return fmt.Errorf("GC reference token owner state is unavailable")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := source.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state.mu.Lock()
	s.mu.Lock()
	entry, ok = s.gcByToken[token]
	ownerIndex := entry.ownerIndex
	if !ok || entry.owner != source || state.resultTokenCount == 0 || ownerIndex >= state.resultRootsMade || state.resultTokens[ownerIndex] != token || state.resultRootSlots[ownerIndex] != entry.slot {
		s.mu.Unlock()
		state.mu.Unlock()
		return fmt.Errorf("invalid or stale GC reference token")
	}
	if source.gc == nil {
		s.mu.Unlock()
		state.mu.Unlock()
		return fmt.Errorf("GC reference token collector is unavailable")
	}
	if err := source.gc.SetGlobalSlot(entry.slot, gc.Null()); err != nil {
		s.mu.Unlock()
		state.mu.Unlock()
		return fmt.Errorf("release GC reference token: %w", err)
	}
	delete(s.gcByToken, token)
	state.resultTokens[ownerIndex] = 0
	state.resultTokenCount--
	s.mu.Unlock()
	state.mu.Unlock()
	source.releaseResourceRoot()
	var release referenceTokenEntries
	s.mu.Lock()
	if s.runtimeClosed && s.liveInstances == 0 && s.liveObjects == 0 && len(s.gcByToken) == 0 {
		release = s.releaseEntriesLocked()
	}
	s.mu.Unlock()
	releaseReferenceEntries(release)
	return nil
}

func (s *referenceStore) gcRefExactType(token uint64) (ValueTypeDescriptor, *Instance, uint32, bool) {
	s.mu.Lock()
	entry, ok := s.gcByToken[token]
	s.mu.Unlock()
	if !ok {
		return ValueTypeDescriptor{}, nil, 0, false
	}
	return entry.exact, entry.owner, entry.slot, true
}

func lockGCPublicStates(a, b *gcPublicState) (first, second *gcPublicState) {
	if a == b {
		a.mu.Lock()
		return a, nil
	}
	first, second = a, b
	if uintptr(unsafe.Pointer(first)) > uintptr(unsafe.Pointer(second)) {
		first, second = second, first
	}
	first.mu.Lock()
	second.mu.Lock()
	return first, second
}

func unlockGCPublicStates(first, second *gcPublicState) {
	if second != nil {
		second.mu.Unlock()
	}
	first.mu.Unlock()
}

func (s *referenceStore) stageGCRefArgument(target *Instance, token uint64, required ValueTypeDescriptor) (gc.Ref, error) {
	if s == nil || target == nil || target.gc == nil || target.c == nil || token == 0 {
		return gc.Null(), fmt.Errorf("invalid GC reference token")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := target.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	s.mu.Lock()
	entry, ok := s.gcByToken[token]
	s.mu.Unlock()
	if !ok || entry.owner == nil {
		return gc.Null(), fmt.Errorf("invalid or stale GC reference token")
	}
	owner := entry.owner
	ownerState := owner.existingPublicGCState()
	if ownerState == nil {
		return gc.Null(), fmt.Errorf("GC reference token owner state is unavailable")
	}
	targetState := target.publicGCState()
	firstState, secondState := lockGCPublicStates(ownerState, targetState)
	defer unlockGCPublicStates(firstState, secondState)

	s.mu.Lock()
	current, ok := s.gcByToken[token]
	ownerRecord := s.instances[owner]
	targetRecord := s.instances[target]
	s.mu.Unlock()
	ownerIndex := current.ownerIndex
	if !ok || current.owner != owner || current.ref != entry.ref || current.slot != entry.slot || ownerIndex >= ownerState.resultRootsMade || ownerState.resultTokens[ownerIndex] != token || ownerState.resultRootSlots[ownerIndex] != entry.slot {
		return gc.Null(), fmt.Errorf("invalid or stale GC reference token")
	}
	if ownerRecord == nil || ownerRecord.resourcesReleased || targetRecord == nil || targetRecord.resourcesReleased || owner.refStore != s || target.refStore != s || owner.gc == nil || owner.gc != target.gc {
		return gc.Null(), fmt.Errorf("GC reference token belongs to a different collector domain")
	}
	if required.Kind != ValueTypeReference || !target.gcRefMatchesValueType(current.ref, required) {
		return gc.Null(), fmt.Errorf("GC reference token does not match the required structural argument type")
	}
	if int(targetState.argumentRootCount) >= len(targetState.argumentRootSlots) {
		return gc.Null(), fmt.Errorf("GC reference argument count exceeds %d", len(targetState.argumentRootSlots))
	}
	rootIndex := targetState.argumentRootCount
	if rootIndex == targetState.argumentRootsMade {
		slot, err := target.gc.NewCheckedGlobalSlot(current.ref)
		if err != nil {
			return gc.Null(), fmt.Errorf("root GC reference argument: %w", err)
		}
		targetState.argumentRootSlots[rootIndex] = slot
		targetState.argumentRootsMade++
	} else if err := target.gc.SetGlobalSlot(targetState.argumentRootSlots[rootIndex], current.ref); err != nil {
		return gc.Null(), fmt.Errorf("root GC reference argument: %w", err)
	}
	targetState.argumentRootCount++
	return current.ref, nil
}

func (in *Instance) rootGCHostArguments(token gcHostActivationToken, dispatch uint32, args []uint64) error {
	state := token.state
	if in == nil || state == nil || in.gc == nil || in.refStore == nil || dispatch&hostFuncRefDispatchBit == 0 {
		return nil
	}
	owner := in.refStore.hostFuncRef(dispatch)
	if owner == nil {
		return fmt.Errorf("GC host argument owner is unavailable")
	}
	owner.mu.Lock()
	if owner.gc == nil || owner.closed || owner.gc.collector != in.gc || owner.gc.domainID == 0 {
		owner.mu.Unlock()
		return fmt.Errorf("GC host argument owner is outside the active collector domain")
	}
	types := owner.sig.Params // immutable for the HostFuncRef lifetime
	owner.mu.Unlock()

	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state.mu.Lock()
	defer state.mu.Unlock()
	slot := 0
	for i, typ := range types {
		if typ == ValV128 {
			slot += 2
			continue
		}
		if slot >= len(args) {
			return fmt.Errorf("missing GC host argument slot %d", slot)
		}
		if typ == ValAnyRef || typ == ValI31Ref {
			bits := args[slot]
			ref := gc.Ref(uint32(bits))
			if bits != 0 && (uint64(ref) != bits || (!ref.IsObj() && !ref.IsI31())) {
				return fmt.Errorf("invalid compact GC host argument %d", i)
			}
			if ref.IsObj() {
				index := token.index
				count := state.hostArgumentRootCount[index]
				if int(count) >= len(state.hostArgumentRootSlots[index]) {
					return fmt.Errorf("GC host object argument count exceeds %d", len(state.hostArgumentRootSlots[index]))
				}
				if count == state.hostArgumentRootsMade[index] {
					root, err := in.gc.NewCheckedGlobalSlot(ref)
					if err != nil {
						return fmt.Errorf("root GC host argument %d: %w", i, err)
					}
					state.hostArgumentRootSlots[index][count] = root
					state.hostArgumentRootsMade[index]++
				} else if err := in.gc.SetGlobalSlot(state.hostArgumentRootSlots[index][count], ref); err != nil {
					return fmt.Errorf("root GC host argument %d: %w", i, err)
				}
				state.hostArgumentRootCount[index]++
			}
		}
		slot++
	}
	return nil
}

func (s *referenceStore) stageGCHostResult(target *Instance, ctrl uintptr, token uint64, required ValueTypeDescriptor) (gc.Ref, error) {
	if s == nil || target == nil || target.gc == nil || target.c == nil || ctrl == 0 || token == 0 {
		return gc.Null(), fmt.Errorf("invalid GC host result token")
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := target.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	s.mu.Lock()
	entry, ok := s.gcByToken[token]
	s.mu.Unlock()
	if !ok || entry.owner == nil {
		return gc.Null(), fmt.Errorf("invalid or stale GC host result token")
	}
	owner := entry.owner
	ownerState := owner.existingPublicGCState()
	if ownerState == nil {
		return gc.Null(), fmt.Errorf("GC host result token owner state is unavailable")
	}
	targetState := target.publicGCState()
	firstState, secondState := lockGCPublicStates(ownerState, targetState)
	defer unlockGCPublicStates(firstState, secondState)

	s.mu.Lock()
	current, ok := s.gcByToken[token]
	ownerRecord := s.instances[owner]
	targetRecord := s.instances[target]
	s.mu.Unlock()
	ownerIndex := current.ownerIndex
	if !ok || current.owner != owner || current.ref != entry.ref || current.slot != entry.slot || ownerIndex >= ownerState.resultRootsMade || ownerState.resultTokens[ownerIndex] != token || ownerState.resultRootSlots[ownerIndex] != entry.slot {
		return gc.Null(), fmt.Errorf("invalid or stale GC host result token")
	}
	if ownerRecord == nil || ownerRecord.resourcesReleased || targetRecord == nil || targetRecord.resourcesReleased || owner.refStore != s || target.refStore != s || owner.gc == nil || owner.gc != target.gc {
		return gc.Null(), fmt.Errorf("GC host result token belongs to a different collector domain")
	}
	if required.Kind != ValueTypeReference || !target.gcRefMatchesValueType(current.ref, required) {
		return gc.Null(), fmt.Errorf("GC host result token does not match the required structural result type")
	}
	activation := -1
	for i := int(targetState.hostActivationCount) - 1; i >= 0; i-- {
		if targetState.hostActivations[i].ctrl == ctrl {
			activation = i
			break
		}
	}
	if activation < 0 {
		return gc.Null(), fmt.Errorf("GC host result has no active parked frame")
	}
	count := targetState.hostResultRootCount[activation]
	if int(count) >= len(targetState.hostResultRootSlots[activation]) {
		return gc.Null(), fmt.Errorf("GC host result count exceeds %d", len(targetState.hostResultRootSlots[activation]))
	}
	if count == targetState.hostResultRootsMade[activation] {
		slot, err := target.gc.NewCheckedGlobalSlot(current.ref)
		if err != nil {
			return gc.Null(), fmt.Errorf("root GC host result: %w", err)
		}
		targetState.hostResultRootSlots[activation][count] = slot
		targetState.hostResultRootsMade[activation]++
	} else if err := target.gc.SetGlobalSlot(targetState.hostResultRootSlots[activation][count], current.ref); err != nil {
		return gc.Null(), fmt.Errorf("root GC host result: %w", err)
	}
	targetState.hostResultRootCount[activation]++
	return current.ref, nil
}

func (in *Instance) clearGCHostResultRoots(token gcHostActivationToken) {
	state := token.state
	if in == nil || state == nil || in.gc == nil || int(token.index) >= len(state.hostResultRootSlots) {
		return
	}
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state.mu.Lock()
	defer state.mu.Unlock()
	argumentCount := state.hostArgumentRootCount[token.index]
	for i := uint8(0); i < argumentCount; i++ {
		if err := in.gc.SetGlobalSlot(state.hostArgumentRootSlots[token.index][i], gc.Null()); err != nil {
			panic(gcStructHelperError{err: fmt.Errorf("clear GC host argument root %d: %w", i, err)})
		}
	}
	state.hostArgumentRootCount[token.index] = 0
	resultCount := state.hostResultRootCount[token.index]
	for i := uint8(0); i < resultCount; i++ {
		if err := in.gc.SetGlobalSlot(state.hostResultRootSlots[token.index][i], gc.Null()); err != nil {
			panic(gcStructHelperError{err: fmt.Errorf("clear GC host result root %d: %w", i, err)})
		}
	}
	state.hostResultRootCount[token.index] = 0
}

func (in *Instance) clearGCRefArgumentRoots() {
	state := in.existingPublicGCState()
	if state == nil || in.gc == nil {
		return
	}
	unlockNative := lockNativeExecutionForHostAccess()
	defer unlockNative()
	lockedDomain := in.lockGCCollector()
	defer unlockGCCollector(lockedDomain)
	state.mu.Lock()
	defer state.mu.Unlock()
	for i := uint8(0); i < state.argumentRootCount; i++ {
		if err := in.gc.SetGlobalSlot(state.argumentRootSlots[i], gc.Null()); err != nil {
			panic(gcStructHelperError{err: fmt.Errorf("clear GC reference argument root %d: %w", i, err)})
		}
	}
	state.argumentRootCount = 0
}

// ReleaseGCRef releases one non-null GC result token issued by this producer.
// It is valid after Instance.Close while the token retains the producer's
// collector; null releases are no-ops. Stale, foreign-store, and cross-producer
// tokens reject without changing either owner.
func (in *Instance) ReleaseGCRef(ref GCRef) error {
	if ref.token == 0 {
		return nil
	}
	if in == nil {
		return fmt.Errorf("release GC reference token on nil instance")
	}
	in.lifeMu.Lock()
	store := in.refStore
	in.lifeMu.Unlock()
	if store == nil {
		return fmt.Errorf("instance has no GC reference token store")
	}
	return store.releaseGCRef(in, ref.token)
}

func (s *referenceStore) resolveFuncrefTokenOwner(token uint64) (uint64, *Instance, bool) {
	if token == 0 {
		return 0, nil, true
	}
	s.mu.Lock()
	entry := s.byToken[token]
	s.mu.Unlock()
	if entry == nil || entry.owner == nil || !entry.owner.retainResourceRootForFinalization() {
		return 0, nil, false
	}
	return entry.descriptor, entry.owner, true
}

func (s *referenceStore) resolve(token uint64) (uint64, bool) {
	if token == 0 {
		return 0, true
	}
	s.mu.Lock()
	entry := s.byToken[token]
	s.mu.Unlock()
	if entry == nil {
		return 0, false
	}
	return entry.descriptor, true
}

func (s *referenceStore) tokenFuncrefExactType(token uint64) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	if token == 0 {
		return ValueTypeDescriptor{}, nil, false
	}
	s.mu.Lock()
	entry := s.byToken[token]
	var owner *Instance
	var descriptor uint64
	if entry != nil {
		owner, descriptor = entry.owner, entry.descriptor
	}
	s.mu.Unlock()
	return instanceFuncrefExactType(owner, descriptor)
}

func (s *referenceStore) descriptorFuncrefExactType(source *Instance, descriptor uint64) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	if source == nil || descriptor == 0 {
		return ValueTypeDescriptor{}, nil, false
	}
	s.mu.Lock()
	var owner *Instance
	canonical := descriptor
	ok := false
	if entry := s.byIdentity[funcrefIdentity{descriptor: descriptor}]; entry != nil {
		owner, canonical, ok = entry.owner, entry.descriptor, true
	} else {
		owner, canonical, ok = s.canonicalFuncrefOwnerLocked(source, descriptor)
	}
	s.mu.Unlock()
	if !ok {
		return ValueTypeDescriptor{}, nil, false
	}
	return instanceFuncrefExactType(owner, canonical)
}

func instanceFuncrefExactType(owner *Instance, descriptor uint64) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	if owner == nil || owner.c == nil || descriptor == 0 {
		return ValueTypeDescriptor{}, nil, false
	}
	index, ok := owner.funcrefDescriptorIndex(descriptor)
	if !ok {
		return ValueTypeDescriptor{}, nil, false
	}
	exact, err := owner.c.functionRefExactType(uint32(index))
	if err != nil {
		return ValueTypeDescriptor{}, nil, false
	}
	return exact, owner.c.Types, true
}

// attachedFuncrefExactType resolves function identities that are physically
// reachable from this instance without requiring a public reference store.
// This covers local descriptors and direct function imports, including imports
// whose refSlot points at a provider-owned canonical descriptor.
func (in *Instance) attachedFuncrefExactType(descriptor uint64) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	if in == nil || in.c == nil || descriptor == 0 {
		return ValueTypeDescriptor{}, nil, false
	}
	if index, ok := in.funcrefDescriptorIndex(descriptor); ok {
		return in.attachedFunctionIndexExactType(index)
	}
	for index := 0; index < in.c.NumImports && index < len(in.c.Imports); index++ {
		off := (index + 1) * coreruntime.FuncRefDescBytes
		if off+coreruntime.FuncRefDescBytes > len(in.funcRefDescs) || binary.LittleEndian.Uint64(in.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:]) != descriptor {
			continue
		}
		return in.attachedFunctionIndexExactType(index)
	}
	return ValueTypeDescriptor{}, nil, false
}

func (in *Instance) attachedFunctionIndexExactType(index int) (ValueTypeDescriptor, []DefinedTypeDescriptor, bool) {
	if index < 0 || index >= len(in.c.FuncTypeID) {
		return ValueTypeDescriptor{}, nil, false
	}
	if index >= in.c.NumImports {
		exact, err := in.c.functionRefExactType(uint32(index))
		return exact, in.c.Types, err == nil
	}
	if index >= len(in.c.Imports) {
		return ValueTypeDescriptor{}, nil, false
	}
	if export, ok := in.imports[in.c.Imports[index]].(*InstanceExport); ok && export != nil && export.inst != nil && export.inst.c != nil && export.localIdx >= 0 {
		providerIndex := export.inst.c.NumImports + export.localIdx
		exact, err := export.inst.c.functionRefExactType(uint32(providerIndex))
		return exact, export.inst.c.Types, err == nil
	}
	exact, err := in.c.functionRefExactType(uint32(index))
	return exact, in.c.Types, err == nil
}

func (s *referenceStore) issueExternref(value any) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosed && !s.private {
		return 0, fmt.Errorf("wago: reference store is closed")
	}
	if uint64(len(s.externrefs)) >= uint64(^uint32(0)) {
		return 0, fmt.Errorf("wago: externref store is full")
	}
	if s.externKey == 0 {
		key, err := randomNonzeroUint64()
		if err != nil {
			return 0, fmt.Errorf("create externref store key: %w", err)
		}
		s.externKey = key
		s.externSeed = uint32(key>>32) | 1
	}
	index := uint32(len(s.externrefs)) + 1
	generation := s.externSeed + index - 1
	if generation == 0 {
		generation = 1
	}
	for {
		raw := uint64(generation)<<32 | uint64(index)
		token := bits.RotateLeft64(raw^s.externKey, 17)
		if token != 0 {
			s.externrefs = append(s.externrefs, externrefSlot{value: value, generation: generation})
			return token, nil
		}
		generation++
		if generation == 0 {
			generation = 1
		}
	}
}

func (s *referenceStore) resolveExternref(token uint64) (any, bool) {
	if token == 0 {
		return nil, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.externKey == 0 {
		return nil, false
	}
	raw := bits.RotateLeft64(token, -17) ^ s.externKey
	index := uint32(raw)
	generation := uint32(raw >> 32)
	if index == 0 || uint64(index) > uint64(len(s.externrefs)) {
		return nil, false
	}
	slot := &s.externrefs[index-1]
	if slot.generation != generation {
		return nil, false
	}
	return slot.value, true
}

func (s *referenceStore) newTokenLocked() (uint64, error) {
	for {
		token, err := randomNonzeroUint64()
		if err != nil {
			return 0, fmt.Errorf("create reference token: %w", err)
		}
		_, gcExists := s.gcByToken[token]
		if token>>32 != 0 && s.byToken[token] == nil && !gcExists {
			return token, nil
		}
	}
}

func randomNonzeroUint64() (uint64, error) {
	var buf [8]byte
	for {
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, err
		}
		if token := binary.LittleEndian.Uint64(buf[:]); token != 0 {
			return token, nil
		}
	}
}

type referenceTokenEntries struct {
	funcrefs   []*funcrefTokenEntry
	gcRefs     []gcRefTokenEntry
	collectors []*gc.Collector
}

func (s *referenceStore) releaseEntriesLocked() referenceTokenEntries {
	var entries referenceTokenEntries
	if len(s.byToken) != 0 {
		entries.funcrefs = make([]*funcrefTokenEntry, 0, len(s.byToken))
		for _, entry := range s.byToken {
			entries.funcrefs = append(entries.funcrefs, entry)
		}
	}
	if len(s.gcByToken) != 0 {
		entries.gcRefs = make([]gcRefTokenEntry, 0, len(s.gcByToken))
		for _, entry := range s.gcByToken {
			entries.gcRefs = append(entries.gcRefs, entry)
		}
	}
	s.byIdentity = nil
	s.byToken = nil
	s.gcByToken = nil
	clear(s.externrefs)
	s.externrefs = nil
	s.externKey = 0
	s.externSeed = 0
	for domain := s.gcDomains; domain != nil; domain = domain.next {
		entries.collectors = append(entries.collectors, domain.collector)
	}
	s.gcDomains = nil
	return entries
}

func releaseReferenceEntries(entries referenceTokenEntries) {
	for _, entry := range entries.funcrefs {
		if hostOwner := entry.owner.hostFuncRefForDescriptor(entry.descriptor); hostOwner != nil {
			hostOwner.tokenReleased(entry.owner, entry.descriptor)
		}
		entry.owner.releaseResourceRoot()
	}
	for _, entry := range entries.gcRefs {
		state := entry.owner.existingPublicGCState()
		if state != nil {
			state.mu.Lock()
			ownerIndex := entry.ownerIndex
			if ownerIndex < state.resultRootsMade && state.resultTokens[ownerIndex] == entry.token && state.resultRootSlots[ownerIndex] == entry.slot {
				if entry.owner.gc != nil {
					_ = entry.owner.gc.SetGlobalSlot(entry.slot, gc.Null())
				}
				state.resultTokens[ownerIndex] = 0
				if state.resultTokenCount != 0 {
					state.resultTokenCount--
				}
			}
			state.mu.Unlock()
		}
		entry.owner.releaseResourceRoot()
	}
	for _, collector := range entries.collectors {
		collector.Close()
	}
}

func (s *referenceStore) hasInstanceResourcesLocked(in *Instance) bool {
	entry := s.instances[in]
	return entry != nil && !entry.resourcesReleased
}

// retainDescriptorOwnerForFinalization resolves a live descriptor owner and
// acquires one root while the store still proves its physical resources exist.
func (s *referenceStore) retainDescriptorOwnerForFinalization(descriptor uint64) *Instance {
	if s == nil || descriptor == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.byIdentity[funcrefIdentity{descriptor: descriptor}]; entry != nil {
		if s.hasInstanceResourcesLocked(entry.owner) && entry.owner.retainResourceRootForFinalization() {
			return entry.owner
		}
	}
	for candidate, state := range s.instances {
		if state.resourcesReleased || !candidate.reachesFuncrefDescriptor(descriptor) {
			continue
		}
		owner, canonical, ok := s.canonicalFuncrefOwnerLocked(candidate, descriptor)
		if !ok || canonical != descriptor || !s.hasInstanceResourcesLocked(owner) {
			continue
		}
		if owner.retainResourceRootForFinalization() {
			return owner
		}
	}
	return nil
}

func (s *referenceStore) canonicalFuncrefOwnerLocked(source *Instance, descriptor uint64) (*Instance, uint64, bool) {
	if fidx, ok := source.funcrefDescriptorIndex(descriptor); ok {
		if fidx >= source.c.NumImports {
			_, registered := s.instances[source]
			return source, descriptor, registered
		}
		if fidx >= len(source.c.Imports) || fidx >= len(source.c.importFuncSigs) {
			return nil, 0, false
		}
		key := source.c.Imports[fidx]
		off := (fidx + 1) * coreruntime.FuncRefDescBytes
		refSlot := binary.LittleEndian.Uint64(source.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:])
		if ex, ok := source.imports[key].(*InstanceExport); ok {
			if ex == nil || ex.inst == nil || ex.inst.refStore != s || ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
				return nil, 0, false
			}
			entry := source.funcRefDescs[off : off+coreruntime.FuncRefDescBytes]
			expectedCode := uint64(ex.inst.base) + uint64(ex.inst.c.Entry[ex.localIdx])
			home := binary.LittleEndian.Uint64(entry[coreruntime.TableEntryHomeLinMemOffset:])
			home &^= abi.FuncRefInternalHomeTag | abi.FuncRefCrossInstanceHomeTag | abi.FuncRefLocalWrapperHomeTag
			if binary.LittleEndian.Uint64(entry[coreruntime.TableEntryCodePtrOffset:]) != expectedCode ||
				home != uint64(ex.inst.jm.LinMemBase()) ||
				binary.LittleEndian.Uint64(entry[coreruntime.TableEntrySigKeyOffset:]) != source.c.funcTypeKey(fidx) ||
				binary.LittleEndian.Uint64(entry[coreruntime.FuncRefContextOffset:]) != uint64(ex.inst.nativeContext) {
				return nil, 0, false
			}
			canonical, hasCanonical := ex.inst.localFuncrefDescriptor(ex.localIdx)
			if !hasCanonical {
				// The producer never needed a descriptor arena itself. The importer's
				// exact proxy becomes the store identity; token retention keeps this
				// importer physically live, and its function attachment retains the
				// producer code/context until the token is released.
				if refSlot != descriptor {
					return nil, 0, false
				}
				_, registered := s.instances[source]
				return source, descriptor, registered
			}
			if refSlot != canonical {
				return nil, 0, false
			}
			if s.byIdentity[funcrefIdentity{descriptor: canonical}] != nil {
				return ex.inst, canonical, true
			}
			_, registered := s.instances[ex.inst]
			return ex.inst, canonical, registered
		}
		hostOwner, ok := source.imports[key].(*HostFuncRef)
		if !ok || hostOwner == nil || hostOwner.store != s || refSlot != descriptor {
			return nil, 0, false
		}
		entry := source.funcRefDescs[off : off+coreruntime.TableEntryBytes]
		home := binary.LittleEndian.Uint64(entry[coreruntime.TableEntryHomeLinMemOffset:])
		home &^= abi.FuncRefHomeTagMask
		if binary.LittleEndian.Uint64(entry[coreruntime.TableEntryCodePtrOffset:]) == 0 ||
			home != uint64(source.jm.LinMemBase()) ||
			binary.LittleEndian.Uint64(entry[coreruntime.TableEntrySigKeyOffset:]) != source.c.funcTypeKey(fidx) {
			return nil, 0, false
		}
		return hostOwner.canonicalDescriptor(source, descriptor, source.c.importFuncSigs[fidx])
	}
	for candidate := range s.instances {
		if candidate.ownsLocalFuncrefDescriptor(descriptor) {
			return candidate, descriptor, true
		}
	}
	return nil, 0, false
}

func (in *Instance) funcrefDescriptorIndex(descriptor uint64) (int, bool) {
	if len(in.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
		return 0, false
	}
	base := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[0])))
	if descriptor < base+coreruntime.FuncRefDescBytes || descriptor >= base+uint64(len(in.funcRefDescs)) {
		return 0, false
	}
	delta := descriptor - base
	if delta%coreruntime.FuncRefDescBytes != 0 {
		return 0, false
	}
	funcIndex := int(delta/coreruntime.FuncRefDescBytes) - 1
	return funcIndex, funcIndex >= 0 && funcIndex < len(in.c.FuncTypeID)
}

func (in *Instance) funcrefFunctionIdentity(descriptor uint64) (funcrefIdentity, bool) {
	fidx, ok := in.funcrefDescriptorIndex(descriptor)
	if !ok {
		return funcrefIdentity{}, false
	}
	if fidx >= in.c.NumImports {
		return funcrefIdentity{instance: in, localIdx: fidx - in.c.NumImports}, true
	}
	if fidx >= len(in.c.Imports) {
		return funcrefIdentity{}, false
	}
	export, ok := in.imports[in.c.Imports[fidx]].(*InstanceExport)
	if !ok || export == nil || export.inst == nil || export.localIdx < 0 {
		return funcrefIdentity{}, false
	}
	return funcrefIdentity{instance: export.inst, localIdx: export.localIdx}, true
}

func (in *Instance) ownsLocalFuncrefDescriptor(descriptor uint64) bool {
	funcIndex, ok := in.funcrefDescriptorIndex(descriptor)
	return ok && funcIndex >= in.c.NumImports
}

// reachesFuncrefDescriptor reports whether descriptor is represented in this
// instance's function-index descriptor space. Imported InstanceExport entries
// may reuse a producer's canonical refSlot, while bare producers and HostFuncRef
// bindings use importer-owned proxy slots. Retaining this instance preserves the
// already-established function/host attachment chain for every form.
func (in *Instance) reachesFuncrefDescriptor(descriptor uint64) bool {
	if in == nil || descriptor == 0 || len(in.funcRefDescs) < 2*coreruntime.FuncRefDescBytes {
		return false
	}
	for fidx := 0; fidx < len(in.c.FuncTypeID); fidx++ {
		off := (fidx + 1) * coreruntime.FuncRefDescBytes
		if off+coreruntime.FuncRefDescBytes > len(in.funcRefDescs) {
			return false
		}
		if binary.LittleEndian.Uint64(in.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:]) == descriptor {
			return true
		}
	}
	return false
}

func (in *Instance) hostFuncRefForDescriptor(descriptor uint64) *HostFuncRef {
	funcIndex, ok := in.funcrefDescriptorIndex(descriptor)
	if !ok || funcIndex < 0 || funcIndex >= in.c.NumImports || funcIndex >= len(in.c.Imports) {
		return nil
	}
	owner, _ := in.imports[in.c.Imports[funcIndex]].(*HostFuncRef)
	return owner
}

func (in *Instance) localFuncrefDescriptor(localIdx int) (uint64, bool) {
	fidx := in.c.NumImports + localIdx
	if localIdx < 0 || fidx < in.c.NumImports || fidx >= len(in.c.FuncTypeID) || len(in.funcRefDescs) == 0 {
		return 0, false
	}
	off := (fidx + 1) * coreruntime.FuncRefDescBytes
	if off+coreruntime.FuncRefDescBytes > len(in.funcRefDescs) {
		return 0, false
	}
	return uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[off]))), true
}

func (in *Instance) funcrefStoreForEgress() (*referenceStore, error) {
	return in.referenceStoreForBoundary()
}

// funcrefStoreForAttachedEgress returns the already-established store of a
// producer reached through a live function import attachment. It never creates
// a store after logical close: the attachment may preserve physical resources,
// but it cannot establish a new cross-instance token domain retroactively.
func (in *Instance) funcrefStoreForAttachedEgress() (*referenceStore, error) {
	if in == nil {
		return nil, fmt.Errorf("instance is nil")
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.resourcesClosed {
		return nil, fmt.Errorf("instance resources are closed")
	}
	if in.refStore == nil {
		return nil, fmt.Errorf("attached funcref producer has no compatible reference store")
	}
	return in.refStore, nil
}

// FuncRefMatchesFunction reports whether ref has the canonical identity of the
// function at index in this instance's Wasm function index space. It compares
// descriptor identity rather than opaque public token bits, so imported aliases
// and cross-instance references remain stable across store tokenization.
func (in *Instance) FuncRefMatchesFunction(ref FuncRef, index uint32) bool {
	if in == nil || ref.token == 0 {
		return false
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.closed || in.resourcesClosed || in.refStore == nil || int(index) >= len(in.c.FuncTypeID) {
		return false
	}
	descriptor, ok := in.refStore.resolve(ref.token)
	if !ok || descriptor == 0 {
		return false
	}
	actual := unsafe.Slice((*byte)(offHeapPtr(uintptr(descriptor))), coreruntime.TableEntryBytes)
	identity := binary.LittleEndian.Uint64(actual[coreruntime.TableEntryRefSlotOffset:])
	off := (int(index) + 1) * coreruntime.FuncRefDescBytes
	if identity == 0 || off < coreruntime.FuncRefDescBytes || off+coreruntime.FuncRefDescBytes > len(in.funcRefDescs) {
		return false
	}
	expected := binary.LittleEndian.Uint64(in.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:])
	return expected != 0 && identity == expected
}

func (in *Instance) referenceStoreForBoundary() (*referenceStore, error) {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.closed {
		return nil, fmt.Errorf("instance is closed")
	}
	if in.refStore == nil {
		store := newReferenceStore(true)
		if err := store.registerInstance(in); err != nil {
			return nil, err
		}
		in.refStore = store
	}
	return in.refStore, nil
}

func (in *Instance) retainResourceRoot() bool {
	return in.retainResourceRootMode(false)
}

func (in *Instance) retainResourceRootForFinalization() bool {
	return in.retainResourceRootMode(true)
}

func (in *Instance) retainResourceRootMode(finalization bool) bool {
	if in == nil {
		return false
	}
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.resourcesClosed || (!finalization && in.invocationState.Load()&instanceInvocationClosed != 0) || in.resourceRefs == maxInt() {
		return false
	}
	in.resourceRefs++
	return true
}

func (in *Instance) releaseResourceRoot() {
	if in == nil {
		return
	}
	in.lifeMu.Lock()
	if in.resourceRefs == 0 {
		in.lifeMu.Unlock()
		return
	}
	in.resourceRefs--
	closed := in.closed
	in.lifeMu.Unlock()
	if closed {
		in.tryFinalize()
	}
}

func (in *Instance) hasResourceRoots() bool {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	return in.resourceRefs != 0
}

func (in *Instance) hasPhysicalResources() bool {
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	return !in.resourcesClosed
}
