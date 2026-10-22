//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var errManagedFunctionMarkChanged = errors.New("managed function ownership changed during joint mark")

type managedFunctionSlot struct {
	kind  gc.SlotKind
	index uint32
}

type managedFunctionObjectWork struct {
	ref    gc.Ref
	cursor gc.ObjectTraceCursor
}

type managedFunctionReceiptProof struct {
	slot   int
	record dependencyRecord
}

type managedFunctionMarkDomain struct {
	state         *managedFunctionCollector
	seen          []uint64 // one bit per compact handle; no per-object Go map entry
	work          []managedFunctionObjectWork
	at            int
	liveEnvs      []gc.Root
	nativeRoots   []gc.Root // live owners' native globals, tables, and element entries
	global, table uint32
	seedsDone     bool
	mark          *managedFunctionMark
	transient     gc.RootSet
}

type managedFunctionMark struct {
	graph              *managedFunctionGraph
	epoch              uint64
	domains            []*managedFunctionMarkDomain
	byCollector        map[*gc.Collector]*managedFunctionMarkDomain
	live               map[*managedFunctionMember]bool
	owners             []*managedFunctionMember
	ownerAt, ownerSlot int
	edges              map[*managedFunctionMember][]*managedFunctionMember
	incoming           map[*managedFunctionMember]int
	err                error
	complete           bool
	members            []*managedFunctionMember
	receipts           []managedFunctionReceiptProof
	nativeCursor       managedFunctionNativeRootCursor
	nativeDone         bool
}

// bindPersistentSlot transfers a private instance slot from the unconditional
// collector roots to its physical owner's conditional roots. Public token and
// host argument slots must never be bound here. Slot indexes are append-only.
func (s *managedFunctionCollector) bindPersistentSlot(owner *Instance, kind gc.SlotKind, index uint32) error {
	if owner == nil || owner.gc != s.collector || owner.refStore != s.graph.store {
		return fmt.Errorf("managed function slot has a different collector or Runtime")
	}
	if _, err := s.collector.PersistentSlotRef(kind, index); err != nil {
		return err
	}
	s.graph.mu.Lock()
	defer s.graph.mu.Unlock()
	member := s.graph.members[owner]
	if member == nil {
		member = s.graph.registerMemberLocked(owner, false)
	}
	var rows map[uint32]*managedFunctionMember
	switch kind {
	case gc.SlotGlobal:
		rows = s.globalOwners
	case gc.SlotTable:
		rows = s.tableOwners
	default:
		return fmt.Errorf("managed function slot has invalid kind")
	}
	if previous := rows[index]; previous != nil {
		if previous != member {
			return fmt.Errorf("managed function slot has a different physical owner")
		}
		return nil
	}
	rows[index] = member
	member.slots = append(member.slots, managedFunctionSlot{kind, index})
	s.graph.epoch.Add(1)
	return nil
}

// newMark requires an ordered lease over every participating collector and
// stable native execution. It changes no marks and acquires no physical roots.
// Public entry points do not use this API until their joint lease is installed.
func (g *managedFunctionGraph) newMark() *managedFunctionMark {
	m := &managedFunctionMark{graph: g, byCollector: make(map[*gc.Collector]*managedFunctionMarkDomain), live: make(map[*managedFunctionMember]bool), edges: make(map[*managedFunctionMember][]*managedFunctionMember), incoming: make(map[*managedFunctionMember]int)}
	g.mu.Lock()
	defer g.mu.Unlock()
	m.epoch = g.epoch.Load()
	for _, state := range g.collectors {
		domain := &managedFunctionMarkDomain{state: state, mark: m}
		m.domains = append(m.domains, domain)
		m.byCollector[state.collector] = domain
	}
	// Dependency receipt identity and physical counts are read under the same
	// graph-to-lifetime order as the ordinary dependency collector. A target with
	// a pending, external, releasing, or foreign consumer root stays strong.
	dependencies := rv64FunctionDependencyGraph()
	dependencies.mu.Lock()
	defer dependencies.mu.Unlock()
	for i := range dependencies.records {
		record := &dependencies.records[i]
		if record.state != dependencyTyped || record.target == nil || record.consumer == nil {
			continue
		}
		target := g.members[record.target.owner]
		consumer := g.members[record.consumer.owner]
		if target == nil || consumer == nil || !dependencies.authoritativeLocked(record.target) || !dependencies.authoritativeLocked(record.consumer) {
			continue
		}
		m.incoming[target]++
		m.edges[consumer] = append(m.edges[consumer], target)
		m.receipts = append(m.receipts, managedFunctionReceiptProof{i, *record})
	}
	for _, member := range g.rows {
		if member == nil {
			continue
		}
		m.members = append(m.members, member)
		owner := member.owner
		owner.lifeMu.Lock()
		closeState := owner.closeState.Load()
		quiesced := owner.closed && !owner.finalizing && !owner.constructionActive && owner.invocationState.Load() == instanceInvocationClosed && closeState != nil && closeState.prepared.Load() && closeState.terminalComplete.Load()
		strong := !quiesced || owner.resourceRefs != int(member.envPins)+m.incoming[member]
		if owner.resourcesClosed {
			strong = false
		}
		owner.lifeMu.Unlock()
		if strong {
			m.visitOwner(member)
		}
	}
	return m
}

func (m *managedFunctionMark) visitOwner(member *managedFunctionMember) {
	if member != nil && !m.live[member] {
		m.live[member] = true
		m.owners = append(m.owners, member)
	}
}

func (d *managedFunctionMarkDomain) VisitObjectRef(ref gc.Ref) {
	if d.mark.err != nil || !ref.IsObj() {
		return
	}
	index := uint32(ref) >> 1
	word, bit := int(index>>6), uint64(1)<<(index&63)
	if word < len(d.seen) && d.seen[word]&bit != 0 {
		return
	}
	hasChildren, err := d.state.collector.ObjectMayHaveChildren(ref)
	if err != nil {
		d.mark.err = err
		return
	}
	if word >= len(d.seen) {
		d.seen = append(d.seen, make([]uint64, word+1-len(d.seen))...)
	}
	d.seen[word] |= bit
	if env := d.state.envs[ref]; env != nil {
		d.liveEnvs = append(d.liveEnvs, gc.Root(ref))
		d.mark.visitOwner(env.member)
	}
	if hasChildren {
		d.work = append(d.work, managedFunctionObjectWork{ref: ref})
	}
}

func (d *managedFunctionMarkDomain) VisitRootRef(ref gc.Ref) bool {
	d.VisitObjectRef(ref)
	return d.mark.err == nil
}

// seedRoots supplies parked frames or other transient roots in this collector.
// Persistent owner roots are read separately; each reference stays in its domain.
func (m *managedFunctionMark) seedRoots(collector *gc.Collector, roots gc.RootSet) error {
	d := m.byCollector[collector]
	if d == nil {
		return fmt.Errorf("joint roots have no participating collector")
	}
	if roots != nil {
		if d.transient != nil {
			return fmt.Errorf("joint transient roots were supplied twice")
		}
		d.transient = roots
		if direct, ok := roots.(gc.DirectRootRefSet); ok {
			direct.RangeRootRefs(d)
		} else {
			roots.RangeRoots(func(slot gc.RootSlot) bool { return d.VisitRootRef(slot.GetRef()) })
		}
	}
	return m.err
}

// step scans at most budget persistent slots, owner edges, or object descriptor
// entries. The caller must reacquire the same globally ordered lease each time.
// An epoch change discards authority instead of using a stale reachability set.
func (m *managedFunctionMark) step(budget uint32) (uint32, bool, error) {
	if budget == 0 {
		return 0, false, fmt.Errorf("joint mark budget is zero")
	}
	if m.graph.epoch.Load() != m.epoch {
		return 0, false, errManagedFunctionMarkChanged
	}
	var used uint32
	for used < budget {
		progress := false
		for _, d := range m.domains {
			if d.seedsDone {
				continue
			}
			kind, index := gc.SlotGlobal, &d.global
			if *index == d.state.collector.PersistentSlotCount(kind) {
				kind, index = gc.SlotTable, &d.table
			}
			if *index == d.state.collector.PersistentSlotCount(kind) {
				d.seedsDone = true
				continue
			}
			owners := d.state.globalOwners
			if kind == gc.SlotTable {
				owners = d.state.tableOwners
			}
			if owners[*index] == nil && (kind != gc.SlotGlobal || compactExternHostRootIsStrong(d.state.collector, *index)) {
				ref, err := d.state.collector.PersistentSlotRef(kind, *index)
				if err != nil {
					return used, false, err
				}
				d.VisitObjectRef(ref)
			}
			*index++
			used++
			progress = true
			break
		}
		if !progress && m.ownerAt < len(m.owners) {
			owner := m.owners[m.ownerAt]
			if m.ownerSlot < len(owner.slots) {
				slot := owner.slots[m.ownerSlot]
				d := m.byCollector[owner.owner.gc]
				if d == nil {
					return used, false, fmt.Errorf("owner root has no participating collector")
				}
				ref, err := d.state.collector.PersistentSlotRef(slot.kind, slot.index)
				if err != nil {
					return used, false, err
				}
				d.VisitObjectRef(ref)
				m.ownerSlot++
			} else {
				if !m.nativeDone {
					done, err := m.traceNativeOwnerRoot(owner.owner, &m.nativeCursor)
					if err != nil {
						return used, false, err
					}
					m.nativeDone = done
					used++
					continue
				}
				// Receipt fanout is also charged one edge per step.
				edge := m.ownerSlot - len(owner.slots)
				if edge < len(m.edges[owner]) {
					m.visitOwner(m.edges[owner][edge])
					m.ownerSlot++
				} else {
					m.ownerAt++
					m.ownerSlot = 0
					m.nativeCursor = managedFunctionNativeRootCursor{}
					m.nativeDone = false
				}
			}
			used++
			progress = true
		}
		if !progress {
			for _, d := range m.domains {
				if d.at == len(d.work) {
					continue
				}
				work := d.work[d.at]
				scanned, complete, err := d.state.collector.TraceObjectChildren(work.ref, &work.cursor, budget-used, d)
				d.work[d.at] = work
				used += scanned
				if err != nil {
					return used, false, err
				}
				if complete {
					d.at++
				}
				// A scalar/empty descriptor can complete without an entry. Charge the
				// queue row to prevent an unbounded sequence of empty objects.
				if scanned == 0 {
					used++
				}
				progress = true
				break
			}
		}
		if m.err != nil {
			return used, false, m.err
		}
		if !progress {
			if m.graph.epoch.Load() != m.epoch {
				return used, false, errManagedFunctionMarkChanged
			}
			m.complete = true
			return used, true, nil
		}
	}
	return used, false, nil
}

// collect commits only a completed unchanged mark under the caller's joint
// lease. The live Env roots bridge cross-domain edges for each local collector.
// Reclamation journals must be drained after all collector locks are released.
func (m *managedFunctionMark) collect() error {
	if !m.complete {
		return errManagedFunctionMarkChanged
	}
	// The caller's topology/execution lease freezes directories. Exact physical
	// root revalidation and claims close the acquisition race at commit.
	dependencies := rv64FunctionDependencyGraph()
	dependencies.mu.Lock()
	for _, proof := range m.receipts {
		if proof.slot >= len(dependencies.records) || dependencies.records[proof.slot] != proof.record {
			dependencies.mu.Unlock()
			return errManagedFunctionMarkChanged
		}
	}
	for _, member := range m.members {
		member.owner.lifeMu.Lock()
	}
	if m.graph.epoch.Load() != m.epoch {
		for i := len(m.members) - 1; i >= 0; i-- {
			m.members[i].owner.lifeMu.Unlock()
		}
		dependencies.mu.Unlock()
		return errManagedFunctionMarkChanged
	}
	for _, member := range m.members {
		if !m.live[member] {
			member.collecting.Store(true)
		}
	}
	for i := len(m.members) - 1; i >= 0; i-- {
		m.members[i].owner.lifeMu.Unlock()
	}
	dependencies.mu.Unlock()
	// Dead owners remain claimed until physical cleanup retires their member.
	// Their native cells can still contain collected references while outgoing
	// receipt cleanup runs; no external lifetime may resurrect that state.
	for _, d := range m.domains {
		d.state.authorized = true
		d.state.liveOwners = m.live
	}
	defer func() {
		for _, d := range m.domains {
			d.state.authorized = false
			d.state.liveOwners = nil
		}
	}()
	// Unreachable closed owners cannot acquire a new root while claimed. Clear
	// their private root slots before local collection can invalidate the values.
	for _, member := range m.members {
		if m.live[member] {
			continue
		}
		d := m.byCollector[member.owner.gc]
		if d == nil && len(member.slots) != 0 {
			return fmt.Errorf("owner slots have no participating collector")
		}
		for _, slot := range member.slots {
			var err error
			if slot.kind == gc.SlotGlobal {
				err = d.state.collector.SetGlobalSlot(slot.index, gc.Null())
			} else {
				err = d.state.collector.SetTableSlot(slot.index, gc.Null())
			}
			if err != nil {
				return err
			}
		}
	}
	for _, d := range m.domains {
		if err := d.clearUnreachableHostExternRoots(); err != nil {
			return err
		}
	}
	for _, d := range m.domains {
		roots := gc.RootGroups{{Class: gc.RootForeignInstance, Roots: managedFunctionEnvRoots(d.liveEnvs)}, {Class: gc.RootForeignInstance, Roots: managedFunctionEnvRoots(d.nativeRoots)}, {Class: gc.RootNativeFrame, Roots: d.transient}}
		if err := d.state.collector.CollectFull(roots); err != nil {
			return err
		}
	}
	return nil
}

type managedFunctionEnvRoots []gc.Root

func (r managedFunctionEnvRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	for i := range r {
		if !fn(&r[i]) {
			return
		}
	}
}
func (r managedFunctionEnvRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	for _, ref := range r {
		if !sink.VisitRootRef(ref.GetRef()) {
			return false
		}
	}
	return true
}
