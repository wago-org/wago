//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"sync"
	"sync/atomic"
)

// This explicit graph is a receipt prerequisite, not a collector. Its
// classifications do not change the anonymous root shadow. RV64 native-function
// attachments install authoritative members through a lazy architecture holder;
// other acquisitions and the host-testable explicit graphs stay independent.
const dependencyReceiptSlotBits = 12
const maxDependencyPrimitiveCapacity = 1 << dependencyReceiptSlotBits
const maxDependencyReceiptGeneration = (uint64(1) << (64 - dependencyReceiptSlotBits)) - 1

var (
	errDependencyCapacity = errors.New("dependency metadata capacity exhausted")
	errDependencyIdentity = errors.New("dependency identity exhausted or stale")
	errDependencyMember   = errors.New("dependency member is not authoritative")
	errDependencyState    = errors.New("dependency receipt state does not permit operation")
	errDependencyClosed   = errors.New("dependency owner is closed or finalizing")
	errDependencyLease    = errors.New("dependency consumer requires a construction or invocation lifetime")
	errDependencyKind     = errors.New("dependency kind is invalid")
	errDependencyGraph    = errors.New("dependency transfer requires the same graph")
)

type dependencyKind uint8

const (
	dependencyFunction dependencyKind = iota + 1
	dependencyMemory
	dependencyGlobalCell
	dependencyTableStorage
	dependencyFrozenDescriptor
	dependencyContainerDescriptor
)

type dependencyReceiptState uint8

const (
	dependencyReserved dependencyReceiptState = iota
	dependencyPending
	dependencyTyped
	dependencyExternal
	dependencyReleasing
	dependencyReleased
)

type dependencyMember struct {
	owner *Instance // immutable; supplied by the sole future Instance holder
	graph atomic.Pointer[dependencyGraph]
	// Written before active publishes registration, never reused thereafter.
	identity uint64
	active   atomic.Bool
	// Outstanding ownership, including Releasing, prevents source retirement.
	// Cross-graph external adoption updates this under the owner's lifeMu.
	outgoing atomic.Uint32
	// The authoritative graph's mutex protects receipt accounting below.
	receipts int
	external int
	incoming int
}

type dependencyRecord struct {
	generation uint64
	state      dependencyReceiptState
	target     *dependencyMember
	consumer   *dependencyMember
	kind       dependencyKind
}

// Copies share a slot/generation identity, so an alias cannot acquire another
// physical release. No address or Wasm index acts as an acquisition identity.
type dependencyReceipt struct {
	graph    *dependencyGraph
	identity uint64
}

type dependencyGraph struct {
	mu           sync.Mutex
	nodeCap      int
	receiptCap   int
	nextIdentity uint64
	owners       map[*Instance]*dependencyMember
	records      []dependencyRecord
	free         []uint16
	live         int
	// Optional per-graph test seam, immutable before the first release and invoked
	// only after real physical release.
	// There is no replaceable retain or decrement operation.
	afterRelease func(*Instance)
	// Production retirement runs after the releasing record has cleared, outside
	// graph/lifetime locks. Installed before any operations on this graph.
	afterReceiptRelease func(*dependencyMember, *dependencyMember)
}

func newDependencyGraph(nodeCap, receiptCap int) (*dependencyGraph, error) {
	if nodeCap <= 0 || nodeCap > maxDependencyPrimitiveCapacity || receiptCap <= 0 || receiptCap > maxDependencyPrimitiveCapacity {
		return nil, errDependencyCapacity
	}
	return &dependencyGraph{nodeCap: nodeCap, receiptCap: receiptCap, nextIdentity: 1}, nil
}

// Registration accepts a caller-owned unique member, not an unauthenticated raw
// descriptor. No global registry is installed by this standalone primitive.
func (g *dependencyGraph) register(member *dependencyMember) error {
	return g.registerWithRetainedRoot(member, false)
}

// registerRetained requires the caller to own a concrete physical root on this
// producer throughout registration. Frozen descriptors can legitimately reach
// a logically closed producer through such a guard. Registration acquires no
// root; even active-member reuse must verify that storage is still pinned.
func (g *dependencyGraph) registerRetained(member *dependencyMember) error {
	return g.registerWithRetainedRoot(member, true)
}

func (g *dependencyGraph) registerWithRetainedRoot(member *dependencyMember, retained bool) error {
	if g == nil || member == nil || member.owner == nil {
		return errDependencyMember
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if retained {
		member.owner.lifeMu.Lock()
		defer member.owner.lifeMu.Unlock()
		if member.owner.resourcesClosed || member.owner.resourceRefs <= 0 {
			return errDependencyClosed
		}
		if g.authoritativeLocked(member) {
			return nil
		}
	}
	if member.graph.Load() != nil || g.owners[member.owner] != nil {
		return errDependencyMember
	}
	if len(g.owners) == g.nodeCap {
		return errDependencyCapacity
	}
	if g.nextIdentity == 0 {
		return errDependencyIdentity
	}
	if !retained {
		member.owner.lifeMu.Lock()
		defer member.owner.lifeMu.Unlock()
		if dependencyConsumerClosedLocked(member.owner) {
			return errDependencyClosed
		}
	}
	// A concurrent registration through a different graph may use this same
	// member object. A failed CAS acquires neither membership nor a root.
	if !member.graph.CompareAndSwap(nil, g) {
		return errDependencyMember
	}
	if g.owners == nil {
		g.owners = make(map[*Instance]*dependencyMember)
	}
	member.identity = g.nextIdentity
	if g.nextIdentity == ^uint64(0) {
		g.nextIdentity = 0
	} else {
		g.nextIdentity++
	}
	g.owners[member.owner] = member
	member.active.Store(true)
	return nil
}

func (g *dependencyGraph) authoritativeLocked(member *dependencyMember) bool {
	return member != nil && member.owner != nil && member.graph.Load() == g && member.active.Load() && member.identity != 0 && g.owners[member.owner] == member
}

func (g *dependencyGraph) unregister(member *dependencyMember) error {
	if g == nil {
		return errDependencyMember
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.authoritativeLocked(member) {
		return errDependencyMember
	}
	member.owner.lifeMu.Lock()
	defer member.owner.lifeMu.Unlock()
	if member.receipts != 0 || member.outgoing.Load() != 0 || !member.owner.resourcesClosed {
		return errDependencyState
	}
	member.active.Store(false)
	delete(g.owners, member.owner)
	return nil
}

func (g *dependencyGraph) reserveLocked() (uint16, error) {
	if g.live == g.receiptCap {
		return 0, errDependencyCapacity
	}
	for len(g.free) != 0 {
		slot := g.free[len(g.free)-1]
		g.free = g.free[:len(g.free)-1]
		record := &g.records[slot]
		if record.generation == maxDependencyReceiptGeneration {
			continue // exhausted identities are never reused or wrapped
		}
		*record = dependencyRecord{generation: record.generation + 1, state: dependencyReserved}
		return slot, nil
	}
	if len(g.records) == g.receiptCap {
		return 0, errDependencyCapacity
	}
	slot := uint16(len(g.records))
	g.records = append(g.records, dependencyRecord{generation: 1, state: dependencyReserved})
	return slot, nil
}

func (g *dependencyGraph) acquirePending(target *dependencyMember, finalization bool) (dependencyReceipt, error) {
	if g == nil {
		return dependencyReceipt{}, errDependencyMember
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.authoritativeLocked(target) {
		return dependencyReceipt{}, errDependencyMember
	}
	slot, err := g.reserveLocked()
	if err != nil {
		return dependencyReceipt{}, err
	}
	// The real admission and shadow increment remain lifeMu-only. The caller
	// holds graph first; neither retain mode invokes cleanup or graph operations.
	if !target.owner.retainResourceRootMode(finalization) {
		g.records[slot].state = dependencyReleased
		g.free = append(g.free, slot)
		return dependencyReceipt{}, errDependencyClosed
	}
	record := &g.records[slot]
	record.target, record.state = target, dependencyPending
	target.receipts++
	target.external++
	g.live++
	return dependencyReceipt{graph: g, identity: record.generation<<dependencyReceiptSlotBits | uint64(slot)}, nil
}

func (g *dependencyGraph) recordLocked(identity uint64) (*dependencyRecord, uint16) {
	slot := uint16(identity & (maxDependencyPrimitiveCapacity - 1))
	if identity == 0 || int(slot) >= len(g.records) {
		return nil, 0
	}
	record := &g.records[slot]
	if record.generation != identity>>dependencyReceiptSlotBits {
		return nil, 0
	}
	return record, slot
}

func dependencyConsumerClosedLocked(owner *Instance) bool {
	return owner.closed || owner.finalizing || owner.resourcesClosed || owner.invocationState.Load()&instanceInvocationClosed != 0
}

func dependencyConsumerLeaseLocked(owner *Instance) bool {
	return owner.constructionActive || owner.invocationState.Load()&instanceInvocationCount != 0
}

// Same-graph endpoints have stable IDs. The graph mutex prevents unregistering
// either while locked. Cross-graph pending adoption does not change target
// accounting, so it takes only the consumer lifetime lock, never another graph.
type dependencyEndpointLocks struct {
	first, second *Instance
}

func lockDependencyEndpoints(target, consumer *dependencyMember, sameGraph bool) dependencyEndpointLocks {
	if !sameGraph || target == consumer {
		consumer.owner.lifeMu.Lock()
		return dependencyEndpointLocks{first: consumer.owner}
	}
	first, second := target, consumer
	if second.identity < first.identity {
		first, second = second, first
	}
	first.owner.lifeMu.Lock()
	second.owner.lifeMu.Lock()
	return dependencyEndpointLocks{first: first.owner, second: second.owner}
}

func (locks dependencyEndpointLocks) unlock() {
	if locks.second != nil {
		locks.second.lifeMu.Unlock()
	}
	locks.first.lifeMu.Unlock()
}

// adopt publishes an exact caller-owned receipt. The caller must own the
// consumer's construction/invocation lifetime and have already installed its
// rollback/terminal outgoing drain owner. Observing somebody else's active
// lease does not authorize publication; this primitive installs no Close hook.
func (r dependencyReceipt) adopt(consumer *dependencyMember, kind dependencyKind) (bool, error) {
	return r.commit(consumer, kind, false)
}

// transfer requires the same caller-owned receipt and destination lease/drain
// preconditions as adopt. It moves ownership; it does not create an alias root.
func (r dependencyReceipt) transfer(consumer *dependencyMember, kind dependencyKind) (bool, error) {
	return r.commit(consumer, kind, true)
}

func (r dependencyReceipt) commit(consumer *dependencyMember, kind dependencyKind, transfer bool) (bool, error) {
	if r.graph == nil || consumer == nil || consumer.owner == nil || consumer.graph.Load() == nil {
		return false, errDependencyMember
	}
	if kind < dependencyFunction || kind > dependencyContainerDescriptor {
		return false, errDependencyKind
	}
	g := r.graph
	g.mu.Lock()
	defer g.mu.Unlock()
	record, _ := g.recordLocked(r.identity)
	if record == nil {
		return false, errDependencyIdentity
	}
	if !transfer && record.state != dependencyPending || transfer && record.state != dependencyTyped && record.state != dependencyExternal {
		return false, errDependencyState
	}
	sameGraph := consumer.graph.Load() == g
	if transfer && !sameGraph {
		return false, errDependencyGraph
	}
	if sameGraph && !g.authoritativeLocked(consumer) {
		return false, errDependencyMember
	}
	locks := lockDependencyEndpoints(record.target, consumer, sameGraph)
	defer locks.unlock()
	if !consumer.active.Load() || consumer.identity == 0 {
		return false, errDependencyMember
	}
	if dependencyConsumerClosedLocked(consumer.owner) {
		return false, errDependencyClosed
	}
	if !dependencyConsumerLeaseLocked(consumer.owner) {
		return false, errDependencyLease
	}
	if record.consumer != consumer && consumer.outgoing.Load() == ^uint32(0) {
		return false, errDependencyCapacity
	}
	if record.consumer != consumer {
		consumer.outgoing.Add(1)
		if record.consumer != nil {
			record.consumer.outgoing.Add(^uint32(0))
		}
	}
	if sameGraph && record.state != dependencyTyped {
		record.target.external--
		record.target.incoming++
	}
	record.consumer, record.kind = consumer, kind
	if sameGraph {
		record.state = dependencyTyped
	} else {
		record.state = dependencyExternal
	}
	return sameGraph, nil
}

// release is called outside every store/native/container/lifetime lock. Claim
// once and demote before physical release; finalize the record in a defer even
// if actual finalization or the after-decrement test callback propagates panic.
func (r dependencyReceipt) release() bool { return r.releaseWithCleanup(nil) }

// releaseWithCounter permits physical release only after an exact counter-only
// claim commits. The claim must have no user callback or failure after its
// decrement. False or a pre-consumption panic restores this receipt, including
// its original classification, and keeps the same physical root for retry.
func (r dependencyReceipt) releaseWithCounter(consume func() bool) bool {
	if r.graph == nil || consume == nil {
		return false
	}
	g := r.graph
	g.mu.Lock()
	record, slot := g.recordLocked(r.identity)
	if record == nil || record.state != dependencyPending && record.state != dependencyTyped && record.state != dependencyExternal {
		g.mu.Unlock()
		return false
	}
	target, consumer, previous := record.target, record.consumer, record.state
	if previous == dependencyTyped {
		target.incoming--
		target.external++
	}
	record.state = dependencyReleasing
	g.mu.Unlock()
	var cleanup resourceCleanup
	consumed := false
	cleanup.run(func() { consumed = consume() })
	if !consumed {
		g.mu.Lock()
		// Other acquisitions may append and relocate records while unlocked.
		// Reacquire by generation identity; a Releasing slot cannot be reused.
		current, currentSlot := g.recordLocked(r.identity)
		if current == nil || currentSlot != slot || current.state != dependencyReleasing {
			g.mu.Unlock()
			cleanup.run(func() { panic("dependency counter release lost its claim") })
			cleanup.finish()
			return false
		}
		current.state = previous
		if previous == dependencyTyped {
			target.incoming++
			target.external--
		}
		g.mu.Unlock()
		cleanup.finish()
		return false
	}
	return r.releaseClaimed(slot, target, consumer, nil)
}

// A storage receipt must become non-adoptable before its counter obligation is
// consumed. The optional prerequisite runs after the exact graph release claim,
// outside graph/lifetime locks, before the real physical root decrement. Both
// cleanup steps complete and preserve the first panic.
func (r dependencyReceipt) releaseWithCleanup(before func()) bool {
	if r.graph == nil {
		return false
	}
	g := r.graph
	g.mu.Lock()
	record, slot := g.recordLocked(r.identity)
	if record == nil || record.state != dependencyPending && record.state != dependencyTyped && record.state != dependencyExternal {
		g.mu.Unlock()
		return false
	}
	target, consumer := record.target, record.consumer
	if record.state == dependencyTyped {
		target.incoming--
		target.external++
	}
	record.state = dependencyReleasing
	g.mu.Unlock()
	return r.releaseClaimed(slot, target, consumer, before)
}

// The counter commit (when present) has already happened. This path is
// irreversible: finalization completes even after a physical callback panic.
func (r dependencyReceipt) releaseClaimed(slot uint16, target, consumer *dependencyMember, before func()) bool {
	g := r.graph
	defer func() {
		g.mu.Lock()
		// A releasing slot cannot be recycled while this physical call runs.
		target.external--
		target.receipts--
		g.live--
		g.records[slot].target, g.records[slot].consumer = nil, nil
		g.records[slot].state = dependencyReleased
		if g.records[slot].generation < maxDependencyReceiptGeneration {
			g.free = append(g.free, slot)
		}
		// A foreign graph cannot inspect this record. Clear its source links and
		// publish Released before allowing that graph to retire the consumer.
		if consumer != nil {
			consumer.outgoing.Add(^uint32(0))
		}
		g.mu.Unlock()
		if g.afterReceiptRelease != nil {
			g.afterReceiptRelease(target, consumer)
		}
	}()
	var cleanup resourceCleanup
	if before != nil {
		cleanup.run(before)
	}
	cleanup.run(target.owner.releaseResourceRoot)
	if g.afterRelease != nil {
		cleanup.run(func() { g.afterRelease(target.owner) })
	}
	cleanup.finish()
	return true
}
