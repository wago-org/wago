//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"sync"
	"sync/atomic"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// These registries are cold for ordinary modules. Typed pointer keys avoid the
// interface boxing that sync.Map requires on each RV64 native-entry lifetime
// check once managed function storage is active.
var managedFunctionGraphs managedFunctionPointerRegistry[*referenceStore] // *referenceStore -> *managedFunctionGraph
var managedFunctionCollectors managedFunctionCollectorRegistry
var managedFunctionRegistryMu sync.Mutex                             // graph registration and last-domain retirement
var managedFunctionMembers managedFunctionPointerRegistry[*Instance] // *Instance -> *managedFunctionMember
var managedFunctionMemberCount atomic.Uint32

type managedFunctionPointerRegistry[K comparable] struct {
	mu   sync.RWMutex
	rows map[K]any
}

func (r *managedFunctionPointerRegistry[K]) Load(key K) (any, bool) {
	r.mu.RLock()
	v, ok := r.rows[key]
	r.mu.RUnlock()
	return v, ok
}
func (r *managedFunctionPointerRegistry[K]) Store(key K, value any) {
	r.mu.Lock()
	if r.rows == nil {
		r.rows = make(map[K]any)
	}
	r.rows[key] = value
	r.mu.Unlock()
}
func (r *managedFunctionPointerRegistry[K]) Delete(key K) {
	r.mu.Lock()
	delete(r.rows, key)
	r.mu.Unlock()
}

// Typed collector keys avoid interface boxing under strict pointer checks.
// Values keep the existing assertions at readers while the registry owns no
// collector or instance field.
type managedFunctionCollectorRegistry struct {
	mu   sync.RWMutex
	rows map[*gc.Collector]any
}

func (r *managedFunctionCollectorRegistry) Load(c *gc.Collector) (any, bool) {
	r.mu.RLock()
	v, ok := r.rows[c]
	r.mu.RUnlock()
	return v, ok
}
func (r *managedFunctionCollectorRegistry) Store(c *gc.Collector, v any) {
	r.mu.Lock()
	if r.rows == nil {
		r.rows = make(map[*gc.Collector]any)
	}
	r.rows[c] = v
	r.mu.Unlock()
}
func (r *managedFunctionCollectorRegistry) Delete(c *gc.Collector) {
	r.mu.Lock()
	delete(r.rows, c)
	r.mu.Unlock()
}

type managedFunctionGraph struct {
	mu                sync.Mutex
	store             *referenceStore
	epoch             atomic.Uint64
	members           map[*Instance]*managedFunctionMember
	rows              []*managedFunctionMember
	freeRows          []uint32
	retired           bool
	collectors        map[*gc.Collector]*managedFunctionCollector
	pendingReclaims   atomic.Uint64
	pendingMetadata   atomic.Bool
	pendingFinalizers atomic.Uint64
	pendingCollection atomic.Bool
	collecting        atomic.Bool
	drainingReclaims  bool
}

type managedFunctionMember struct {
	owner               *Instance
	graph               *managedFunctionGraph
	envPins             uint32 // identified physical roots, including queued reclamation
	row                 uint32
	registered          bool
	slots               []managedFunctionSlot
	collecting          atomic.Bool
	finalizationPending bool // graph.mu; no physical callbacks under a topology reader
}

// lifeMu is held by the physical root ledger. This cold lookup does not take
// another lock and cannot reverse the graph-to-lifetime commit lock order.
func managedFunctionRootEpochLocked(in *Instance) {
	if managedFunctionMemberCount.Load() == 0 {
		return
	}
	if value, ok := managedFunctionMembers.Load(in); ok {
		member := value.(*managedFunctionMember)
		member.graph.epoch.Add(1)
		if in.invocationState.Load()&instanceInvocationClosed != 0 {
			member.graph.pendingCollection.Store(true)
		}
	}
}

func managedFunctionRetainAllowedLocked(in *Instance) bool {
	if managedFunctionMemberCount.Load() != 0 {
		if value, ok := managedFunctionMembers.Load(in); ok {
			return !value.(*managedFunctionMember).collecting.Load()
		}
	}
	return true
}

type managedFunctionEnv struct {
	root        gc.Root
	member      *managedFunctionMember
	descriptors []uint64
}

type managedFunctionCollector struct {
	mu           sync.Mutex // observer journal only; maps are collector-domain serialized
	graph        *managedFunctionGraph
	collector    *gc.Collector
	typeID       gc.TypeID
	envs         map[gc.Ref]*managedFunctionEnv    // weak, removed before handle reuse
	byOwner      map[*Instance]*managedFunctionEnv // same weak identity
	byDescriptor map[uint64]*managedFunctionEnv
	shadows      map[gc.Ref]managedFunctionEdge   // inline first producer; null tombstones preserve Tiny cursors
	extraShadows map[gc.Ref][]managedFunctionEdge // cold overflow for multiple producers
	aggregate    *managedFunctionAggregateScratch
	globalOwners map[uint32]*managedFunctionMember
	tableOwners  map[uint32]*managedFunctionMember
	authorized   bool // only a validated joint mark may make slots conditional
	liveOwners   map[*managedFunctionMember]bool
	reclaimed    []*managedFunctionEnv
	closed       bool
	exception    *managedFunctionExceptionScratch
}

func (s *managedFunctionCollector) ObjectEdgeCount(ref gc.Ref) uint32 {
	if _, ok := s.shadows[ref]; ok {
		return 1 + uint32(len(s.extraShadows[ref]))
	}
	return 0
}
func (s *managedFunctionCollector) ObjectEdgeAt(ref gc.Ref, index uint32) gc.Ref {
	return s.objectShadowEdge(ref, index).ref
}
func (s *managedFunctionCollector) ObjectEdgesMutated(gc.Ref)  { s.graph.epoch.Add(1) }
func (s *managedFunctionCollector) ObjectRootPublished(gc.Ref) { s.graph.epoch.Add(1) }
func (s *managedFunctionCollector) PersistentRootIsStrong(kind gc.SlotKind, index uint32) bool {
	if s.authorized && kind == gc.SlotGlobal && !compactExternHostRootIsStrong(s.collector, index) {
		return false
	}
	var member *managedFunctionMember
	switch kind {
	case gc.SlotGlobal:
		member = s.globalOwners[index]
	case gc.SlotTable:
		member = s.tableOwners[index]
	}
	if member != nil && member.collecting.Load() {
		return false
	}
	if !s.authorized {
		return true
	}
	return member == nil || s.liveOwners[member]
}

func managedFunctionGraphFor(store *referenceStore) *managedFunctionGraph {
	if graph, ok := managedFunctionGraphs.Load(store); ok {
		return graph.(*managedFunctionGraph)
	}
	graph := &managedFunctionGraph{store: store, members: make(map[*Instance]*managedFunctionMember), collectors: make(map[*gc.Collector]*managedFunctionCollector)}
	// First activation is cold and may walk the already registered owners once.
	// Later registration/lifetime hooks update stable rows under store.mu.
	store.mu.Lock()
	graph.mu.Lock()
	for in, entry := range store.instances {
		if entry != nil && !entry.resourcesReleased {
			graph.registerMemberLocked(in, true)
		}
	}
	managedFunctionGraphs.Store(store, graph)
	graph.mu.Unlock()
	store.mu.Unlock()
	return graph
}

// Registration must publish the graph before registerInstance takes store.mu.
// Mutable function containers need lifecycle deferral even when no heap closure
// has allocated a collector environment yet.
func managedFunctionPrepareRegistration(store *referenceStore, in *Instance) {
	if store == nil || in == nil || !compiledManagedFunctionReachability(in.c) {
		return
	}
	if _, ok := managedFunctionGraphs.Load(store); ok {
		return
	}
	managedFunctionRegistryMu.Lock()
	defer managedFunctionRegistryMu.Unlock()
	managedFunctionGraphFor(store)
}

// graph.mu is held (or the new graph is unpublished).
func (g *managedFunctionGraph) registerMemberLocked(in *Instance, registered bool) *managedFunctionMember {
	if member := g.members[in]; member != nil {
		member.registered = member.registered || registered
		return member
	}
	member := &managedFunctionMember{owner: in, graph: g, registered: registered}
	if n := len(g.freeRows); n != 0 {
		member.row = g.freeRows[n-1]
		g.freeRows = g.freeRows[:n-1]
		g.rows[member.row] = member
	} else {
		member.row = uint32(len(g.rows))
		g.rows = append(g.rows, member)
	}
	g.members[in] = member
	managedFunctionMembers.Store(in, member)
	if state := in.pluginState.Load(); state != nil {
		managedFunctionInvocationStates.Store(state, g)
	}
	managedFunctionMemberCount.Add(1)
	g.epoch.Add(1)
	return member
}

func (g *managedFunctionGraph) removeMemberLocked(member *managedFunctionMember) {
	if member.finalizationPending {
		member.finalizationPending = false
		g.pendingFinalizers.Add(^uint64(0))
	}

	if state := g.collectors[member.owner.gc]; state != nil {
		for _, slot := range member.slots {
			if slot.kind == gc.SlotGlobal {
				delete(state.globalOwners, slot.index)
			} else {
				delete(state.tableOwners, slot.index)
			}
		}
	}
	member.slots = nil
	delete(g.members, member.owner)
	g.rows[member.row] = nil
	g.freeRows = append(g.freeRows, member.row)
	managedFunctionMembers.Delete(member.owner)
	if state := member.owner.pluginState.Load(); state != nil {
		managedFunctionInvocationStates.Delete(state)
	}
	managedFunctionMemberCount.Add(^uint32(0))
	g.epoch.Add(1)
}

// These hooks run with store.mu held, never from a collector callback.
func managedFunctionRegisteredLocked(store *referenceStore, in *Instance) {
	if value, ok := managedFunctionGraphs.Load(store); ok {
		g := value.(*managedFunctionGraph)
		g.mu.Lock()
		if !g.retired {
			g.registerMemberLocked(in, true)
		}
		g.mu.Unlock()
	}
}
func managedFunctionLifetimeLocked(store *referenceStore, in *Instance, released bool) {
	if managedFunctionMemberCount.Load() == 0 {
		return
	}
	if value, ok := managedFunctionGraphs.Load(store); ok {
		g := value.(*managedFunctionGraph)
		g.mu.Lock()
		if member := g.members[in]; member != nil && !g.retired {
			if released {
				member.registered = false
				if member.envPins == 0 {
					g.removeMemberLocked(member)
				}
			}
			g.epoch.Add(1)
		}
		g.mu.Unlock()
	}
}

// Called with the collector domain locked. The private empty struct occupies
// only its16-byte collector header; its producer edge belongs to the joint graph.
func (in *Instance) managedFunctionCollector(domain *gcStoreDomain) *managedFunctionCollector {
	if state, ok := managedFunctionCollectors.Load(in.gc); ok {
		return state.(*managedFunctionCollector)
	}
	managedFunctionRegistryMu.Lock()
	defer managedFunctionRegistryMu.Unlock()
	graph := managedFunctionGraphFor(in.refStore)
	id := gc.TypeID(in.gc.TypeCount())
	desc, err := gc.NewStructDesc(id, nil)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	if err := in.gc.AddTypes([]gc.TypeDesc{desc}); err != nil {
		panic(gcHelperFailure(err))
	}
	if domain != nil {
		in.refStore.mu.Lock()
		domain.types = append(domain.types, desc)
		domain.typeReps = append(domain.typeReps, gcDomainTypeRepresentative{})
		in.refStore.mu.Unlock()
	}
	state := &managedFunctionCollector{graph: graph, collector: in.gc, typeID: id,
		envs: make(map[gc.Ref]*managedFunctionEnv), byOwner: make(map[*Instance]*managedFunctionEnv), byDescriptor: make(map[uint64]*managedFunctionEnv),
		shadows: make(map[gc.Ref]managedFunctionEdge), globalOwners: make(map[uint32]*managedFunctionMember), tableOwners: make(map[uint32]*managedFunctionMember)}
	managedFunctionCollectors.Store(in.gc, state)
	graph.mu.Lock()
	graph.collectors[in.gc] = state
	graph.mu.Unlock()
	in.gc.SetObjectEdgeSource(state)
	in.gc.AddReclaimObserver(state.objectReclaimed)
	return state
}

func (s *managedFunctionCollector) objectReclaimed(ref gc.Ref) {
	s.mu.Lock()
	if ref.IsNull() {
		for ref, env := range s.envs {
			s.reclaimed = append(s.reclaimed, env)
			s.graph.pendingReclaims.Add(1)
			delete(s.envs, ref)
		}
		clear(s.byOwner)
		clear(s.byDescriptor)
		clear(s.shadows)
		clear(s.extraShadows)
		s.closed = true
	} else {
		delete(s.shadows, ref)
		delete(s.extraShadows, ref)
		if env := s.envs[ref]; env != nil {
			for _, descriptor := range env.descriptors {
				delete(s.byDescriptor, descriptor)
			}
			delete(s.envs, ref)
			delete(s.byOwner, env.member.owner)
			s.reclaimed = append(s.reclaimed, env)
			s.graph.pendingReclaims.Add(1)
		}
	}
	s.mu.Unlock()
	if ref.IsNull() {
		drainManagedFunctionReclaims(s.collector)
	}
}

// The source has a native invocation/argument/container lifetime proof. Lookup
// never acquires a replacement anonymous root and never dereferences the raw
// word until a physically retained descriptor directory proves membership.
func managedFunctionDescriptorOwner(source *Instance, descriptor uint64) *Instance {
	if source == nil || source.refStore == nil || descriptor == 0 {
		return nil
	}
	store := source.refStore
	store.mu.Lock()
	defer store.mu.Unlock()
	if entry := store.byIdentity[funcrefIdentity{descriptor: descriptor}]; entry != nil && store.hasInstanceResourcesLocked(entry.owner) {
		return entry.owner
	}
	if owner, canonical, valid := store.canonicalFuncrefOwnerLocked(source, descriptor); valid && canonical == descriptor && store.hasInstanceResourcesLocked(owner) {
		return owner
	}
	for candidate, state := range store.instances {
		if state.resourcesReleased || !candidate.reachesFuncrefDescriptor(descriptor) {
			continue
		}
		owner, canonical, valid := store.canonicalFuncrefOwnerLocked(candidate, descriptor)
		if valid && canonical == descriptor && store.hasInstanceResourcesLocked(owner) {
			return owner
		}
	}
	return nil
}

func (s *managedFunctionCollector) environmentForDescriptor(source *Instance, descriptor uint64, roots gc.RootSet) (gc.Ref, error) {
	if descriptor == 0 {
		return gc.Null(), nil
	}
	if env := s.byDescriptor[descriptor]; env != nil {
		return env.root.GetRef(), nil
	}
	producer := managedFunctionDescriptorOwner(source, descriptor)
	if producer == nil {
		return gc.Null(), fmt.Errorf("managed function descriptor has no live physical owner")
	}
	ref, err := s.newManagedFunctionEnv(producer, roots)
	if err != nil {
		return gc.Null(), err
	}
	env := s.envs[ref]
	env.descriptors = append(env.descriptors, descriptor)
	s.byDescriptor[descriptor] = env
	return ref, nil
}

func visitManagedFunctionFrameRoot(source *Instance, descriptor uint64, fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	if descriptor == 0 {
		return true
	}
	var state *managedFunctionCollector
	if value, ok := managedFunctionCollectors.Load(source.gc); ok {
		state = value.(*managedFunctionCollector)
	}
	var env *managedFunctionEnv
	if state != nil {
		env = state.byDescriptor[descriptor]
	}
	if env == nil {
		producer := managedFunctionDescriptorOwner(source, descriptor)
		if producer == nil {
			panic(gcHelperFailuref("function frame root has no live physical owner"))
		}
		if state != nil {
			env = state.byOwner[producer]
		}
		if producerSink, ok := sink.(managedFunctionProducerRootSink); ok && !producerSink.VisitFunctionProducer(producer) {
			return false
		}
	} else if producerSink, ok := sink.(managedFunctionProducerRootSink); ok && !producerSink.VisitFunctionProducer(env.member.owner) {
		return false
	}
	// Before a function has entered a managed container, its existing native
	// invocation/argument/import lifetime is strong. The joint root sink visits
	// that physical producer directly; a walk must not allocate an Env.
	if env == nil {
		return true
	}
	if sink != nil {
		return sink.VisitRootRef(env.root.GetRef())
	}
	return fn(&env.root)
}

func visitManagedFunctionOwnerFrameRoot(owner *Instance, collector *gc.Collector, fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	if producerSink, ok := sink.(managedFunctionProducerRootSink); ok && !producerSink.VisitFunctionProducer(owner) {
		return false
	}
	if managedFunctionMemberCount.Load() == 0 {
		return true
	}
	value, ok := managedFunctionCollectors.Load(collector)
	if !ok {
		return true
	}
	env := value.(*managedFunctionCollector).byOwner[owner]
	if env == nil {
		return true
	}
	if sink != nil {
		return sink.VisitRootRef(env.root.GetRef())
	}
	return fn(&env.root)
}

// newManagedFunctionEnv requires a proven physically live producer. Allocation
// roots must include the parked native frame and all earlier constructor Envs.
// Returning a Ref does not root it: the caller must publish a managed edge or
// constructor root before another collecting operation.
func (s *managedFunctionCollector) newManagedFunctionEnv(producer *Instance, roots gc.RootSet) (gc.Ref, error) {
	if env := s.byOwner[producer]; env != nil {
		return env.root.GetRef(), nil
	}
	if producer == nil || producer.refStore != s.graph.store || !producer.retainResourceRootForFinalization() {
		return gc.Null(), fmt.Errorf("managed function producer has no live physical owner")
	}
	s.graph.mu.Lock()
	member := s.graph.members[producer]
	if member == nil {
		member = s.graph.registerMemberLocked(producer, false)
	}
	member.envPins++
	s.graph.epoch.Add(1)
	s.graph.mu.Unlock()
	env := &managedFunctionEnv{member: member}
	ref, err := s.collector.NewStructDefaultWithRoots(s.typeID, roots)
	if err != nil {
		// Physical release can run arbitrary instance callbacks, so even failed
		// allocation relinquishes its root through the post-lock journal.
		s.mu.Lock()
		s.reclaimed = append(s.reclaimed, env)
		s.graph.pendingReclaims.Add(1)
		s.mu.Unlock()
		return gc.Null(), err
	}
	env.root = gc.Root(ref)
	s.envs[ref] = env
	s.byOwner[producer] = env
	return ref, nil
}

func drainManagedFunctionReclaims(collector *gc.Collector) {
	value, ok := managedFunctionCollectors.Load(collector)
	if !ok {
		return
	}
	s := value.(*managedFunctionCollector)
	s.mu.Lock()
	pending := s.reclaimed
	s.reclaimed = nil
	closed := s.closed
	s.mu.Unlock()
	if len(pending) != 0 {
		// Count queued work, not a batch already owned by a callback journal.
		// Another post-gate driver must not spin behind a blocked finalizer.
		s.graph.pendingReclaims.Add(^uint64(len(pending) - 1))
	}
	var cleanup resourceCleanup
	for _, env := range pending {
		s.graph.mu.Lock()
		env.member.envPins--
		if env.member.envPins == 0 && !env.member.registered {
			s.graph.removeMemberLocked(env.member)
		}
		s.graph.epoch.Add(1)
		s.graph.mu.Unlock()
		cleanup.run(func() {
			defer s.graph.epoch.Add(1)
			env.member.owner.releaseResourceRoot()
		})
	}
	clear(pending)
	s.mu.Lock()
	if len(s.reclaimed) == 0 && cap(pending) > cap(s.reclaimed) {
		s.reclaimed = pending[:0]
	}
	s.mu.Unlock()
	if closed {
		managedFunctionCollectors.Delete(collector)
		managedFunctionRegistryMu.Lock()
		s.graph.mu.Lock()
		delete(s.graph.collectors, collector)
		if len(s.graph.collectors) == 0 {
			s.graph.retired = true
			for _, member := range s.graph.members {
				s.graph.removeMemberLocked(member)
			}
			managedFunctionGraphs.Delete(s.graph.store)
		}
		s.graph.mu.Unlock()
		managedFunctionRegistryMu.Unlock()
	}
	cleanup.finish()
}
