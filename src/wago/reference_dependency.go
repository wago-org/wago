//go:build linux && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"sort"
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
	errDependencyCapacity   = errors.New("dependency metadata capacity exhausted")
	errDependencyIdentity   = errors.New("dependency identity exhausted or stale")
	errDependencyMember     = errors.New("dependency member is not authoritative")
	errDependencyState      = errors.New("dependency receipt state does not permit operation")
	errDependencyClosed     = errors.New("dependency owner is closed or finalizing")
	errDependencyLease      = errors.New("dependency consumer requires a construction or invocation lifetime")
	errDependencyKind       = errors.New("dependency kind is invalid")
	errDependencyGraph      = errors.New("dependency transfer requires the same graph")
	errDependencyCollecting = errors.New("dependency member has an active collection claim")
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
	identity   uint64
	active     atomic.Bool
	collecting atomic.Bool
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

// dependencyComponentStatus is the conservative result of passive cycle
// discovery. A candidate is not a collection claim: a later collector must
// atomically revalidate the returned identities, topology, root counts and
// lifecycle state before changing any physical ownership.
type dependencyComponentStatus uint8

const (
	dependencyComponentBlocked dependencyComponentStatus = iota
	dependencyComponentIncomplete
	dependencyComponentCandidate
)

type dependencyComponent struct {
	status             dependencyComponentStatus
	graph              *dependencyGraph
	members            []*dependencyMember
	memberIdentities   []uint64
	memberOutgoing     []uint32
	receiptIdentities  []uint64
	outgoingIdentities []uint64
	receiptSlots       int
	internalReceipts   int
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

// closedComponentMemberLocked is shared by discovery and exact revalidation.
// The caller holds g.mu and this member's lifeMu.
func (g *dependencyGraph) closedComponentMemberLocked(member *dependencyMember) bool {
	if !g.authoritativeLocked(member) {
		return false
	}
	owner := member.owner
	state := owner.closeState.Load()
	return !member.collecting.Load() && owner.closed && !owner.finalizing && !owner.resourcesClosed && !owner.constructionActive && owner.invocationState.Load() == instanceInvocationClosed &&
		state != nil && state.prepared.Load() && state.completed.Load() && state.terminalComplete.Load() && member.external == 0
}

// discoverClosedComponent passively finds the strongly connected component
// containing seed. It performs no release, lifecycle transition or admission
// change. nodeBudget bounds reachable topology; receiptBudget charges every
// allocated receipt slot, including released history. Together they bound the
// cold traversal work and allocation. Exhaustion returns Incomplete and leaves
// every physical root untouched.
//
// The graph lock precedes lifetime locks, matching receipt operations. Members
// are lifetime-locked in immutable identity order. No native, store or
// container lock is acquired here.
func (g *dependencyGraph) discoverClosedComponent(seed *dependencyMember, nodeBudget, receiptBudget int) dependencyComponent {
	result := dependencyComponent{status: dependencyComponentBlocked}
	if g == nil || seed == nil {
		return result
	}
	if nodeBudget <= 0 || receiptBudget <= 0 {
		result.status = dependencyComponentIncomplete
		return result
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.authoritativeLocked(seed) {
		return result
	}

	type edge struct {
		consumer, target *dependencyMember
		identity         uint64
	}
	edges := make([]edge, 0, min(g.live, receiptBudget))
	for i := range g.records {
		// Released slots still consume inspection work. Charging every allocated
		// slot keeps the caller's receipt budget an actual bound even after a
		// graph has accumulated cold, exhausted generations.
		if i == receiptBudget {
			result.status = dependencyComponentIncomplete
			return result
		}
		record := &g.records[i]
		if record.state != dependencyTyped {
			continue
		}
		edges = append(edges, edge{consumer: record.consumer, target: record.target, identity: record.generation<<dependencyReceiptSlotBits | uint64(i)})
	}

	// First find every node reachable from seed. Reverse reachability restricted
	// to that set then selects exactly seed's strongly connected component.
	forward := map[*dependencyMember]struct{}{seed: {}}
	work := []*dependencyMember{seed}
	for len(work) != 0 {
		current := work[len(work)-1]
		work = work[:len(work)-1]
		for _, edge := range edges {
			if edge.consumer != current {
				continue
			}
			if _, exists := forward[edge.target]; exists {
				continue
			}
			if len(forward) == nodeBudget {
				result.status = dependencyComponentIncomplete
				return result
			}
			forward[edge.target] = struct{}{}
			work = append(work, edge.target)
		}
	}
	reverse := map[*dependencyMember]struct{}{seed: {}}
	work = append(work, seed)
	for len(work) != 0 {
		current := work[len(work)-1]
		work = work[:len(work)-1]
		for _, edge := range edges {
			if edge.target != current {
				continue
			}
			if _, reachable := forward[edge.consumer]; !reachable {
				continue
			}
			if _, exists := reverse[edge.consumer]; exists {
				continue
			}
			reverse[edge.consumer] = struct{}{}
			work = append(work, edge.consumer)
		}
	}
	result.members = make([]*dependencyMember, 0, len(reverse))
	for member := range reverse {
		result.members = append(result.members, member)
	}
	sort.Slice(result.members, func(i, j int) bool { return result.members[i].identity < result.members[j].identity })
	for _, member := range result.members {
		member.owner.lifeMu.Lock()
	}
	lockedMembers := result.members
	defer func() {
		for i := len(lockedMembers) - 1; i >= 0; i-- {
			lockedMembers[i].owner.lifeMu.Unlock()
		}
	}()

	component := make(map[*dependencyMember]struct{}, len(result.members))
	for _, member := range result.members {
		component[member] = struct{}{}
		if !g.closedComponentMemberLocked(member) {
			result.members = nil
			return result
		}
	}

	incoming := make(map[*dependencyMember]int, len(result.members))
	outgoing := make(map[*dependencyMember]int, len(result.members))
	internalReceipts := 0
	receiptIdentities := make([]uint64, 0, len(result.members))
	outgoingIdentities := make([]uint64, 0, len(result.members))
	for _, edge := range edges {
		if _, consumerInside := component[edge.consumer]; consumerInside {
			outgoing[edge.consumer]++
			outgoingIdentities = append(outgoingIdentities, edge.identity)
		}
		if _, targetInside := component[edge.target]; !targetInside {
			continue
		}
		if _, consumerInside := component[edge.consumer]; !consumerInside {
			result.members = nil
			return result
		}
		incoming[edge.target]++
		internalReceipts++
		receiptIdentities = append(receiptIdentities, edge.identity)
	}
	// A zero-edge singleton is ordinary finalization work, not a dependency
	// cycle. Every physical root of a candidate must be one exact internal typed
	// receipt; pending, external, releasing and anonymous roots all block here.
	if internalReceipts == 0 {
		result.members = nil
		return result
	}
	for _, member := range result.members {
		if member.receipts != incoming[member] || member.incoming != incoming[member] || member.owner.resourceRefs != incoming[member] || uint64(member.outgoing.Load()) != uint64(outgoing[member]) {
			result.members = nil
			return result
		}
	}
	result.graph = g
	result.memberIdentities = make([]uint64, len(result.members))
	result.memberOutgoing = make([]uint32, len(result.members))
	for i, member := range result.members {
		result.memberIdentities[i] = member.identity
		result.memberOutgoing[i] = member.outgoing.Load()
	}
	result.receiptIdentities = receiptIdentities
	result.outgoingIdentities = outgoingIdentities
	result.receiptSlots = len(g.records)
	result.internalReceipts = internalReceipts
	result.status = dependencyComponentCandidate
	return result
}

// revalidateClosedComponent checks an exact passive candidate under the same
// graph-to-lifetime lock order as discovery. It is deliberately still not a
// collection claim: success changes no root or receipt state. The stored slot
// extent bounds this second scan; growth conservatively invalidates the
// candidate instead of extending work after discovery.
func (g *dependencyGraph) revalidateClosedComponent(candidate dependencyComponent) bool {
	return g.withRevalidatedClosedComponent(candidate, nil)
}

// withRevalidatedClosedComponent optionally runs a non-callback commit while
// the exact graph and member lifetime snapshot is still locked.
func (g *dependencyGraph) withRevalidatedClosedComponent(candidate dependencyComponent, commit func()) bool {
	if g == nil || candidate.status != dependencyComponentCandidate || candidate.graph != g || len(candidate.members) == 0 ||
		len(candidate.members) != len(candidate.memberIdentities) || len(candidate.members) != len(candidate.memberOutgoing) ||
		len(candidate.members) > g.nodeCap || candidate.receiptSlots <= 0 || candidate.receiptSlots > g.receiptCap ||
		candidate.internalReceipts == 0 || candidate.internalReceipts != len(candidate.receiptIdentities) || candidate.internalReceipts > candidate.receiptSlots ||
		len(candidate.outgoingIdentities) > candidate.receiptSlots {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.records) != candidate.receiptSlots {
		return false
	}
	for i, member := range candidate.members {
		if member == nil || i != 0 && candidate.memberIdentities[i-1] >= candidate.memberIdentities[i] || member.identity != candidate.memberIdentities[i] || !g.authoritativeLocked(member) {
			return false
		}
	}
	for _, member := range candidate.members {
		member.owner.lifeMu.Lock()
	}
	defer func() {
		for i := len(candidate.members) - 1; i >= 0; i-- {
			candidate.members[i].owner.lifeMu.Unlock()
		}
	}()

	component := make(map[*dependencyMember]int, len(candidate.members))
	for i, member := range candidate.members {
		component[member] = i
		if member.identity != candidate.memberIdentities[i] || member.outgoing.Load() != candidate.memberOutgoing[i] || !g.closedComponentMemberLocked(member) {
			return false
		}
	}

	incoming := make([]int, len(candidate.members))
	identities := make([]uint64, 0, candidate.internalReceipts)
	outgoingIdentities := make([]uint64, 0, len(candidate.outgoingIdentities))
	for slot := range g.records {
		record := &g.records[slot]
		if _, consumerInside := component[record.consumer]; consumerInside && record.state == dependencyTyped {
			outgoingIdentities = append(outgoingIdentities, record.generation<<dependencyReceiptSlotBits|uint64(slot))
		}
		target, targetInside := component[record.target]
		if !targetInside || record.state != dependencyTyped {
			continue
		}
		if _, consumerInside := component[record.consumer]; !consumerInside {
			return false
		}
		incoming[target]++
		identities = append(identities, record.generation<<dependencyReceiptSlotBits|uint64(slot))
	}
	if len(identities) != len(candidate.receiptIdentities) {
		return false
	}
	for i := range identities {
		if identities[i] != candidate.receiptIdentities[i] {
			return false
		}
	}
	if len(outgoingIdentities) != len(candidate.outgoingIdentities) {
		return false
	}
	for i := range outgoingIdentities {
		if outgoingIdentities[i] != candidate.outgoingIdentities[i] {
			return false
		}
	}
	for i, member := range candidate.members {
		if member.receipts != incoming[i] || member.incoming != incoming[i] || member.owner.resourceRefs != incoming[i] {
			return false
		}
	}
	if commit != nil {
		commit()
	}
	return true
}

// dependencyComponentClaim owns one synthetic guard root on every member. It
// only freezes acquisition; it does not drain receipts or release resources.
// The claim is pointer-owned because copying an atomic release identity would
// create ambiguous rollback authority.
type dependencyComponentClaim struct {
	graph   *dependencyGraph
	members []*dependencyMember
	active  atomic.Bool
}

// dependencyComponentRelease is an irreversible, atomically committed
// resourcesClosed claim for every member. The caller owns the detached
// physical callbacks and must run the complete physical-release journal before
// complete drops the synthetic guards.
type dependencyComponentRelease struct {
	claim      *dependencyComponentClaim
	finalizers []*instanceFinalizers
	// 0 is unclaimed/retryable, 1 is running, and 2 is complete. Completion is
	// published before a callback panic is rethrown from resourceCleanup.
	physicalJournals []atomic.Uint32
}

func (g *dependencyGraph) claimClosedComponent(candidate dependencyComponent) (*dependencyComponentClaim, bool) {
	claim := &dependencyComponentClaim{graph: g}
	if !g.withRevalidatedClosedComponent(candidate, func() {
		claim.members = append(claim.members, candidate.members...)
		for _, member := range claim.members {
			member.collecting.Store(true)
			member.owner.resourceRefs++
			railshotResourceRootAcquiredLocked(member.owner)
		}
		claim.active.Store(true)
	}) {
		return nil, false
	}
	return claim, true
}

// claimClosedComponentForPhysicalRelease atomically makes every member
// non-finalizable by ordinary single-instance lifetime code. The guard roots
// keep internal receipt releases from reaching an unclaimed zero while the
// caller runs physical teardown outside graph and lifetime locks.
func (g *dependencyGraph) claimClosedComponentForPhysicalRelease(candidate dependencyComponent) (*dependencyComponentRelease, bool) {
	claim := &dependencyComponentClaim{graph: g}
	release := &dependencyComponentRelease{claim: claim}
	if !g.withRevalidatedClosedComponent(candidate, func() {
		claim.members = append(claim.members, candidate.members...)
		release.finalizers = make([]*instanceFinalizers, len(claim.members))
		release.physicalJournals = make([]atomic.Uint32, len(claim.members))
		for i, member := range claim.members {
			member.collecting.Store(true)
			member.owner.resourceRefs++
			railshotResourceRootAcquiredLocked(member.owner)
			member.owner.resourcesClosed = true
			release.finalizers[i] = member.owner.finalizers
			member.owner.finalizers = nil
		}
		claim.active.Store(true)
	}) {
		return nil, false
	}
	return release, true
}

// runPhysicalJournal gives one caller exactly-once authority to execute a
// member's complete physical-release journal. Ordinary cleanup panics are
// delayed by the supplied resourceCleanup; the completed attestation is
// published before finish rethrows the first one. A direct journal panic before
// return resets authority for retry.
func (release *dependencyComponentRelease) runPhysicalJournal(index int, journal func(*resourceCleanup)) (completed bool) {
	if release == nil || release.claim == nil || journal == nil || index < 0 || index >= len(release.physicalJournals) || !release.claim.active.Load() || !release.physicalJournals[index].CompareAndSwap(0, 1) {
		return false
	}
	defer func() {
		if !completed {
			release.physicalJournals[index].CompareAndSwap(1, 0)
		}
	}()
	var cleanup resourceCleanup
	journal(&cleanup)
	// This journal owner is the only path that can consume the detached
	// callback. A direct pre-return panic leaves it for retry; a callback panic
	// is captured after the pointer is cleared and cannot be replayed.
	if finalizers := release.finalizers[index]; finalizers != nil {
		release.finalizers[index] = nil
		if finalizers.physical != nil {
			cleanup.run(finalizers.physical)
		}
	}
	release.physicalJournals[index].Store(2)
	completed = true
	cleanup.finish()
	return true
}

// release rolls back a nonphysical guard exactly once. An irreversible
// physical-release claim has a distinct completion path and cannot roll back
// resourcesClosed or its detached finalizers.
func (claim *dependencyComponentClaim) release() bool {
	if claim == nil || claim.graph == nil || len(claim.members) == 0 || !claim.active.Load() {
		return false
	}
	g := claim.graph
	g.mu.Lock()
	if !claim.active.Load() {
		g.mu.Unlock()
		return false
	}
	for _, member := range claim.members {
		member.owner.lifeMu.Lock()
	}
	for _, member := range claim.members {
		if !g.authoritativeLocked(member) || !member.collecting.Load() || member.owner.resourcesClosed || member.owner.resourceRefs == 0 {
			for i := len(claim.members) - 1; i >= 0; i-- {
				claim.members[i].owner.lifeMu.Unlock()
			}
			g.mu.Unlock()
			panic("dependency component claim lost its guard authority")
		}
	}
	claim.active.Store(false) // g.mu serializes every release owner
	for _, member := range claim.members {
		member.owner.resourceRefs--
		railshotResourceRootReleasedLocked(member.owner)
		member.collecting.Store(false)
	}
	for i := len(claim.members) - 1; i >= 0; i-- {
		claim.members[i].owner.lifeMu.Unlock()
	}
	g.mu.Unlock()
	var cleanup resourceCleanup
	for _, member := range claim.members {
		cleanup.run(member.owner.tryFinalize)
	}
	cleanup.finish()
	return true
}

// complete consumes an irreversible physical-release claim exactly once. It
// accepts only the post-journal state: every graph receipt and outgoing edge is
// gone and each member owns exactly its one synthetic guard. A failed check
// retains the guards and retry authority.
func (release *dependencyComponentRelease) complete() bool {
	if release == nil || release.claim == nil || release.claim.graph == nil || !release.claim.active.Load() {
		return false
	}
	claim, g := release.claim, release.claim.graph
	if len(release.physicalJournals) != len(claim.members) {
		return false
	}
	for i := range release.physicalJournals {
		if release.physicalJournals[i].Load() != 2 {
			return false
		}
	}
	g.mu.Lock()
	if !claim.active.Load() {
		g.mu.Unlock()
		return false
	}
	for _, member := range claim.members {
		member.owner.lifeMu.Lock()
	}
	for _, member := range claim.members {
		if !g.authoritativeLocked(member) || !member.collecting.Load() || !member.owner.resourcesClosed || member.owner.resourceRefs != 1 || member.receipts != 0 || member.external != 0 || member.incoming != 0 || member.outgoing.Load() != 0 {
			for i := len(claim.members) - 1; i >= 0; i-- {
				claim.members[i].owner.lifeMu.Unlock()
			}
			g.mu.Unlock()
			return false
		}
	}
	claim.active.Store(false)
	for _, member := range claim.members {
		member.owner.resourceRefs--
		railshotResourceRootReleasedLocked(member.owner)
		member.collecting.Store(false)
	}
	for i := len(claim.members) - 1; i >= 0; i-- {
		claim.members[i].owner.lifeMu.Unlock()
	}
	g.mu.Unlock()
	var cleanup resourceCleanup
	for _, member := range claim.members {
		cleanup.run(func() { railshotDependencyComponentReleaseCompleted(member.owner) })
	}
	cleanup.finish()
	return true
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
	if member.collecting.Load() || member.receipts != 0 || member.outgoing.Load() != 0 || !member.owner.resourcesClosed {
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
	if target.collecting.Load() {
		return dependencyReceipt{}, errDependencyCollecting
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
