// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/bits"
	"sort"
	"sync"
	"sync/atomic"
	"unsafe"

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
	gcObjectTokenIndex
	gcDomainStoreState
	externKey  uint64
	externSeed uint32
	externFree uint32
	externrefs []externrefSlot
}

// gcDomainTopology keeps the globally ordered Runtime GC-domain list stable
// across dynamic funcref invocations. It is a sidecar so scalar-only stores keep
// the established referenceStore footprint. Host callbacks suspend the read
// lease together with collector leases, allowing instantiation and teardown to
// update the list before native resume.
type gcDomainTopology struct {
	// Keep the lock's method set on the gate. Promoting it makes the topology
	// participate in generic Locker dispatch and retains its full type graph.
	gate        gcTopologyGate
	first       *gcStoreDomain
	last        *gcStoreDomain
	n           int
	byCollector map[*gc.Collector]*gcStoreDomain
	gcFrameCodeDirectory

	funcrefMu       sync.Mutex
	funcrefGCActive bool
	funcrefTables   map[*Table]uint32
}

const gcCollectorIndexThreshold = 4

// domainForCollectorLocked uses the side index for exact collector lookups.
// The linked-list fallback keeps test-built and legacy zero-value topologies
// valid; production mutations maintain byCollector together with the list.
// Callers hold the owning referenceStore.mu.
func (topology *gcDomainTopology) domainForCollectorLocked(collector *gc.Collector) *gcStoreDomain {
	if topology == nil || collector == nil {
		return nil
	}
	if topology.byCollector == nil {
		for domain := topology.first; domain != nil; domain = domain.next {
			if domain.collector == collector {
				return domain
			}
		}
		return nil
	}
	if domain := topology.byCollector[collector]; domain != nil {
		return domain
	}
	for domain := topology.first; domain != nil; domain = domain.next {
		if domain.collector == collector {
			return domain
		}
	}
	return nil
}

// appendDomainLocked and unlinkDomainLocked preserve global acquisition order
// while keeping exact collector lookup and removal constant-time. Callers hold
// the owning referenceStore.mu and the topology write gate when required.
func (topology *gcDomainTopology) appendDomainLocked(domain *gcStoreDomain) {
	if topology.byCollector == nil && topology.n >= gcCollectorIndexThreshold {
		topology.byCollector = make(map[*gc.Collector]*gcStoreDomain, topology.n+1)
		for existing := topology.first; existing != nil; existing = existing.next {
			topology.byCollector[existing.collector] = existing
		}
	}
	if topology.last == nil {
		topology.first = domain
	} else {
		topology.last.next = domain
	}
	domain.prev, domain.next = topology.last, nil
	topology.last = domain
	if topology.byCollector != nil {
		topology.byCollector[domain.collector] = domain
	}
	topology.n++
}

func (topology *gcDomainTopology) unlinkDomainLocked(domain *gcStoreDomain) bool {
	if topology == nil || domain == nil {
		return false
	}
	if topology.byCollector != nil && topology.byCollector[domain.collector] != domain {
		return false
	}
	if domain.prev == nil && topology.first != domain || domain.prev != nil && domain.prev.next != domain {
		return false
	}
	if domain.next == nil && topology.last != domain || domain.next != nil && domain.next.prev != domain {
		return false
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
	if topology.byCollector[domain.collector] == domain {
		delete(topology.byCollector, domain.collector)
	}
	domain.prev, domain.next = nil, nil
	if topology.n > 0 {
		topology.n--
	}
	return true
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
	invocationMu    invocationGate
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
	gcInstanceRegistration
	closeAccounted    bool
	quiesced          bool
	resourcesReleased bool
}

type gcInvocationRegistration struct {
	domain *gcStoreDomain
	// imports is allocated only when direct or transitive function imports
	// reach a collector domain other than this instance's own domain.
	imports *gcInvocationDomainSet
	dynamic bool
}

const inlineGCInvocationDomains = 4

type gcInvocationDomainSet struct {
	inline [inlineGCInvocationDomains]*gcStoreDomain
	extra  []*gcStoreDomain
	n      int
	index  map[*gcStoreDomain]struct{}
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
	if s.index != nil {
		if _, ok := s.index[domain]; ok {
			return
		}
	} else {
		for i := 0; i < s.n; i++ {
			if s.at(i) == domain {
				return
			}
		}
	}
	if s.n < len(s.inline) {
		s.inline[s.n] = domain
	} else {
		s.extra = append(s.extra, domain)
	}
	s.n++
	if s.index != nil {
		s.index[domain] = struct{}{}
	} else if s.n > inlineGCInvocationDomains*2 {
		s.index = make(map[*gcStoreDomain]struct{}, s.n)
		for i := 0; i < s.n; i++ {
			s.index[s.at(i)] = struct{}{}
		}
	}
}

func (s *gcInvocationDomainSet) sort() {
	if s == nil || s.n < 2 {
		return
	}
	if s.n <= len(s.inline) {
		// The inline set has at most four entries. Keep it on the caller's stack.
		for i := 1; i < s.n; i++ {
			value := s.inline[i]
			j := i
			for j > 0 && value.id < s.inline[j-1].id {
				s.inline[j] = s.inline[j-1]
				j--
			}
			s.inline[j] = value
		}
		return
	}
	*s = sortedGCInvocationDomainSet(*s)
}

// The sort interface retains only this copy. Empty and inline sets never escape.
func sortedGCInvocationDomainSet(s gcInvocationDomainSet) gcInvocationDomainSet {
	sort.Sort(gcInvocationDomainSetSorter{&s})
	return s
}

type gcInvocationDomainSetSorter struct{ set *gcInvocationDomainSet }

func (s gcInvocationDomainSetSorter) Len() int           { return s.set.n }
func (s gcInvocationDomainSetSorter) Less(i, j int) bool { return s.set.at(i).id < s.set.at(j).id }
func (s gcInvocationDomainSetSorter) Swap(i, j int) {
	a, b := s.set.at(i), s.set.at(j)
	s.set.set(i, b)
	s.set.set(j, a)
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

const maxStructuralCallIdentityRegistrationBytes = 16 << 20

type funcrefIdentity struct {
	descriptor uint64
	instance   *Instance
	localIdx   int
}

type funcrefTokenEntry struct {
	token        uint64
	descriptor   uint64
	owner        *Instance
	resultClaims uint32 // unfinished public result conversions
	published    bool   // a successful boundary or direct issuance exposed the token
}

// gcResultRoot identifies one published collector slot and its producer claim.
type gcResultRoot struct {
	token      uint64
	slot       uint32
	ownerIndex uint32
	owner      *Instance
}

type gcRefTokenEntry struct {
	gcResultRoot
	ref        gc.Ref
	exact      ValueTypeDescriptor // owner-local diagnostic identity
	domainType gc.TypeID           // canonical collector-domain identity
}

const (
	gcNativeFrameLayoutAMD64 uint8 = iota
	gcNativeFrameLayoutARM64
	gcNativeFrameLayoutRISCV64
	gcNativeFrameLayoutMask      = 0x7f
	gcNativeFrameSyncGlobalRoots = 0x80
)

// Include the outer callback as well as every permitted nested host reentry.
// Preserve the existing capacity for builds with a lower reentry limit.
const gcHostActivationLimit = max(8, maxHostReentryDepth+1)

func (r *gcNativeFrameRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	r.walk(fn, nil)
}

func (r *gcNativeFrameRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	return r.walk(nil, sink)
}

// RangeClassifiedRootRefs preserves exact runtime ownership for opt-in
// root traversal without allocating composite RootSet values on helper
// paths.
func (r *gcNativeFrameRoots) RangeClassifiedRootRefs(sink gc.ClassifiedRootRefSink) bool {
	if r == nil || sink == nil {
		return true
	}
	if !r.rangeTransientRoots(nil, classifiedRootSink{sink: sink, class: gc.RootNativeFrame}) {
		return false
	}
	if r.owner != nil && r.owner.refStore != nil && r.owner.refStore.ownsGCCollector(r.owner.gc) {
		return r.owner.refStore.rangeGCDomainPersistentRootsClassified(r.owner.gc, sink)
	}
	if r.owner != nil && managedFunctionOwnerRootsStrong(r.owner, r.owner.gc) {
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
	if !r.rangeTransientRoots(fn, sink) {
		return false
	}

	domainRoots := false
	if r.owner != nil && r.owner.refStore != nil && r.owner.refStore.ownsGCCollector(r.owner.gc) {
		domainRoots = true
		if !r.owner.refStore.rangeGCDomainPersistentRoots(r.owner.gc, fn, sink) {
			return false
		}
	}
	if !domainRoots && r.owner != nil && managedFunctionOwnerRootsStrong(r.owner, r.owner.gc) {
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
		panic(gcHelperFailuref("generic GC table length %d exceeds descriptor capacity", length))
	}
	for i := uint64(0); i < length; i++ {
		addr := r.desc + 8 + uintptr(i*8)
		word := binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(addr)), 8))
		if word != uint64(gc.Ref(uint32(word))) {
			panic(gcHelperFailuref("generic GC table root %d contains non-compact reference %#x", i, word))
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

// The legacy abstract-reference products register checked collector slots.
// Their table descriptors can also hold foreign conversion identities.
func (c *Compiled) usesLegacyGCTableRoots() bool {
	return rv64LegacyGCProductsForImage && (rv64GCRefTestAbstract(c) || rv64GCExtern(c) ||
		rv64GCRefCastAbstract(c) || rv64GCBrOnCastAbstract(c) || rv64GCBrOnCastFailAbstract(c))
}

func (in *Instance) rangeLocalGCTableRoots(fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	if in == nil || in.c == nil {
		return true
	}
	if !rangeLocalCompactExternRoots(in, fn, sink) {
		return false
	}
	// Only walk descriptors whose entries hold raw collector references.
	if !in.c.usesLegacyGCTableRoots() {
		for tableIndex := 0; tableIndex < in.c.tableCount(); tableIndex++ {
			if !isGCRefValType(in.c.tableElementType(tableIndex)) {
				continue
			}
			desc := in.tableDescriptor(tableIndex)
			if len(desc) < 8 {
				panic(gcHelperFailuref("generic GC table %d descriptor is unavailable", tableIndex))
			}
			roots := gcNativeTableRoots{desc: uintptr(unsafe.Pointer(&desc[0])), bytes: uintptr(len(desc))}
			if !roots.walk(fn, sink) {
				return false
			}
		}
	}
	passiveBase := in.jm.PassiveElemPtr()
	for i := range in.c.passiveElems {
		elem := &in.c.passiveElems[i]
		if elem.Mode != ElemModePassive || !isGCRefValType(normalizedElemRefType(elem.RefType)) {
			continue
		}
		if passiveBase == 0 {
			panic(gcHelperFailuref("generic GC passive element descriptor %d is unavailable", i))
		}
		descAddr := passiveBase + uintptr(i*coreruntime.PassiveElemDescBytes)
		desc := unsafe.Slice((*byte)(offHeapPtr(descAddr)), coreruntime.PassiveElemDescBytes)
		entries := uintptr(binary.LittleEndian.Uint64(desc))
		length := uint64(binary.LittleEndian.Uint32(desc[8:]))
		if length > uint64(len(elem.Values)) {
			panic(gcHelperFailuref("generic GC passive element %d length %d exceeds %d", i, length, len(elem.Values)))
		}
		if length != 0 && entries == 0 {
			panic(gcHelperFailuref("generic GC passive element %d entries are unavailable", i))
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

const gcPublicSlotLimit = 64

// gcPublicState serializes public-token, generic helper, and boundary-collection
// access. The first 64 result and argument roots stay inline. Wider workloads use
// reusable overflow slices; released result slots remain holes and are reused
// without making stale opaque tokens valid again.
type gcPublicState struct {
	gcDomainHostRootScratch
	parkedHostRootCache
	gcHostActivationState
	mu sync.Mutex
	gcResultSlotState
	gcArgumentRootState
	gcCloneRootState
	gcArrayConstructorState
	gcStructConstructorState
	frameRoots gcNativeFrameRoots // exact parked native-frame roots; reused under mu
	gcGlobalRootState
}

type gcSuspendedRootOwner struct {
	owner *Instance
	state *gcPublicState
}

type externrefSlot struct {
	value      any
	generation uint32
	// nextFree is a pin count for a live public slot, a free-list link for a
	// released public slot, or externrefInternalSlot for an internal owner.
	nextFree uint32
}

const externrefInternalSlot = ^uint32(0)

func newReferenceStore(private bool) *referenceStore {
	return &referenceStore{private: private, runtimeClosed: private}
}

func (s *referenceStore) ensureGCTopology() *gcDomainTopology {
	if !nativeCollectorFrameRootsBuilt || s == nil {
		return nil
	}
	s.mu.Lock()
	topology := s.ensureGCTopologyLocked()
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
	if !nativeCollectorFrameRootsBuilt || s == nil || collector == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gcTopologyLocked().domainForCollectorLocked(collector) != nil
}

func (s *referenceStore) gcDomainForCollector(collector *gc.Collector) *gcStoreDomain {
	if !nativeCollectorFrameRootsBuilt || s == nil || collector == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gcTopologyLocked().domainForCollectorLocked(collector)
}

func (s *referenceStore) lockGCCollector(collector *gc.Collector) *gcStoreDomain {
	if !nativeCollectorFrameRootsBuilt || s == nil || collector == nil {
		return nil
	}
	s.mu.Lock()
	found := s.gcTopologyLocked().domainForCollectorLocked(collector)
	s.mu.Unlock()
	if found != nil {
		found.mu.Lock()
	}
	return found
}

func (in *Instance) lockGCCollector() *gcStoreDomain {
	if !nativeCollectorFrameRootsBuilt || in == nil || in.refStore == nil {
		return nil
	}
	return in.refStore.lockGCCollector(in.gc)
}

func unlockGCCollector(domain *gcStoreDomain) {
	if nativeCollectorFrameRootsBuilt && domain != nil {
		domain.mu.Unlock()
		drainGCMetadataReclaims(domain.collector)
	}
}

func (in *Instance) gcInvocationDomainsForInspection() (gcInvocationDomainView, *gcDomainTopology) {
	if !nativeCollectorFrameRootsBuilt {
		return gcInvocationDomainView{}, nil
	}
	if in == nil || in.refStore == nil || in.executionFlags.Load()&executionFlagDynamicGCDomain == 0 {
		return in.gcInvocationDomains(), nil
	}
	in.refStore.mu.Lock()
	topology := in.refStore.gcTopologyLocked()
	in.refStore.mu.Unlock()
	if topology != nil {
		topology.gate.RLock()
	}
	return in.gcInvocationDomains(), topology
}

func (in *Instance) validateGCInvocationImport(key string, store *referenceStore, dynamic, privateWait bool) error {
	hasDomain := in.gc != nil || in.executionFlags.Load()&executionFlagImportedGCDomain != 0
	if dynamic && in.refStore != store && hasDomain {
		return fmt.Errorf("dynamic funcref import %q from a GC-domain producer requires the same Runtime", key)
	}
	if dynamic && in.reachesPrivateGCInvocationDomain() {
		return fmt.Errorf("dynamic funcref import %q cannot reach a private GC invocation domain", key)
	}
	if privateWait && hasDomain {
		return fmt.Errorf("Runtime GC-domain import %q is unsupported for modules with atomic wait helpers", key)
	}
	return nil
}

func (in *Instance) reachesPrivateGCInvocationDomain() bool {
	domains, topology := in.gcInvocationDomainsForInspection()
	if topology != nil {
		defer topology.gate.RUnlock()
	}
	for i := 0; i < domains.len(); i++ {
		if domains.at(i).private {
			return true
		}
	}
	return false
}

func (in *Instance) gcInvocationDomain() *gcStoreDomain {
	if !nativeCollectorFrameRootsBuilt || in == nil || in.refStore == nil || in.gc == nil {
		return nil
	}
	in.refStore.mu.Lock()
	entry := in.refStore.instances[in]
	var domain *gcStoreDomain
	if entry != nil {
		domain = entry.gcRegistration().domain
	}
	in.refStore.mu.Unlock()
	return domain
}

func (in *Instance) gcInvocationDomains() gcInvocationDomainView {
	if !nativeCollectorFrameRootsBuilt || in == nil || in.refStore == nil || in.gc == nil && in.executionFlags.Load()&executionFlagImportedGCDomain == 0 {
		return gcInvocationDomainView{}
	}
	store := in.refStore
	store.mu.Lock()
	entry := store.instances[in]
	var domains gcInvocationDomainView
	if entry != nil {
		registration := entry.gcRegistration()
		if registration.dynamic {
			if registration.domain != nil && registration.domain.private {
				domains.local = registration.domain
			}
			if topology := store.gcTopologyLocked(); topology != nil {
				domains.first, domains.last, domains.n = topology.first, topology.last, topology.n
			}
			domains.dynamic = true
		} else {
			domains = gcInvocationDomainView{local: registration.domain, set: registration.imports}
		}
	}
	store.mu.Unlock()
	return domains
}

// mayNeedGCInvocationLease reads current ownership: a reference store or
// dynamic GC domain can be published after a prepared handle is created.
func (in *Instance) mayNeedGCInvocationLease() bool {
	return nativeCollectorFrameRootsBuilt && in != nil && in.refStore != nil && (in.gc != nil || in.executionFlags.Load()&(executionFlagImportedGCDomain|executionFlagDynamicGCDomain) != 0)
}

// hostCallNeedsGCSuspension is checked on the actual lease owner under native
// ownership. Collector identity and domain-admission flags are established
// before invocation. Dynamic topology always falls back, even when empty now.
// No signature-based test can replace this predicate: a scalar relay can own
// imported domains without having a local collector.
func (in *Instance) hostCallNeedsGCSuspension() bool {
	return nativeCollectorFrameRootsBuilt && in != nil && (in.gc != nil || in.executionFlags.Load()&
		(executionFlagImportedGCDomain|executionFlagDynamicGCDomain|executionFlagStoreOwnedGCCollector) != 0)
}

func (s *referenceStore) rangeGCDomainPersistentRoots(collector *gc.Collector, fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	if s == nil || collector == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for candidate, state := range s.instances {
		if state == nil || state.resourcesReleased || candidate == nil || candidate.gc != collector || candidate.c == nil || !managedFunctionOwnerRootsStrong(candidate, collector) {
			continue
		}
		for i, global := range candidate.globalCells {
			if global == nil || i >= len(candidate.c.Globals) || !isGCRefValType(candidate.c.Globals[i].Type) || len(global.cell) < 8 {
				continue
			}
			bits := binary.LittleEndian.Uint64(global.cell)
			ref := gc.Ref(uint32(bits))
			if bits != uint64(ref) {
				panic(gcHelperFailuref("Runtime GC-domain global %d contains non-compact reference %#x", i, bits))
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
		if state == nil || state.resourcesReleased || candidate == nil || candidate.gc != collector || candidate.c == nil || !managedFunctionOwnerRootsStrong(candidate, collector) {
			continue
		}
		for i, global := range candidate.globalCells {
			if global == nil || i >= len(candidate.c.Globals) || !isGCRefValType(candidate.c.Globals[i].Type) || len(global.cell) < 8 {
				continue
			}
			bits := binary.LittleEndian.Uint64(global.cell)
			ref := gc.Ref(uint32(bits))
			if bits != uint64(ref) {
				panic(gcHelperFailuref("Runtime GC-domain global %d contains non-compact reference %#x", i, bits))
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

func (s *referenceStore) gcNativeCallerOwner(pc, home, context uintptr) *Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	for candidate, registration := range s.instances {
		if registration != nil && !registration.resourcesReleased && candidate != nil && candidate.c != nil && candidate.jm != nil && candidate.nativeContext == context && candidate.jm.LinMemBase() == home && pc >= candidate.base && pc-candidate.base < uintptr(len(candidate.c.code)) {
			return candidate
		}
	}
	return nil
}

func equalGCConfigs(a, b gc.Config) bool {
	return a == b
}

func (s *referenceStore) acquireGCCollector(ctx context.Context, config gc.Config, c *Compiled, preferred *gc.Collector) (*gc.Collector, *gcTypeMapping, error) {
	if s == nil || s.private {
		return nil, nil, fmt.Errorf("wago: shared WasmGC ownership requires an explicit Runtime")
	}
	topology := s.ensureGCTopology()
	if err := topology.gate.lockContext(ctx, true); err != nil {
		return nil, nil, err
	}
	topologyLocked := true
	defer func() {
		if topologyLocked {
			topology.gate.Unlock()
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
		selected = topology.domainForCollectorLocked(preferred)
		if selected == nil {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("wago: imported WasmGC collector is not a live Runtime domain")
		}
		if !equalGCConfigs(selected.config, config) {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("wago: WasmGC collector configuration is incompatible with the imported Runtime GC domain")
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
		collector.UseOwnedThroughputBacking()
		selected = &gcStoreDomain{id: newGCDomainIdentity(), collector: collector, config: config, types: types, typeReps: reps, claims: 1}
		topology.appendDomainLocked(selected)
		s.mu.Unlock()
		c.rememberGCTypeMapping(selected.id, len(selected.typeReps), mapping)
		return collector, mapping, nil
	}
	if selected.claims == ^uint32(0) {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("wago: Runtime GC domain has too many pending instances")
	}
	selected.claims++
	s.mu.Unlock()
	topology.gate.Unlock()
	topologyLocked = false

	// Native subtype readers hold invocationMu but do not enter Go or selected.mu.
	// Quiesce the whole domain before replacing and republishing the interval
	// backing, then follow the ordinary invocationMu -> mu lock order.
	if err := selected.invocationMu.lockContext(ctx); err != nil {
		s.releaseUnclaimedGCCollector(selected.collector)
		return nil, nil, err
	}
	selected.mu.Lock()
	domainTypeCount := len(selected.typeReps)
	mapping := c.cachedGCTypeMapping(selected.id, domainTypeCount)
	types, reps := selected.types, selected.typeReps
	var err error
	if mapping == nil {
		mapping, types, reps, err = gcCanonicalTypePlan(c, selected.typeReps, selected.types, preferred != nil)
	}
	if err == nil && len(types) > len(selected.types) {
		err = selected.collector.AddTypes(types[len(selected.types):])
	}
	if err == nil {
		// Compatibility probes read these slices under the store lock. Keep
		// publication in that lock as well as the collector's mutation lock.
		s.mu.Lock()
		selected.types, selected.typeReps = types, reps
		domainTypeCount = len(selected.typeReps)
		s.mu.Unlock()
	}
	selected.mu.Unlock()
	selected.invocationMu.Unlock()
	if err != nil {
		s.releaseUnclaimedGCCollector(selected.collector)
		return nil, nil, err
	}
	c.rememberGCTypeMapping(selected.id, domainTypeCount, mapping)
	return selected.collector, mapping, nil
}

func (s *referenceStore) registerInstance(in *Instance) error {
	managedFunctionPrepareRegistration(s, in)
	var importedInvocationDomains gcInvocationDomainSet
	dynamicInvocationDomains := in != nil && (compiledManagedFunctionReachability(in.c) || !s.private && compiledHasDynamicFuncrefReachability(in.c))
	importsPrivateInvocationDomain := false
	if hostFunctionImportsBuilt && in != nil && in.c != nil {
		for i, displayKey := range in.c.Imports {
			export, ok := in.imports[in.c.functionImportBindingKey(i)].(*InstanceExport)
			if !ok || export == nil || export.inst == nil {
				continue
			}
			if export.inst.executionFlags.Load()&executionFlagDynamicGCDomain != 0 || compiledManagedFunctionReachability(export.inst.c) {
				if export.inst.refStore != s {
					return fmt.Errorf("wago: dynamic funcref import %q requires the same Runtime", displayKey)
				}
				dynamicInvocationDomains = true
				continue
			}
			if nativeCollectorFrameRootsBuilt {
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
	}
	if dynamicInvocationDomains && importsPrivateInvocationDomain {
		return fmt.Errorf("wago: dynamic funcref imports cannot reach a private GC invocation domain")
	}
	if structuralCallKeysForImage && in != nil && in.c != nil {
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
	if nativeCollectorFrameRootsBuilt && dynamicInvocationDomains {
		s.ensureGCTopologyLocked()
	}
	if _, exists := s.instances[in]; exists {
		return nil
	}
	if s.liveInstances == ^uint32(0) {
		return fmt.Errorf("wago: reference store has too many live instances")
	}

	var keys []uint64
	if structuralCallKeysForImage {
		var err error
		keys, err = s.registerStructuralCallKeysLocked(in.c)
		if err != nil {
			return err
		}
	}
	var domain *gcStoreDomain
	if topology := s.gcTopologyLocked(); nativeCollectorFrameRootsBuilt && topology != nil {
		domain = topology.domainForCollectorLocked(in.gc)
		if domain != nil {
			if domain.refs == ^uint32(0) {
				return fmt.Errorf("wago: Runtime GC domain has too many instances")
			}
			if domain.claims == 0 {
				return fmt.Errorf("wago: Runtime GC domain registration has no prepared claim")
			}
			domain.claims--
			domain.refs++
		}
	}
	if nativeCollectorFrameRootsBuilt && domain == nil && in.gc != nil {
		// Private collectors still need complete-call ownership through public
		// result tokenization. Keep their invocation domain out of the Runtime
		// topology so ownership, root scanning, and collector lifetime remain local.
		domain = &gcStoreDomain{id: newGCDomainIdentity(), collector: in.gc, private: true}
	}
	var storedInvocationDomains *gcInvocationDomainSet
	if nativeCollectorFrameRootsBuilt {
		importedInvocationDomains.add(domain)
		importedInvocationDomains.sort()
		if !dynamicInvocationDomains && importedInvocationDomains.len() != 0 && (importedInvocationDomains.len() != 1 || importedInvocationDomains.at(0) != domain) {
			storedInvocationDomains = new(gcInvocationDomainSet)
			*storedInvocationDomains = importedInvocationDomains
		}
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
	if structuralCallKeysForImage {
		s.instanceTypes[in] = keys
	}
	s.instances[in] = &referenceStoreInstance{
		gcInstanceRegistration: newGCInstanceRegistration(domain, storedInvocationDomains, dynamicInvocationDomains),
	}
	s.registerGCFrameCodeRangeLocked(in)
	s.liveInstances++
	managedFunctionRegisteredLocked(s, in)
	return nil
}

func (s *referenceStore) registerStructuralCallKeysLocked(c *Compiled) ([]uint64, error) {
	// Resolve every fast-key equality against exact structural descriptors before
	// publishing any descriptor from this instance. Native calls may then compare
	// the compact key authoritatively: the store invariant guarantees that one key
	// never denotes two distinct live structural types.
	candidate := make(map[uint64]structuralTypeRegistration)
	keyCapacity := len(c.FuncTypeID)
	if types := len(c.Types); types > 0 && types < keyCapacity {
		// Most functions reuse declared types. This is only an allocation hint:
		// append still accepts every distinct key, including legacy metadata.
		keyCapacity = types
	}
	keys := make([]uint64, 0, keyCapacity)
	var identitiesByType map[uint32][]byte
	var typeGroups map[uint32]structuralTypeGroupBounds
	identityBytes := 0
	for i, key := range c.FuncTypeID {
		canonical, cached := c.cachedStructuralCallIdentity(i)
		if !cached {
			sig, sigOK := compiledFunctionSignature(c, i)
			if sigOK && sig.HasTypeIndex {
				canonical = identitiesByType[sig.TypeIndex]
			}
			if canonical == nil {
				if typeGroups == nil && sigOK && sig.HasTypeIndex {
					typeGroups = compiledStructuralTypeGroups(c.Types)
				}
				var err error
				canonical, err = compiledStructuralCallIdentityWithGroups(c, i, typeGroups)
				if err != nil {
					return nil, fmt.Errorf("wago: function %d exact type: %w", i, err)
				}
				if sigOK && sig.HasTypeIndex && len(c.FuncTypeID) > 1 {
					if identitiesByType == nil {
						identitiesByType = make(map[uint32][]byte, min(keyCapacity, len(c.Types)))
					}
					identitiesByType[sig.TypeIndex] = canonical
				}
			}
		}
		exact := structuralTypeRegistration{canonical: canonical}
		if prior, ok := candidate[key]; ok {
			if !bytes.Equal(prior.canonical, exact.canonical) {
				return nil, fmt.Errorf("wago: native structural type key %#x collides within module", key)
			}
			continue
		}
		if len(canonical) > maxStructuralCallIdentityRegistrationBytes-identityBytes {
			return nil, fmt.Errorf("wago: structural call identities exceed the %d-byte per-registration budget", maxStructuralCallIdentityRegistrationBytes)
		}
		identityBytes += len(canonical)
		candidate[key] = exact
		keys = append(keys, key)
		if registered, ok := s.typeKeys[key]; ok && !bytes.Equal(registered.canonical, exact.canonical) {
			return nil, fmt.Errorf("wago: native structural type key %#x collides with a distinct store type", key)
		}
	}
	for _, key := range keys {
		if registered, ok := s.typeKeys[key]; ok && registered.refs == ^uint32(0) {
			return nil, fmt.Errorf("wago: native structural type key %#x has too many owners", key)
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
	return keys, nil
}

func compiledHasDynamicFuncrefReachability(c *Compiled) bool {
	if c == nil {
		return false
	}
	if compiledManagedFunctionReachability(c) {
		return true
	}
	// A funcref table or global can acquire a cross-instance descriptor after
	// instantiation. Conservatively lease every live Runtime GC domain for these
	// container-bearing modules; ordinary ref.func and direct-import modules keep
	// their compact statically discovered domain sets.
	if tablesForImage && c.hasFuncrefTable() {
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
	if !nativeCollectorFrameRootsBuilt || entry == nil {
		return nil
	}
	registration := entry.gcRegistration()
	domain := registration.domain
	if domain == nil {
		return nil
	}
	registration.domain = nil
	if domain.private {
		return nil
	}
	if domain.refs > 0 {
		domain.refs--
	}
	if domain.refs != 0 || domain.claims != 0 {
		return nil
	}
	topology := s.gcTopologyLocked()
	if topology == nil {
		return nil
	}
	if topology.unlinkDomainLocked(domain) {
		return domain.collector
	}
	return nil
}

func (s *referenceStore) lockGCDomainTopology() *gcDomainTopology {
	if !nativeCollectorFrameRootsBuilt || s == nil {
		return nil
	}
	s.mu.Lock()
	topology := s.gcTopologyLocked()
	s.mu.Unlock()
	if topology != nil {
		topology.gate.Lock()
	}
	return topology
}

func (s *referenceStore) lockGCDomainTopologyForInstance(in *Instance) *gcDomainTopology {
	if !nativeCollectorFrameRootsBuilt || in == nil || in.gc == nil {
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
		managedFunctionLifetimeLocked(s, in, true)
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
		topology.gate.Unlock()
	}
	releaseLifetimeEntries(collector, release)
}

func (s *referenceStore) advanceInstanceLifetime(in *Instance, event referenceLifetimeEvent) {
	var release referenceTokenEntries
	var collector *gc.Collector
	topology := s.lockGCDomainTopologyForInstance(in)
	s.mu.Lock()
	if entry := s.instances[in]; entry != nil {
		managedFunctionLifetimeLocked(s, in, event == referenceLifetimeResourcesReleased)
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
		topology.gate.Unlock()
	}
	releaseLifetimeEntries(collector, release)
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
	return s.runtimeClosed && s.liveInstances == 0 && s.liveObjects == 0 && !s.hasObjectTokensLocked() && s.allClosedInstancesQuiescedLocked()
}

func (s *referenceStore) maybeReleaseEntriesLocked() referenceTokenEntries {
	if s.canReleaseEntriesLocked() {
		return s.releaseEntriesLocked()
	}
	return s.closedHostPublicTokenEntriesLocked()
}

func (s *referenceStore) unregisterInstanceTypesLocked(in *Instance) {
	s.unregisterGCFrameCodeRangeLocked(in)
	if !structuralCallKeysForImage {
		return
	}
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
	if managedFunctionLifetimeBuilt {
		defer drainManagedFunctionStoreReclaims(s)
	}
	var release referenceTokenEntries
	topology := s.lockGCDomainTopology()
	s.mu.Lock()
	if s.liveObjects > 0 {
		s.liveObjects--
	}
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	if topology != nil {
		topology.gate.Unlock()
	}
	releaseReferenceEntries(release)
}

func (s *referenceStore) closeRuntime() {
	if managedFunctionLifetimeBuilt {
		defer drainManagedFunctionStoreReclaims(s)
	}
	var release referenceTokenEntries
	topology := s.lockGCDomainTopology()
	s.mu.Lock()
	s.runtimeClosed = true
	release = s.maybeReleaseEntriesLocked()
	s.mu.Unlock()
	if topology != nil {
		topology.gate.Unlock()
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
		!s.hasObjectTokensLocked() && len(s.externrefs) == 0 && s.gcTopologyLocked() == nil
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
	token, _, err := s.issueResultMode(source, descriptor, attachedResult, nil, false)
	return token, err
}

func (s *referenceStore) issueResultMode(source *Instance, descriptor uint64, attachedResult bool, journal []publicResultToken, provisional bool) (uint64, *funcrefTokenEntry, error) {
	if descriptor == 0 {
		return 0, nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := func(entry *funcrefTokenEntry) (uint64, *funcrefTokenEntry, error) {
		if !railshotValidFuncrefPublication(s, entry.owner, entry.descriptor) {
			return 0, nil, fmt.Errorf("invalid funcref result descriptor")
		}
		if !provisional {
			entry.published = true
			return entry.token, nil, nil
		}
		if entry.published {
			return entry.token, nil, nil
		}
		for _, issued := range journal {
			if issued.funcref == entry {
				return entry.token, nil, nil
			}
		}
		if entry.resultClaims == ^uint32(0) {
			return 0, nil, fmt.Errorf("funcref result claim limit")
		}
		entry.resultClaims++
		return entry.token, entry, nil
	}
	// RV64's imported descriptors are importer-owned wrappers. Authenticate
	// their frozen publication metadata even when a token was already issued.
	if railshotNativeContextTrapABI() && source != nil {
		if index, own := source.funcrefDescriptorIndex(descriptor); own && index < source.c.NumImports {
			if _, _, valid := s.canonicalFuncrefOwnerLocked(source, descriptor); !valid {
				return 0, nil, fmt.Errorf("invalid funcref result descriptor")
			}
		}
	}
	if entry := s.byIdentity[funcrefIdentity{descriptor: descriptor}]; entry != nil {
		if entry.owner != nil && entry.owner.hostEvents != nil {
			return 0, nil, deferredHostEventCalleeError()
		}
		return existing(entry)
	}
	if source == nil {
		return 0, nil, fmt.Errorf("invalid funcref result descriptor")
	}
	owner, canonical, ok := s.canonicalFuncrefOwnerLocked(source, descriptor)
	if !ok {
		return 0, nil, fmt.Errorf("invalid funcref result descriptor")
	}
	if owner.hostEvents != nil {
		return 0, nil, deferredHostEventCalleeError()
	}
	identity, hasIdentity := source.funcrefFunctionIdentity(descriptor)
	if hasIdentity {
		if entry := s.byIdentity[identity]; entry != nil {
			if entry.owner != nil && entry.owner.hostEvents != nil {
				return 0, nil, deferredHostEventCalleeError()
			}
			return existing(entry)
		}
	}
	if entry := s.byIdentity[funcrefIdentity{descriptor: canonical}]; entry != nil {
		if entry.owner != nil && entry.owner.hostEvents != nil {
			return 0, nil, deferredHostEventCalleeError()
		}
		return existing(entry)
	}
	var retained bool
	// RV64 canonicalization authenticates foreign rows against retained
	// attachments before returning their owner. A local wrapper can return
	// such a row after the producer closes, just as an imported export can.
	// Only result conversion may transfer this already-held physical lifetime.
	retainedForeignResult := provisional && railshotNativeContextTrapABI() && owner != source && s.hasInstanceResourcesLocked(owner)
	if attachedResult || retainedForeignResult {
		retained = owner.retainResourceRootForFinalization()
	} else {
		retained = owner.retainResourceRoot()
	}
	if !retained {
		return 0, nil, fmt.Errorf("funcref producer is closed")
	}
	token, err := s.newTokenLocked()
	if err != nil {
		owner.releaseResourceRoot()
		return 0, nil, err
	}
	entry := &funcrefTokenEntry{token: token, descriptor: canonical, owner: owner, published: !provisional}
	if provisional {
		entry.resultClaims = 1
	}
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
		return 0, nil, fmt.Errorf("host funcref owner closed during token issue")
	}
	return token, entry, nil
}

// finishFuncrefResult commits a successful boundary or rolls back an unpublished
// token. Existing tokens and tokens observed by another boundary are preserved.
func (s *referenceStore) finishFuncrefResult(entry *funcrefTokenEntry, success bool) {
	s.mu.Lock()
	if s.byToken[entry.token] != entry {
		s.mu.Unlock()
		return
	}
	if entry.resultClaims == 0 {
		s.mu.Unlock()
		panic("funcref result claim underflow")
	}
	entry.resultClaims--
	entry.published = entry.published || success
	if entry.published || entry.resultClaims != 0 {
		s.mu.Unlock()
		return
	}
	delete(s.byToken, entry.token)
	for key, alias := range s.byIdentity {
		if alias == entry {
			delete(s.byIdentity, key)
		}
	}
	s.mu.Unlock()
	var cleanup resourceCleanup
	cleanup.run(func() { releaseFuncrefTokenMetadata(entry) })
	cleanup.run(entry.owner.releaseResourceRoot)
	cleanup.finish()
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
	return owner.attachedFunctionIndexExactType(index)
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
	if !hostFunctionImportsBuilt || index >= len(in.c.Imports) {
		return ValueTypeDescriptor{}, nil, false
	}
	var export *InstanceExport
	if railshotNativeContextTrapABI() {
		entries := in.importReferenceEntries()
		if index < len(entries) {
			export = entries[index].native
		}
	} else {
		export, _ = in.imports[in.c.functionImportBindingKey(index)].(*InstanceExport)
	}
	if export != nil && export.inst != nil && export.inst.c != nil && export.localIdx >= 0 {
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
		if token != 0 && !compactExternWordReserved(s, token) {
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
	var root compactExternRoot
	defer func() {
		s.mu.Unlock()
		root.release()
	}()
	if s.externKey == 0 {
		return false
	}
	raw := bits.RotateLeft64(token, -17) ^ s.externKey
	index, generation := uint32(raw), uint32(raw>>32)
	if index == 0 || uint64(index) > uint64(len(s.externrefs)) {
		return false
	}
	slot := &s.externrefs[index-1]
	if slot.generation != generation || slot.value == releasedExternrefValue || slot.nextFree != 0 {
		return false
	}
	root = claimCompactExternRoot(slot.value)
	slot.value = releasedExternrefValue
	slot.generation = nextExternrefGeneration(slot.generation)
	slot.nextFree = s.externFree
	s.externFree = index
	return true
}

// pinExternref closes the validation-to-execution window for a public
// argument. Release fails while any invocation owns a pin, so the slot cannot
// be recycled under native code carrying the authenticated token.
func (s *referenceStore) pinExternref(token uint64) bool {
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
	if slot.generation != generation || slot.value == releasedExternrefValue || slot.nextFree >= externrefInternalSlot-1 {
		return false
	}
	slot.nextFree++
	return true
}

func (s *referenceStore) unpinExternref(token uint64) bool {
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
	if slot.generation != generation || slot.value == releasedExternrefValue || slot.nextFree == 0 || slot.nextFree == externrefInternalSlot {
		return false
	}
	slot.nextFree--
	if s.runtimeClosed && slot.nextFree == 0 {
		railshotCompactExternUnpinnedAfterClose(s)
	}
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
	return publicExternrefValue(slot.value), true
}

func (s *referenceStore) hasObjectTokensLocked() bool {
	return collectorObjectTokensBuilt && len(s.objectTokensLocked()) != 0
}

func (s *referenceStore) hasObjectTokenLocked(token uint64) bool {
	if !collectorObjectTokensBuilt {
		return false
	}
	_, exists := s.objectTokensLocked()[token]
	return exists
}

func (s *referenceStore) newTokenLocked() (uint64, error) {
	for {
		token, err := randomNonzeroUint64()
		if err != nil {
			return 0, fmt.Errorf("create reference token: %w", err)
		}
		if token>>32 != 0 && s.byToken[token] == nil && !s.hasObjectTokenLocked(token) && !compactExternWordReserved(s, token) {
			return token, nil
		}
	}
}

type referenceTokenEntries struct {
	gcResultReleaseBatch
	compactExternReleaseBatch
	gcReleaseBatch
	funcrefs []*funcrefTokenEntry
}

func (s *referenceStore) releaseEntriesLocked() referenceTokenEntries {
	var entries referenceTokenEntries
	if len(s.byToken) != 0 {
		entries.funcrefs = make([]*funcrefTokenEntry, 0, len(s.byToken))
		for _, entry := range s.byToken {
			entries.funcrefs = append(entries.funcrefs, entry)
		}
	}
	entries.claimGCResultRootsLocked(s)
	s.byIdentity = nil
	s.byToken = nil
	entries.claimCompactExternRoots(s.externrefs)
	clear(s.externrefs)
	s.externrefs = nil
	s.externKey = 0
	s.externSeed = 0
	s.externFree = 0
	entries.claimGCCollectorsLocked(s)
	return entries
}

// The caller has removed these exact token units from its maps. Distinct tokens
// may own separate roots on the same Instance: never deduplicate by owner. The
// per-call operations expose only metadata/collector phases for failure-ordering
// tests; the actual root decrement remains mandatory and noninjectable.
func releaseReferenceEntriesWith(entries referenceTokenEntries, notifyFuncref func(*funcrefTokenEntry), clearGC func(gcResultRoot), closeCollector func(*gc.Collector)) {
	var cleanup resourceCleanup
	for i, entry := range entries.funcrefs {
		entries.funcrefs[i] = nil
		owner := entry.owner
		cleanup.run(func() { notifyFuncref(entry) })
		cleanup.run(owner.releaseResourceRoot)
	}
	if collectorRuntimeForImage {
		entries.releaseGCResultRoots(&cleanup, clearGC)
	}
	entries.releaseDirectExternrefs(&cleanup)
	if collectorRuntimeForImage {
		entries.releaseGCCollectors(&cleanup, closeCollector)
	}
	cleanup.finish()
}

func releaseFuncrefTokenMetadata(entry *funcrefTokenEntry) {
	if hostOwner := entry.owner.hostFuncRefForDescriptor(entry.descriptor); hostOwner != nil {
		hostOwner.tokenReleased(entry.owner, entry.descriptor)
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
			if !railshotValidFuncrefPublication(s, source, descriptor) {
				return nil, 0, false
			}
			_, registered := s.instances[source]
			return source, descriptor, registered
		}
		if !hostFunctionImportsBuilt || fidx >= len(source.c.Imports) || fidx >= len(source.c.importFuncSigs) {
			return nil, 0, false
		}
		var binding any
		if railshotNativeContextTrapABI() {
			if native := source.importReferenceNative(fidx); native != nil {
				binding = native
			} else if host := source.importReferenceHost(fidx); host != nil {
				binding = host
			}
		}
		if binding == nil {
			binding = source.imports[source.c.functionImportBindingKey(fidx)]
		}
		off := (fidx + 1) * coreruntime.FuncRefDescBytes
		refSlot := binary.LittleEndian.Uint64(source.funcRefDescs[off+coreruntime.TableEntryRefSlotOffset:])
		if ex, ok := binding.(*InstanceExport); ok {
			if ex == nil || ex.inst == nil || ex.localIdx < 0 || ex.localIdx >= len(ex.inst.c.Entry) {
				return nil, 0, false
			}
			if railshotNativeContextTrapABI() {
				return source, descriptor, railshotCanonicalImportedFuncref(s, source, ex, fidx, descriptor)
			}
			if ex.inst.refStore != s {
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
		hostOwner, ok := binding.(*HostFuncRef)
		if !ok && railshotCanonicalPlainHostFuncref(s, source, fidx, descriptor) {
			return source, descriptor, true
		}
		if !ok || hostOwner == nil || hostOwner.store != s || refSlot != descriptor {
			return nil, 0, false
		}
		if railshotNativeContextTrapABI() {
			return railshotCanonicalHostFuncref(s, source, hostOwner, fidx, descriptor)
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
	if railshotNativeContextTrapABI() {
		return railshotCanonicalForeignFuncref(s, source, descriptor)
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
	return funcIndex, funcIndex >= 0 && funcIndex < len(in.c.FuncTypeID) && !railshotNonCallableFunction(in.c, funcIndex)
}

func (in *Instance) funcrefFunctionIdentity(descriptor uint64) (funcrefIdentity, bool) {
	fidx, ok := in.funcrefDescriptorIndex(descriptor)
	if !ok {
		return funcrefIdentity{}, false
	}
	if fidx >= in.c.NumImports {
		return funcrefIdentity{instance: in, localIdx: fidx - in.c.NumImports}, true
	}
	if !hostFunctionImportsBuilt || fidx >= len(in.c.Imports) {
		return funcrefIdentity{}, false
	}
	var export *InstanceExport
	if railshotNativeContextTrapABI() {
		export = in.importReferenceNative(fidx)
	} else {
		export, _ = in.imports[in.c.functionImportBindingKey(fidx)].(*InstanceExport)
	}
	if export == nil || export.inst == nil || export.localIdx < 0 {
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
	if !hostFunctionImportsBuilt {
		return nil
	}
	funcIndex, ok := in.funcrefDescriptorIndex(descriptor)
	if !ok || funcIndex < 0 || funcIndex >= in.c.NumImports || funcIndex >= len(in.c.Imports) {
		return nil
	}
	if railshotNativeContextTrapABI() {
		return in.importReferenceHost(funcIndex)
	}
	owner, _ := in.imports[in.c.functionImportBindingKey(funcIndex)].(*HostFuncRef)
	return owner
}

func (in *Instance) localFuncrefDescriptor(localIdx int) (uint64, bool) {
	fidx := in.c.NumImports + localIdx
	if localIdx < 0 || fidx < in.c.NumImports || fidx >= len(in.c.FuncTypeID) || len(in.funcRefDescs) == 0 || railshotNonCallableFunction(in.c, fidx) {
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
// canonical function identity rather than opaque public token bits, so imported aliases
// and cross-instance references remain stable across store tokenization.
func (in *Instance) FuncRefMatchesFunction(ref FuncRef, index uint32) bool {
	if in == nil || ref.token == 0 {
		return false
	}
	if railshotNativeContextTrapABI() {
		return railshotFuncRefMatchesFunction(in, ref, index)
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
	if !referenceStoreForImage {
		return nil, ErrPermissionDenied
	}
	in.revokePrivateGCEntry()
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
	if in.resourcesClosed || !railshotResourceRootRetainAllowedLocked(in) || (!finalization && in.invocationState.Load()&instanceInvocationClosed != 0) || in.resourceRefs == maxInt() {
		return false
	}
	in.resourceRefs++
	railshotResourceRootAcquiredLocked(in)
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
	railshotResourceRootReleasedLocked(in)
	closed := in.closed
	reconcile := closed && in.resourceRefs == 1 && !in.proxyReconciliation && !in.proxyReconciled
	in.lifeMu.Unlock()
	if closed {
		if managedFunctionDeferFinalization(in) {
			return
		}
		if reconcile {
			railshotReconcileClosedReferenceProxies(in)
		}
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
