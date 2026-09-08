package wago

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/bits"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// referenceStore owns public reference tokens. Runtime-created instances share
// one store; package-level Instantiate creates a private store lazily on the
// first non-null reference boundary, so scalar/null-only instances pay no store
// allocation. Externref objects live only in the Go-owned slots below; native
// code and mmap-backed Wasm state carry the generation-checked uint64 handle.
type referenceStore struct {
	mu sync.Mutex

	private       bool
	runtimeClosed bool
	liveInstances uint32
	liveObjects   uint32
	instances     map[*Instance]*referenceStoreInstance
	typeKeys      map[uint64]structuralTypeRegistration
	instanceTypes map[*Instance][]uint64
	byIdentity    map[funcrefIdentity]*funcrefTokenEntry
	byToken       map[uint64]*funcrefTokenEntry
	gcByToken     map[uint64]gcRefTokenEntry
	externKey     uint64
	externSeed    uint32
	externFree    uint32
	externrefs    []externrefSlot
	gcDomains     *gcDomainTopology
}

// gcDomainTopology keeps the globally ordered Runtime GC-domain list stable
// across dynamic funcref invocations. It is a sidecar so scalar-only stores keep
// the established referenceStore footprint. Host callbacks suspend the read
// lease together with collector leases, allowing instantiation and teardown to
// update the list before native resume.
type gcDomainTopology struct {
	sync.RWMutex
	first *gcStoreDomain
	last  *gcStoreDomain
	n     int

	funcrefMu       sync.Mutex
	funcrefGCActive bool
	funcrefTables   map[*Table]uint32
}

// gcStoreDomain gives Runtime-owned WasmGC instances one compact-reference
// address space. Module-local type indexes are translated through canonical
// recursive structural identities before they enter this collector.
type gcStoreDomain struct {
	mu sync.Mutex
	// invocationMu covers the complete guest-call boundary, including compact
	// reference result tokenization after native code returns. Native execution
	// and helper locks alone leave a window where another tenant can collect an
	// as-yet-unrooted result from this shared collector. Arbitrary host callbacks
	// suspend this lease while exact parked roots remain published.
	invocationMu    sync.Mutex
	invocationState sync.Mutex
	invocationOwner invocationID
	id              uint64
	collector       *gc.Collector
	private         bool
	config          gc.Config
	types           []gc.TypeDesc
	typeReps        []gcDomainTypeRepresentative
	refs            uint32
	claims          uint32 // prepared instantiations not yet registered
	prev            *gcStoreDomain
	next            *gcStoreDomain
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
	// invocationDomains is allocated only when direct/transitive function imports
	// reach a GC domain other than this instance's own domain. The common local-GC
	// case uses gcDomain directly and pays no sidecar allocation.
	invocationDomains        *gcInvocationDomainSet
	dynamicInvocationDomains bool
}

const inlineGCInvocationDomains = 4

type gcInvocationDomainSet struct {
	inline [inlineGCInvocationDomains]*gcStoreDomain
	extra  []*gcStoreDomain
	n      int
}

func (s *gcInvocationDomainSet) len() int {
	if s == nil {
		return 0
	}
	return s.n
}

func (s *gcInvocationDomainSet) at(i int) *gcStoreDomain {
	if s == nil || i < 0 || i >= s.n {
		return nil
	}
	if i < len(s.inline) {
		return s.inline[i]
	}
	return s.extra[i-len(s.inline)]
}

func (s *gcInvocationDomainSet) set(i int, domain *gcStoreDomain) {
	if i < len(s.inline) {
		s.inline[i] = domain
		return
	}
	s.extra[i-len(s.inline)] = domain
}

func (s *gcInvocationDomainSet) add(domain *gcStoreDomain) {
	if domain == nil {
		return
	}
	for i := 0; i < s.n; i++ {
		if s.at(i) == domain {
			return
		}
	}
	if s.n < len(s.inline) {
		s.inline[s.n] = domain
	} else {
		s.extra = append(s.extra, domain)
	}
	s.n++
}

func (s *gcInvocationDomainSet) sort() {
	for i := 1; i < s.n; i++ {
		domain := s.at(i)
		j := i
		for j > 0 && s.at(j-1).id > domain.id {
			s.set(j, s.at(j-1))
			j--
		}
		s.set(j, domain)
	}
}

type gcInvocationDomainView struct {
	local   *gcStoreDomain
	set     *gcInvocationDomainSet
	first   *gcStoreDomain
	last    *gcStoreDomain
	n       int
	dynamic bool
}

type gcInvocationDomainIterator struct {
	topology *gcStoreDomain
	local    *gcStoreDomain
	reverse  bool
}

func (v gcInvocationDomainView) iterator(reverse bool) gcInvocationDomainIterator {
	it := gcInvocationDomainIterator{local: v.local, reverse: reverse}
	if reverse {
		it.topology = v.last
	} else {
		it.topology = v.first
	}
	return it
}

func (it *gcInvocationDomainIterator) next() *gcStoreDomain {
	if it.local != nil && (it.topology == nil || (!it.reverse && it.local.id < it.topology.id) || (it.reverse && it.local.id > it.topology.id)) {
		domain := it.local
		it.local = nil
		return domain
	}
	domain := it.topology
	if domain == nil {
		return nil
	}
	if it.reverse {
		it.topology = domain.prev
	} else {
		it.topology = domain.next
	}
	return domain
}

func (v gcInvocationDomainView) len() int {
	if v.dynamic {
		if v.local != nil {
			return v.n + 1
		}
		return v.n
	}
	if v.set != nil {
		return v.set.len()
	}
	if v.local != nil {
		return 1
	}
	return 0
}

func (v gcInvocationDomainView) at(i int) *gcStoreDomain {
	if v.dynamic {
		if i < 0 || i >= v.len() {
			return nil
		}
		it := v.iterator(false)
		var domain *gcStoreDomain
		for ; i >= 0; i-- {
			domain = it.next()
		}
		return domain
	}
	if v.set != nil {
		return v.set.at(i)
	}
	if i == 0 {
		return v.local
	}
	return nil
}

func (v gcInvocationDomainView) lock() {
	if v.dynamic {
		it := v.iterator(false)
		for domain := it.next(); domain != nil; domain = it.next() {
			domain.invocationMu.Lock()
		}
		return
	}
	for i := 0; i < v.len(); i++ {
		v.at(i).invocationMu.Lock()
	}
}

func (v gcInvocationDomainView) unlock() {
	if v.dynamic {
		it := v.iterator(true)
		for domain := it.next(); domain != nil; domain = it.next() {
			domain.invocationMu.Unlock()
		}
		return
	}
	for i := v.len() - 1; i >= 0; i-- {
		v.at(i).invocationMu.Unlock()
	}
}

func (v gcInvocationDomainView) ownedBy(owner invocationID) bool {
	if owner == 0 {
		return false
	}
	if v.dynamic {
		it := v.iterator(false)
		for domain := it.next(); domain != nil; domain = it.next() {
			domain.invocationState.Lock()
			owned := domain.invocationOwner == owner
			domain.invocationState.Unlock()
			if !owned {
				return false
			}
		}
		return true
	}
	for i := 0; i < v.len(); i++ {
		domain := v.at(i)
		domain.invocationState.Lock()
		owned := domain.invocationOwner == owner
		domain.invocationState.Unlock()
		if !owned {
			return false
		}
	}
	return true
}

func (v gcInvocationDomainView) claim(owner invocationID) {
	if v.dynamic {
		it := v.iterator(false)
		for domain := it.next(); domain != nil; domain = it.next() {
			domain.invocationState.Lock()
			if domain.invocationOwner != 0 {
				domain.invocationState.Unlock()
				panic("wago: occupied Runtime GC invocation lease")
			}
			domain.invocationOwner = owner
			domain.invocationState.Unlock()
		}
		return
	}
	for i := 0; i < v.len(); i++ {
		domain := v.at(i)
		domain.invocationState.Lock()
		if domain.invocationOwner != 0 {
			domain.invocationState.Unlock()
			panic("wago: occupied Runtime GC invocation lease")
		}
		domain.invocationOwner = owner
		domain.invocationState.Unlock()
	}
}

func (v gcInvocationDomainView) release(owner invocationID) {
	if v.dynamic {
		it := v.iterator(false)
		for domain := it.next(); domain != nil; domain = it.next() {
			domain.invocationState.Lock()
			if domain.invocationOwner != owner {
				domain.invocationState.Unlock()
				panic("wago: invalid Runtime GC invocation lease")
			}
			domain.invocationOwner = 0
			domain.invocationState.Unlock()
		}
		return
	}
	for i := 0; i < v.len(); i++ {
		domain := v.at(i)
		domain.invocationState.Lock()
		if domain.invocationOwner != owner {
			domain.invocationState.Unlock()
			panic("wago: invalid Runtime GC invocation lease")
		}
		domain.invocationOwner = 0
		domain.invocationState.Unlock()
	}
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
	ownerIndex uint32
	exact      ValueTypeDescriptor // owner-local diagnostic identity
	domainType gc.TypeID           // canonical collector-domain identity
	owner      *Instance
}

const (
	gcNativeFrameLayoutAMD64 uint8 = iota
	gcNativeFrameLayoutARM64
	gcNativeFrameLayoutMask      = 0x7f
	gcNativeFrameSyncGlobalRoots = 0x80
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

func (r *gcNativeFrameRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	return r.walk(nil, sink)
}

func (r *gcNativeFrameRoots) syncGlobalsBeforeCollection() {
	if r == nil || r.frameLayout&gcNativeFrameSyncGlobalRoots == 0 || r.owner == nil || r.suspended == nil {
		return
	}
	if err := r.owner.syncGenericGCGlobalRootsLocked(r.suspended); err != nil {
		panic(gcStructHelperError{err: err})
	}
	// Native execution remains parked for the complete allocation attempt. One
	// synchronization therefore covers a minor collection, root rewrites, and a
	// possible full-collection retry. The next helper republishes this flag.
	r.frameLayout &^= gcNativeFrameSyncGlobalRoots
}

// RangeClassifiedRootRefs preserves exact runtime ownership for opt-in
// collector telemetry without allocating composite RootSet values on helper
// paths.
func (r *gcNativeFrameRoots) RangeClassifiedRootRefs(sink gc.ClassifiedRootRefSink) bool {
	if r == nil || sink == nil {
		return true
	}
	r.syncGlobalsBeforeCollection()
	if !r.rangeChain(nil, classifiedRootSink{sink: sink, class: gc.RootNativeFrame}) {
		return false
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
			if !chain.rangeChain(nil, classifiedRootSink{sink: sink, class: gc.RootNativeFrame}) {
				return false
			}
		}
	}
	if r.owner != nil && r.owner.refStore != nil && r.owner.refStore.ownsGCCollector(r.owner.gc) {
		return r.owner.refStore.rangeGCDomainPersistentRootsClassified(r.owner.gc, sink)
	}
	if r.owner != nil {
		return r.owner.rangeLocalGCTableRoots(nil, classifiedRootSink{sink: sink, class: gc.RootTable})
	}
	return true
}

type classifiedRootSink struct {
	sink  gc.ClassifiedRootRefSink
	class gc.RootClass
}

func (s classifiedRootSink) VisitRootRef(r gc.Ref) bool {
	return s.sink.VisitClassifiedRootRef(s.class, r)
}

func (r *gcNativeFrameRoots) walk(fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	r.syncGlobalsBeforeCollection()
	if !r.rangeChain(fn, sink) {
		return false
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
				return false
			}
		}
	}
	domainRoots := false
	if r.owner != nil && r.owner.refStore != nil && r.owner.refStore.ownsGCCollector(r.owner.gc) {
		domainRoots = true
		if !r.owner.refStore.rangeGCDomainPersistentRoots(r.owner.gc, fn, sink) {
			return false
		}
	}
	if !domainRoots && r.owner != nil {
		return r.owner.rangeLocalGCTableRoots(fn, sink)
	}
	return true
}

type gcNativeTableRoots struct {
	desc  uintptr
	bytes uintptr
}

func (r *gcNativeTableRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	r.walk(fn, nil)
}

func (r *gcNativeTableRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	return r.walk(nil, sink)
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
		if r.frameLayout&gcNativeFrameLayoutMask == gcNativeFrameLayoutARM64 {
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
			codeBase, codeBytes = foreign.base, uintptr(len(foreign.c.code))
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
// access. The first 64 result and argument roots stay inline. Wider workloads use
// reusable overflow slices; released result slots remain holes and are reused
// without making stale opaque tokens valid again.
type gcPublicState struct {
	mu                     sync.Mutex
	resultTokenCount       uint32
	resultRootsMade        uint32
	resultTokens           [gcPublicSlotLimit]uint64
	resultTokensExtra      []uint64
	resultRootSlots        [gcPublicSlotLimit]uint32
	resultRootSlotsExtra   []uint32
	argumentRootCount      uint32
	argumentRootsMade      uint32
	argumentRootSlots      [gcPublicSlotLimit]uint32
	argumentRootSlotsExtra []uint32
	cloneRootSlot          uint32
	cloneRootMade          bool
	// values is the bounded synchronous-helper constructor scratch. Collector
	// access is serialized by mu, so struct.new and array.new_fixed reuse it
	// without per-allocation Go heap traffic.
	values                [63]gc.Value
	valuesExtra           []gc.Value
	initializerRoots      gc.InitializerWordRootScratch
	arrayInitializerRoots gc.ArrayInitializerRootScratch
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

func (s *gcPublicState) resultCapacity() uint32 {
	return gcPublicSlotLimit + uint32(len(s.resultTokensExtra))
}

func (s *gcPublicState) resultToken(index uint32) uint64 {
	if index < gcPublicSlotLimit {
		return s.resultTokens[index]
	}
	return s.resultTokensExtra[index-gcPublicSlotLimit]
}

func (s *gcPublicState) setResultToken(index uint32, token uint64) {
	if index < gcPublicSlotLimit {
		s.resultTokens[index] = token
		return
	}
	s.resultTokensExtra[index-gcPublicSlotLimit] = token
}

func (s *gcPublicState) resultRootSlot(index uint32) uint32 {
	if index < gcPublicSlotLimit {
		return s.resultRootSlots[index]
	}
	return s.resultRootSlotsExtra[index-gcPublicSlotLimit]
}

func (s *gcPublicState) appendResultRootSlot(slot uint32) uint32 {
	index := s.resultRootsMade
	if index < gcPublicSlotLimit {
		s.resultRootSlots[index] = slot
	} else {
		s.resultRootSlotsExtra = append(s.resultRootSlotsExtra, slot)
		s.resultTokensExtra = append(s.resultTokensExtra, 0)
	}
	s.resultRootsMade++
	return index
}

func (s *gcPublicState) nextResultSlot() uint32 {
	for index := uint32(0); index < s.resultRootsMade; index++ {
		if s.resultToken(index) == 0 {
			return index
		}
	}
	return s.resultRootsMade
}

func (s *gcPublicState) constructorValues(count uint32) ([]gc.Value, error) {
	if count <= uint32(len(s.values)) {
		return s.values[:count], nil
	}
	if uint64(count) > uint64(maxInt()) {
		return nil, fmt.Errorf("GC constructor value count %d overflows int", count)
	}
	if cap(s.valuesExtra) < int(count) {
		s.valuesExtra = make([]gc.Value, count)
	} else {
		s.valuesExtra = s.valuesExtra[:count]
		clear(s.valuesExtra)
	}
	return s.valuesExtra, nil
}

func (s *gcPublicState) argumentRootSlot(index uint32) uint32 {
	if index < gcPublicSlotLimit {
		return s.argumentRootSlots[index]
	}
	return s.argumentRootSlotsExtra[index-gcPublicSlotLimit]
}

func (s *gcPublicState) appendArgumentRootSlot(slot uint32) {
	if s.argumentRootsMade < gcPublicSlotLimit {
		s.argumentRootSlots[s.argumentRootsMade] = slot
	} else {
		s.argumentRootSlotsExtra = append(s.argumentRootSlotsExtra, slot)
	}
	s.argumentRootsMade++
}

type externrefSlot struct {
	value      any
	generation uint32
	nextFree   uint32
}

const externrefInternalSlot = ^uint32(0)

func newReferenceStore(private bool) *referenceStore {
	return &referenceStore{private: private, runtimeClosed: private}
}

func (s *referenceStore) ensureGCTopology() *gcDomainTopology {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.gcDomains == nil {
		s.gcDomains = new(gcDomainTopology)
	}
	topology := s.gcDomains
	s.mu.Unlock()
	return topology
}

func (topology *gcDomainTopology) bindFuncrefTables(store *referenceStore) error {
	for table := range topology.funcrefTables {
		if table == nil || table.owner == nil {
			continue
		}
		o := table.owner
		o.mu.Lock()
		foreign := o.hasForeignFuncrefStoreLocked(store)
		bound := o.funcrefGCStore
		if !foreign && (bound == nil || bound == store) {
			o.funcrefGCStore = store
		}
		o.mu.Unlock()
		if foreign || bound != nil && bound != store {
			return fmt.Errorf("wago: Runtime GC domains cannot share a mutable funcref table across reference stores")
		}
	}
	return nil
}

func (s *referenceStore) bindFuncrefGCStore() error {
	topology := s.ensureGCTopology()
	topology.funcrefMu.Lock()
	defer topology.funcrefMu.Unlock()
	if err := topology.bindFuncrefTables(s); err != nil {
		return err
	}
	topology.funcrefGCActive = true
	return nil
}

func (s *referenceStore) ownsGCCollector(collector *gc.Collector) bool {
	if s == nil || collector == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if topology := s.gcDomains; topology != nil {
		for domain := topology.first; domain != nil; domain = domain.next {
			if domain.collector == collector {
				return true
			}
		}
	}
	return false
}

func (s *referenceStore) gcDomainForCollector(collector *gc.Collector) *gcStoreDomain {
	if s == nil || collector == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if topology := s.gcDomains; topology != nil {
		for domain := topology.first; domain != nil; domain = domain.next {
			if domain.collector == collector {
				return domain
			}
		}
	}
	return nil
}

func (s *referenceStore) lockGCCollector(collector *gc.Collector) *gcStoreDomain {
	if s == nil || collector == nil {
		return nil
	}
	s.mu.Lock()
	var found *gcStoreDomain
	if topology := s.gcDomains; topology != nil {
		for domain := topology.first; domain != nil; domain = domain.next {
			if domain.collector == collector {
				found = domain
				break
			}
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

type gcInvocationLease struct {
	in       *Instance
	topology *gcDomainTopology
	domains  gcInvocationDomainView
	owner    invocationID
	acquired bool
	dynamic  bool
}

func (in *Instance) gcInvocationDomainsForInspection() (gcInvocationDomainView, *gcDomainTopology) {
	if in == nil || in.refStore == nil || in.executionFlags.Load()&executionFlagDynamicGCDomain == 0 {
		return in.gcInvocationDomains(), nil
	}
	in.refStore.mu.Lock()
	topology := in.refStore.gcDomains
	in.refStore.mu.Unlock()
	if topology != nil {
		topology.RLock()
	}
	return in.gcInvocationDomains(), topology
}

func (in *Instance) reachesPrivateGCInvocationDomain() bool {
	domains, topology := in.gcInvocationDomainsForInspection()
	if topology != nil {
		defer topology.RUnlock()
	}
	for i := 0; i < domains.len(); i++ {
		if domains.at(i).private {
			return true
		}
	}
	return false
}

func (in *Instance) gcInvocationDomain() *gcStoreDomain {
	if in == nil || in.refStore == nil || in.gc == nil {
		return nil
	}
	in.refStore.mu.Lock()
	entry := in.refStore.instances[in]
	var domain *gcStoreDomain
	if entry != nil {
		domain = entry.gcDomain
	}
	in.refStore.mu.Unlock()
	return domain
}

func (in *Instance) gcInvocationDomains() gcInvocationDomainView {
	if in == nil || in.refStore == nil || in.gc == nil && in.executionFlags.Load()&executionFlagImportedGCDomain == 0 {
		return gcInvocationDomainView{}
	}
	store := in.refStore
	store.mu.Lock()
	entry := store.instances[in]
	var domains gcInvocationDomainView
	if entry != nil {
		if entry.dynamicInvocationDomains {
			if entry.gcDomain != nil && entry.gcDomain.private {
				domains.local = entry.gcDomain
			}
			if topology := store.gcDomains; topology != nil {
				domains.first, domains.last, domains.n = topology.first, topology.last, topology.n
			}
			domains.dynamic = true
		} else {
			domains = gcInvocationDomainView{local: entry.gcDomain, set: entry.invocationDomains}
		}
	}
	store.mu.Unlock()
	return domains
}

func (in *Instance) lockGCInvocation(owner invocationID) gcInvocationLease {
	if owner == 0 {
		owner = newInvocationID()
	}
	dynamic := in != nil && in.refStore != nil && in.executionFlags.Load()&executionFlagDynamicGCDomain != 0
	var topology *gcDomainTopology
	if dynamic {
		in.refStore.mu.Lock()
		topology = in.refStore.gcDomains
		in.refStore.mu.Unlock()
		if topology == nil {
			panic("wago: dynamic Runtime GC invocation has no topology")
		}
		topology.RLock()
	}
	domains := in.gcInvocationDomains()
	if domains.len() == 0 {
		if dynamic {
			return gcInvocationLease{in: in, topology: topology, owner: owner, acquired: true, dynamic: true}
		}
		return gcInvocationLease{}
	}
	// A native cross-instance call reuses the public root's invocation identity.
	// The root pre-acquires the transitive, globally ordered domain set, so a
	// target reached through its retained imports borrows the already-held lease
	// instead of recursively locking the same non-reentrant mutexes.
	first := domains.at(0)
	first.invocationState.Lock()
	borrowed := first.invocationOwner == owner
	first.invocationState.Unlock()
	if borrowed {
		if !domains.ownedBy(owner) {
			panic("wago: partial Runtime GC invocation lease ownership")
		}
		if dynamic {
			topology.RUnlock()
		}
		return gcInvocationLease{in: in, topology: topology, domains: domains, owner: owner}
	}
	domains.lock()
	domains.claim(owner)
	return gcInvocationLease{in: in, topology: topology, domains: domains, owner: owner, acquired: true, dynamic: dynamic}
}

func (l gcInvocationLease) unlock() {
	if !l.acquired {
		return
	}
	domains := l.domains
	if l.dynamic {
		domains = l.in.gcInvocationDomains()
	}
	domains.release(l.owner)
	domains.unlock()
	if l.dynamic {
		l.topology.RUnlock()
	}
}

func (in *Instance) ownsGCInvocation(owner invocationID) bool {
	domain := in.gcInvocationDomain()
	if domain == nil || owner == 0 {
		return false
	}
	domain.invocationState.Lock()
	owned := domain.invocationOwner == owner
	domain.invocationState.Unlock()
	return owned
}

type gcInvocationSuspension struct {
	in       *Instance
	topology *gcDomainTopology
	domains  gcInvocationDomainView
	owner    invocationID
	dynamic  bool
	active   bool
}

func (in *Instance) suspendGCInvocation(owner invocationID) gcInvocationSuspension {
	dynamic := in != nil && in.refStore != nil && in.executionFlags.Load()&executionFlagDynamicGCDomain != 0
	var topology *gcDomainTopology
	if dynamic {
		in.refStore.mu.Lock()
		topology = in.refStore.gcDomains
		in.refStore.mu.Unlock()
		if topology == nil {
			panic("wago: dynamic Runtime GC invocation has no topology")
		}
	}
	domains := in.gcInvocationDomains()
	if owner == 0 || domains.len() == 0 && !dynamic {
		return gcInvocationSuspension{}
	}
	if !domains.ownedBy(owner) {
		if !domains.dynamic && domains.set == nil {
			// Start-function and construction callbacks can run before a public
			// invocation lease exists. Their native entry is already serialized, so
			// there is no complete-call lease to suspend or restore.
			return gcInvocationSuspension{}
		}
		panic("wago: partial Runtime GC invocation lease ownership")
	}
	domains.release(owner)
	domains.unlock()
	if dynamic {
		topology.RUnlock()
	}
	return gcInvocationSuspension{in: in, topology: topology, domains: domains, owner: owner, dynamic: dynamic, active: true}
}

func (s *gcInvocationSuspension) resume() {
	if s == nil || !s.active {
		return
	}
	s.active = false
	domains := s.domains
	if s.dynamic {
		s.topology.RLock()
		domains = s.in.gcInvocationDomains()
	}
	domains.lock()
	domains.claim(s.owner)
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

func (s *referenceStore) rangeGCDomainPersistentRootsClassified(collector *gc.Collector, sink gc.ClassifiedRootRefSink) bool {
	if s == nil || collector == nil || sink == nil {
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
			if !sink.VisitClassifiedRootRef(gc.RootGlobal, ref) {
				return false
			}
		}
		if !candidate.rangeLocalGCTableRoots(nil, classifiedRootSink{sink: sink, class: gc.RootTable}) {
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
		if pc >= candidate.base && pc-candidate.base < uintptr(len(candidate.c.code)) {
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
	topology := s.gcDomains
	s.mu.Unlock()
	if topology == nil {
		return
	}
	topology.Lock()
	defer topology.Unlock()
	s.mu.Lock()
	for domain := topology.first; domain != nil; domain = domain.next {
		if domain.collector == collector {
			if domain.claims > 0 {
				domain.claims--
			}
			if domain.refs != 0 || domain.claims != 0 {
				s.mu.Unlock()
				return
			}
			if domain.prev == nil {
				topology.first = domain.next
			} else {
				domain.prev.next = domain.next
			}
			if domain.next == nil {
				topology.last = domain.prev
			} else {
				domain.next.prev = domain.prev
			}
			domain.prev, domain.next = nil, nil
			if topology.n > 0 {
				topology.n--
			}
			s.mu.Unlock()
			collector.Close()
			return
		}
	}
	s.mu.Unlock()
}

func equalGCConfigs(a, b gc.Config) bool {
	// Telemetry is a diagnostic sink, not a heap-semantics parameter. A consumer
	// may join an existing Runtime domain without supplying the owner's recorder.
	a.Telemetry, b.Telemetry = nil, nil
	return a == b
}

func (s *referenceStore) acquireGCCollector(config gc.Config, c *Compiled, preferred *gc.Collector) (*gc.Collector, *gcTypeMapping, error) {
	if !gc.TelemetryAvailable() {
		config.Telemetry = nil
	}
	if s == nil || s.private {
		return nil, nil, fmt.Errorf("wago: shared WasmGC ownership requires an explicit Runtime")
	}
	topology := s.ensureGCTopology()
	topology.Lock()
	topologyLocked := true
	defer func() {
		if topologyLocked {
			topology.Unlock()
		}
	}()
	if err := s.bindFuncrefGCStore(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	if s.runtimeClosed {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("wago: reference store is closed")
	}
	var selected *gcStoreDomain
	if preferred != nil {
		for domain := topology.first; domain != nil; domain = domain.next {
			if domain.collector == preferred {
				selected = domain
				break
			}
		}
		if selected == nil {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("wago: imported WasmGC collector is not a live Runtime domain")
		}
		if !equalGCConfigs(selected.config, config) {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("wago: WasmGC collector configuration is incompatible with the imported Runtime GC domain")
		}
		if config.Telemetry != nil && selected.config.Telemetry != config.Telemetry {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("wago: WasmGC telemetry recorder does not own the imported Runtime GC domain")
		}
	} else {
		for domain := topology.first; domain != nil; domain = domain.next {
			if !gcModuleFitsDomain(c, domain) {
				continue
			}
			if !equalGCConfigs(domain.config, config) {
				s.mu.Unlock()
				return nil, nil, fmt.Errorf("wago: WasmGC collector configuration is incompatible with the matching Runtime GC domain")
			}
			if config.Telemetry != nil && domain.config.Telemetry != config.Telemetry {
				s.mu.Unlock()
				return nil, nil, fmt.Errorf("wago: WasmGC telemetry recorder does not own the matching Runtime GC domain")
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
		selected = &gcStoreDomain{id: newGCDomainIdentity(), collector: collector, config: config, types: types, typeReps: reps, claims: 1, prev: topology.last}
		if topology.last == nil {
			topology.first = selected
		} else {
			topology.last.next = selected
		}
		topology.last = selected
		topology.n++
		s.mu.Unlock()
		return collector, mapping, nil
	}
	if selected.claims == ^uint32(0) {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("wago: Runtime GC domain has too many pending instances")
	}
	selected.claims++
	s.mu.Unlock()
	topology.Unlock()
	topologyLocked = false

	// Native subtype readers hold invocationMu but do not enter Go or selected.mu.
	// Quiesce the whole domain before replacing and republishing the interval
	// backing, then follow the ordinary invocationMu -> mu lock order.
	selected.invocationMu.Lock()
	selected.mu.Lock()
	mapping, types, reps, err := gcCanonicalTypePlan(c, selected.typeReps, selected.types, preferred != nil)
	if err == nil && len(types) > len(selected.types) {
		err = selected.collector.AddTypes(types[len(selected.types):])
	}
	if err == nil {
		selected.types, selected.typeReps = types, reps
	}
	selected.mu.Unlock()
	selected.invocationMu.Unlock()
	if err != nil {
		s.releaseUnclaimedGCCollector(selected.collector)
		return nil, nil, err
	}
	return selected.collector, mapping, nil
}

func (s *referenceStore) registerInstance(in *Instance) error {
	var importedInvocationDomains gcInvocationDomainSet
	dynamicInvocationDomains := !s.private && in != nil && compiledHasDynamicFuncrefReachability(in.c)
	importsPrivateInvocationDomain := false
	if in != nil && in.c != nil {
		for _, key := range in.c.Imports {
			export, ok := in.imports[key].(*InstanceExport)
			if !ok || export == nil || export.inst == nil {
				continue
			}
			if export.inst.executionFlags.Load()&executionFlagDynamicGCDomain != 0 {
				if export.inst.refStore != s {
					return fmt.Errorf("wago: dynamic funcref import %q requires the same Runtime", key)
				}
				dynamicInvocationDomains = true
				continue
			}
			domains := export.inst.gcInvocationDomains()
			for i := 0; i < domains.len(); i++ {
				domain := domains.at(i)
				if domain.private {
					importsPrivateInvocationDomain = true
				}
				importedInvocationDomains.add(domain)
			}
		}
	}
	if dynamicInvocationDomains && importsPrivateInvocationDomain {
		return fmt.Errorf("wago: dynamic funcref imports cannot reach a private GC invocation domain")
	}
	if in != nil && in.c != nil {
		if err := in.c.prepareStructuralCallIdentities(); err != nil {
			return fmt.Errorf("wago: prepare structural call identities: %w", err)
		}
	}
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
	if dynamicInvocationDomains && s.gcDomains == nil {
		s.gcDomains = new(gcDomainTopology)
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
		canonical, cached := in.c.cachedStructuralCallIdentity(i)
		if !cached {
			var err error
			canonical, err = compiledStructuralCallIdentity(in.c, i)
			if err != nil {
				return fmt.Errorf("wago: function %d exact type: %w", i, err)
			}
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
	if topology := s.gcDomains; topology != nil {
		for candidate := topology.first; candidate != nil; candidate = candidate.next {
			if candidate.collector != in.gc {
				continue
			}
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
	if domain == nil && in.gc != nil {
		// Private collectors still need complete-call ownership through public
		// result tokenization. Keep their invocation domain out of the Runtime
		// topology so ownership, root scanning, and collector lifetime remain local.
		domain = &gcStoreDomain{id: newGCDomainIdentity(), collector: in.gc, private: true}
	}
	invocationDomains := importedInvocationDomains
	invocationDomains.add(domain)
	invocationDomains.sort()
	var storedInvocationDomains *gcInvocationDomainSet
	if !dynamicInvocationDomains && invocationDomains.len() != 0 && (invocationDomains.len() != 1 || invocationDomains.at(0) != domain) {
		storedInvocationDomains = new(gcInvocationDomainSet)
		*storedInvocationDomains = invocationDomains
	}
	flagsToAdd := uint32(0)
	if domain != nil && !domain.private {
		flagsToAdd |= executionFlagStoreOwnedGCCollector
	}
	if dynamicInvocationDomains || storedInvocationDomains != nil {
		flagsToAdd |= executionFlagImportedGCDomain
		if dynamicInvocationDomains {
			flagsToAdd |= executionFlagDynamicGCDomain
		}
	}
	for flagsToAdd != 0 {
		flags := in.executionFlags.Load()
		if flags&flagsToAdd == flagsToAdd || in.executionFlags.CompareAndSwap(flags, flags|flagsToAdd) {
			break
		}
	}
	s.instanceTypes[in] = keys
	s.instances[in] = &referenceStoreInstance{
		gcDomain: domain, invocationDomains: storedInvocationDomains,
		dynamicInvocationDomains: dynamicInvocationDomains,
	}
	s.liveInstances++
	return nil
}

func compiledHasDynamicFuncrefReachability(c *Compiled) bool {
	if c == nil {
		return false
	}
	// A funcref table or global can acquire a cross-instance descriptor after
	// instantiation. Conservatively lease every live Runtime GC domain for these
	// container-bearing modules; ordinary ref.func and direct-import modules keep
	// their compact statically discovered domain sets.
	if c.hasFuncrefTable() {
		return true
	}
	for i := range c.Globals {
		if c.Globals[i].Type == ValFuncRef {
			return true
		}
	}
	if c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
		for i := range c.importFuncSigs {
			if funcSigHasFuncref(c.importFuncSigs[i]) {
				return true
			}
		}
		for i := range c.Funcs {
			if funcSigHasFuncref(c.Funcs[i]) {
				return true
			}
		}
	}
	return false
}

func funcSigHasFuncref(sig FuncSig) bool {
	for _, typ := range sig.Params {
		if typ == ValFuncRef {
			return true
		}
	}
	for _, typ := range sig.Results {
		if typ == ValFuncRef {
			return true
		}
	}
	return false
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
	if domain.private {
		return nil
	}
	if domain.refs > 0 {
		domain.refs--
	}
	if domain.refs != 0 || domain.claims != 0 {
		return nil
	}
	topology := s.gcDomains
	if topology == nil {
		return nil
	}
	for candidate := topology.first; candidate != nil; candidate = candidate.next {
		if candidate == domain {
			if candidate.prev == nil {
				topology.first = candidate.next
			} else {
				candidate.prev.next = candidate.next
			}
			if candidate.next == nil {
				topology.last = candidate.prev
			} else {
				candidate.next.prev = candidate.prev
			}
			candidate.prev, candidate.next = nil, nil
			if topology.n > 0 {
				topology.n--
			}
			return domain.collector
		}
	}
	return nil
}

func (s *referenceStore) lockGCDomainTopology() *gcDomainTopology {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	topology := s.gcDomains
	s.mu.Unlock()
	if topology != nil {
		topology.Lock()
	}
	return topology
}

func (s *referenceStore) lockGCDomainTopologyForInstance(in *Instance) *gcDomainTopology {
	if in == nil || in.gc == nil {
		return nil
	}
	return s.lockGCDomainTopology()
}

// abortRegisteredInstance terminates store membership for an instance whose
// instantiation failed after registration but before publication.
func (s *referenceStore) abortRegisteredInstance(in *Instance) {
	var release referenceTokenEntries
	var collector *gc.Collector
	topology := s.lockGCDomainTopologyForInstance(in)
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
	if topology != nil {
		topology.Unlock()
	}
	if collector != nil {
		collector.Close()
	}
	releaseReferenceEntries(release)
}

func (s *referenceStore) advanceInstanceLifetime(in *Instance, event referenceLifetimeEvent) {
	var release referenceTokenEntries
	var collector *gc.Collector
	topology := s.lockGCDomainTopologyForInstance(in)
	s.mu.Lock()
	if entry := s.instances[in]; entry != nil {
		switch event {
		case referenceLifetimeClosed:
			if !entry.closeAccounted {
				entry.closeAccounted = true
				if s.liveInstances > 0 {
					s.liveInstances--
				}
			}
		case referenceLifetimeQuiesced:
			entry.quiesced = true
		case referenceLifetimeResourcesReleased:
			if !entry.resourcesReleased {
				entry.resourcesReleased = true
				collector = s.releaseGCDomainLocked(entry)
			}
		}
		if entry.closeAccounted && entry.quiesced && entry.resourcesReleased {
			s.unregisterInstanceTypesLocked(in)
			delete(s.instances, in)
		}
	}
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	if topology != nil {
		topology.Unlock()
	}
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
	s.externrefs = append(s.externrefs, externrefSlot{value: owner, nextFree: externrefInternalSlot})
	return uint32(len(s.externrefs)), nil
}

func nextExternrefGeneration(generation uint32) uint32 {
	generation++
	if generation == 0 {
		generation = 1
	}
	return generation
}

var retiredHostFuncRefDispatchBinding = &hostFuncRefDispatchBinding{}

func (s *referenceStore) registerHostFuncRefBindingLocked(binding *hostFuncRefDispatchBinding) (uint32, error) {
	if s.runtimeClosed {
		return 0, fmt.Errorf("wago: reference store is closed")
	}
	for i := range s.externrefs {
		if s.externrefs[i].value == retiredHostFuncRefDispatchBinding {
			s.externrefs[i].generation = nextExternrefGeneration(s.externrefs[i].generation)
			s.externrefs[i].value = binding
			return uint32(i + 1), nil
		}
	}
	if len(s.externrefs) >= int(hostFuncRefDispatchBit-1) {
		return 0, fmt.Errorf("wago: reference store has too many host dispatch bindings")
	}
	s.externrefs = append(s.externrefs, externrefSlot{value: binding, generation: 1, nextFree: externrefInternalSlot})
	return uint32(len(s.externrefs)), nil
}

func (s *referenceStore) unregisterHostFuncRefBindingLocked(binding *hostFuncRefDispatchBinding) {
	if binding == nil || binding.dispatchIndex == 0 || uint64(binding.dispatchIndex) > uint64(len(s.externrefs)) {
		return
	}
	slot := &s.externrefs[binding.dispatchIndex-1]
	if slot.value != binding {
		return
	}
	slot.value = retiredHostFuncRefDispatchBinding
	slot.generation = nextExternrefGeneration(slot.generation)
}

func (s *referenceStore) hostFuncRefDispatch(dispatch uint32) (*HostFuncRef, *hostFuncRefDispatchBinding) {
	index := dispatch &^ hostFuncRefDispatchBit
	if dispatch&hostFuncRefDispatchBit == 0 || index == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if uint64(index) > uint64(len(s.externrefs)) {
		return nil, nil
	}
	switch value := s.externrefs[index-1].value.(type) {
	case *HostFuncRef:
		return value, nil
	case *hostFuncRefDispatchBinding:
		return value.owner, value
	default:
		return nil, nil
	}
}

func (s *referenceStore) hostFuncRef(dispatch uint32) *HostFuncRef {
	owner, _ := s.hostFuncRefDispatch(dispatch)
	return owner
}

func (s *referenceStore) storeObjectClosed() {
	var release referenceTokenEntries
	topology := s.lockGCDomainTopology()
	s.mu.Lock()
	if s.liveObjects > 0 {
		s.liveObjects--
	}
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	if topology != nil {
		topology.Unlock()
	}
	releaseReferenceEntries(release)
}

func (s *referenceStore) closeRuntime() {
	var release referenceTokenEntries
	topology := s.lockGCDomainTopology()
	s.mu.Lock()
	s.runtimeClosed = true
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	if topology != nil {
		topology.Unlock()
	}
	releaseReferenceEntries(release)
}

// emptyForInlineClose reports whether closing the store has constant work. The
// caller has already published Runtime shutdown, so no new entries can arrive.
func (s *referenceStore) emptyForInlineClose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.liveInstances == 0 && s.liveObjects == 0 && len(s.instances) == 0 &&
		len(s.instanceTypes) == 0 && len(s.byIdentity) == 0 && len(s.byToken) == 0 &&
		len(s.gcByToken) == 0 && len(s.externrefs) == 0 && s.gcDomains == nil
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
	generic := source.c.needsExactNativeGCRoots() && source.c.genericGCFrameRoots() != nil
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
	ownerIndex := state.nextResultSlot()
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
		slot, slotErr = source.gc.NewCheckedClassifiedGlobalSlot(ref, gc.RootPublicToken)
		if slotErr != nil {
			return 0, fmt.Errorf("root public GC result: %w", slotErr)
		}
		state.appendResultRootSlot(slot)
	} else {
		slot = state.resultRootSlot(ownerIndex)
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
		state.setResultToken(ownerIndex, token)
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
	lockedDomain := source.lockGCCollector()
	state.mu.Lock()
	s.mu.Lock()
	entry, ok = s.gcByToken[token]
	ownerIndex := entry.ownerIndex
	if !ok || entry.owner != source || state.resultTokenCount == 0 || ownerIndex >= state.resultRootsMade || state.resultToken(ownerIndex) != token || state.resultRootSlot(ownerIndex) != entry.slot {
		s.mu.Unlock()
		state.mu.Unlock()
		unlockGCCollector(lockedDomain)
		unlockNative()
		return fmt.Errorf("invalid or stale GC reference token")
	}
	if source.gc == nil {
		s.mu.Unlock()
		state.mu.Unlock()
		unlockGCCollector(lockedDomain)
		unlockNative()
		return fmt.Errorf("GC reference token collector is unavailable")
	}
	if err := source.gc.SetGlobalSlot(entry.slot, gc.Null()); err != nil {
		s.mu.Unlock()
		state.mu.Unlock()
		unlockGCCollector(lockedDomain)
		unlockNative()
		return fmt.Errorf("release GC reference token: %w", err)
	}
	delete(s.gcByToken, token)
	state.setResultToken(ownerIndex, 0)
	state.resultTokenCount--
	s.mu.Unlock()
	state.mu.Unlock()
	unlockGCCollector(lockedDomain)
	unlockNative()
	source.releaseResourceRoot()
	var release referenceTokenEntries
	topology := s.lockGCDomainTopology()
	s.mu.Lock()
	if s.runtimeClosed && s.liveInstances == 0 && s.liveObjects == 0 && len(s.gcByToken) == 0 {
		release = s.releaseEntriesLocked()
	}
	s.mu.Unlock()
	if topology != nil {
		topology.Unlock()
	}
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
	if !ok || current.owner != owner || current.ref != entry.ref || current.slot != entry.slot || ownerIndex >= ownerState.resultRootsMade || ownerState.resultToken(ownerIndex) != token || ownerState.resultRootSlot(ownerIndex) != entry.slot {
		return gc.Null(), fmt.Errorf("invalid or stale GC reference token")
	}
	if ownerRecord == nil || ownerRecord.resourcesReleased || targetRecord == nil || targetRecord.resourcesReleased || owner.refStore != s || target.refStore != s || owner.gc == nil || owner.gc != target.gc {
		return gc.Null(), fmt.Errorf("GC reference token belongs to a different collector domain")
	}
	if required.Kind != ValueTypeReference || !target.gcRefMatchesValueType(current.ref, required) {
		return gc.Null(), fmt.Errorf("GC reference token does not match the required structural argument type")
	}
	rootIndex := targetState.argumentRootCount
	if rootIndex == targetState.argumentRootsMade {
		slot, err := target.gc.NewCheckedClassifiedGlobalSlot(current.ref, gc.RootForeignInstance)
		if err != nil {
			return gc.Null(), fmt.Errorf("root GC reference argument: %w", err)
		}
		targetState.appendArgumentRootSlot(slot)
	} else if err := target.gc.SetGlobalSlot(targetState.argumentRootSlot(rootIndex), current.ref); err != nil {
		return gc.Null(), fmt.Errorf("root GC reference argument: %w", err)
	}
	targetState.argumentRootCount++
	return current.ref, nil
}

func (in *Instance) rootGCHostArguments(token gcHostActivationToken, dispatch uint32, args []uint64) error {
	state := token.state
	if in == nil || state == nil || in.gc == nil || in.refStore == nil {
		return nil
	}
	var types []ValType
	if dispatch&hostFuncRefDispatchBit != 0 {
		binding, ok := in.boundHostFuncRef(dispatch)
		if !ok {
			return fmt.Errorf("GC host argument owner is unavailable")
		}
		owner := binding.owner
		owner.mu.Lock()
		if !owner.gcCapable || owner.gc == nil || owner.closed || owner.gc.collector != in.gc || owner.gc.domainID == 0 {
			owner.mu.Unlock()
			return fmt.Errorf("GC host argument owner is outside the active collector domain")
		}
		owner.mu.Unlock()
		types = binding.sig.Params
	} else {
		pluginSig, ok := in.pluginGCHostSignature(dispatch)
		if !ok {
			return nil
		}
		if !in.refStore.ownsGCCollector(in.gc) {
			return fmt.Errorf("Runtime plugin GC host import is outside the calling instance's Runtime collector domain")
		}
		types = pluginSig.Params
	}

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
					root, err := in.gc.NewCheckedClassifiedGlobalSlot(ref, gc.RootPublicToken)
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
	if !ok || current.owner != owner || current.ref != entry.ref || current.slot != entry.slot || ownerIndex >= ownerState.resultRootsMade || ownerState.resultToken(ownerIndex) != token || ownerState.resultRootSlot(ownerIndex) != entry.slot {
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
		slot, err := target.gc.NewCheckedClassifiedGlobalSlot(current.ref, gc.RootForeignInstance)
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
	for i := uint32(0); i < state.argumentRootCount; i++ {
		if err := in.gc.SetGlobalSlot(state.argumentRootSlot(i), gc.Null()); err != nil {
			panic(gcStructHelperError{err: fmt.Errorf("clear GC reference argument root %d: %w", i, err)})
		}
	}
	state.argumentRootCount = 0
}

// ReleaseGCRef releases one non-null GC result token issued by this producer.
// It is valid after Instance.Close while the token retains the producer's
// collector; null releases are no-ops. Non-null releases fail with
// ErrPermissionDenied while callback-scoped guest storage is borrowed. Stale,
// foreign-store, and cross-producer tokens reject without changing either owner.
func (in *Instance) ReleaseGCRef(ref GCRef) error {
	if ref.token == 0 {
		return nil
	}
	if in == nil {
		return fmt.Errorf("release GC reference token on nil instance")
	}
	if in.guestStorageBorrowed() {
		return fmt.Errorf("release GC reference token is unavailable while guest storage is borrowed: %w", ErrPermissionDenied)
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
	if s.externFree == 0 && uint64(len(s.externrefs)) >= uint64(^uint32(0)) {
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
	var index uint32
	if s.externFree != 0 {
		index = s.externFree
		slot := &s.externrefs[index-1]
		s.externFree = slot.nextFree
		slot.nextFree = 0
		slot.value = value
	} else {
		index = uint32(len(s.externrefs)) + 1
		generation := s.externSeed + index - 1
		if generation == 0 {
			generation = 1
		}
		s.externrefs = append(s.externrefs, externrefSlot{value: value, generation: generation})
	}
	slot := &s.externrefs[index-1]
	for {
		raw := uint64(slot.generation)<<32 | uint64(index)
		token := bits.RotateLeft64(raw^s.externKey, 17)
		if token != 0 {
			return token, nil
		}
		slot.generation = nextExternrefGeneration(slot.generation)
	}
}

type releasedExternrefMarker struct{ reserved byte }

var releasedExternrefValue = &releasedExternrefMarker{}

func (s *referenceStore) releaseExternref(token uint64) bool {
	if token == 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.externKey == 0 {
		return false
	}
	raw := bits.RotateLeft64(token, -17) ^ s.externKey
	index, generation := uint32(raw), uint32(raw>>32)
	if index == 0 || uint64(index) > uint64(len(s.externrefs)) {
		return false
	}
	slot := &s.externrefs[index-1]
	if slot.generation != generation || slot.value == releasedExternrefValue || slot.nextFree == externrefInternalSlot {
		return false
	}
	slot.value = releasedExternrefValue
	slot.generation = nextExternrefGeneration(slot.generation)
	slot.nextFree = s.externFree
	s.externFree = index
	return true
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
	s.externFree = 0
	if topology := s.gcDomains; topology != nil {
		for domain := topology.first; domain != nil; domain = domain.next {
			entries.collectors = append(entries.collectors, domain.collector)
		}
		topology.first = nil
		topology.last = nil
		topology.n = 0
		topology.funcrefMu.Lock()
		topology.funcrefTables = nil
		topology.funcrefMu.Unlock()
	}
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
			if ownerIndex < state.resultRootsMade && state.resultToken(ownerIndex) == entry.token && state.resultRootSlot(ownerIndex) == entry.slot {
				if entry.owner.gc != nil {
					_ = entry.owner.gc.SetGlobalSlot(entry.slot, gc.Null())
				}
				state.setResultToken(ownerIndex, 0)
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
