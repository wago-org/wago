//go:build (linux || (darwin && arm64)) && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Identity is the actual opaque conversion, never a compact interpretation of
// its word. Neither identity nor domain retains the publishing Instance.
// All heap/receipt operations require topology -> ordered domains -> native
// authority. store.mu protects directory publication; graph.mu protects journals.
type gcConversionIdentity struct {
	store        *referenceStore
	domain       *gcStoreDomain
	word         uint64
	publicExtern uint64
	ref          gc.Ref
	floating     uint32
	publisher    *gcExternConversionState
	receipts     map[*gcConversionReceipt]struct{}
}
type gcConversionCore struct {
	owner  *Instance // cleared at publisher teardown
	domain *gcStoreDomain
}

// A container is one physical storage allocation, shared by every alias of it.
// Distinct containers must use distinct objects even for equal slot indexes.
type gcConversionContainer struct {
	storage    []byte
	owner      *Instance
	hostTable  *tableOwner
	hostGlobal *globalOwner
	store      *referenceStore
	slots      map[uint64]*gcConversionReceipt
	closed     bool
	cell       bool // a physical global cell has exactly one slot
}
type gcConversionReceipt struct {
	identity  *gcConversionIdentity
	container *gcConversionContainer
	index     uint64
	root      uint32
	released  bool
}
type gcConversionTransaction struct {
	container *gcConversionContainer
	index     uint64
	old, next *gcConversionReceipt
	done      bool
}

// acquireReceiptRoot reuses only roots detached by detachReceiptRoot in this
// exact collector domain. Receipt roots share the collector's table directory
// with public roots, so the domain-owned free list is the authority that keeps
// ordinary table slots out of this reuse path.
func acquireReceiptRoot(domain *gcStoreDomain, ref gc.Ref) (uint32, error) {
	if n := len(domain.receiptFreeSlots); n != 0 {
		root := domain.receiptFreeSlots[n-1]
		if err := domain.collector.SetTableSlot(root, ref); err != nil {
			return 0, err
		}
		domain.receiptFreeSlots = domain.receiptFreeSlots[:n-1]
		return root, nil
	}
	return domain.collector.NewCheckedTableSlot(ref)
}

func clearReceiptRoot(domain *gcStoreDomain, root uint32) {
	if err := domain.collector.SetTableSlot(root, gc.Null()); err != nil {
		panic(gcHelperFailure(err))
	}
}

// detachReceiptRoot makes a cleared root reusable only after every ownership
// directory has been detached. A retired domain is detached without publishing
// its closed collector storage to the free list.
func detachReceiptRoot(state *managedFunctionCollector, domain *gcStoreDomain, root uint32, recycle bool) {
	state.graph.mu.Lock()
	delete(state.conversionHostSlots, root)
	member := state.tableOwners[root]
	delete(state.tableOwners, root)
	if member != nil {
		for i, slot := range member.slots {
			if slot.collector == domain.collector && slot.kind == gc.SlotTable && slot.index == root {
				member.slots = append(member.slots[:i], member.slots[i+1:]...)
				break
			}
		}
	}
	state.graph.epoch.Add(1)
	state.graph.mu.Unlock()
	if recycle {
		domain.receiptFreeSlots = append(domain.receiptFreeSlots, root)
	}
}

func bindGCConversionCore(in *Instance) error {
	s := in.existingGCExternConversionState()
	if s == nil || in.refStore == nil {
		return nil
	}
	store := in.refStore
	topology := store.ensureGCTopology()
	topology.Lock()
	defer topology.Unlock()
	store.mu.Lock()
	entry := store.instances[in]
	if entry == nil || entry.gcDomain == nil {
		store.mu.Unlock()
		return fmt.Errorf("conversion publisher has no domain")
	}
	domain := entry.gcDomain
	if domain.private {
		domain.private = false
		domain.conversionOwned = true
		domain.refs = 1
		topology.appendDomainLocked(domain)
		for {
			old := in.executionFlags.Load()
			if in.executionFlags.CompareAndSwap(old, old|executionFlagStoreOwnedGCCollector) {
				break
			}
		}
	}
	if domain.conversionPublishers == ^uint32(0) {
		store.mu.Unlock()
		return fmt.Errorf("too many conversion publishers")
	}
	domain.conversionPublishers++
	store.mu.Unlock()
	s.core = &gcConversionCore{owner: in, domain: domain}
	return nil
}

func newGCConversionContainer(owner *Instance, storage []byte) (*gcConversionContainer, error) {
	if owner == nil || owner.refStore == nil {
		return nil, fmt.Errorf("conversion container has no physical owner")
	}
	owner.lifeMu.Lock()
	live := !owner.resourcesClosed
	owner.lifeMu.Unlock()
	if !live || managedFunctionOwnerClaimed(owner) {
		return nil, fmt.Errorf("conversion container owner retired")
	}
	if len(storage) < 8 {
		return nil, fmt.Errorf("conversion container descriptor unavailable")
	}
	physical := false
	if owner.c != nil {
		for i := owner.c.tableImportCount(); i < owner.c.tableCount(); i++ {
			descriptor := owner.tableDescriptor(i)
			if len(descriptor) > 0 && &descriptor[0] == &storage[0] && owner.c.tableElementType(i) == ValExternRef {
				physical = true
				break
			}
		}
	}
	cell := false
	if !physical && owner.c != nil {
		for i, g := range owner.globalCells {
			if i < len(owner.c.GlobalImports) || owner.c.Globals[i].Type != ValExternRef || g == nil || len(g.cell) < 8 {
				continue
			}
			if &g.cell[0] == &storage[0] {
				physical, cell = true, true
				break
			}
		}
	}
	if !physical {
		return nil, fmt.Errorf("conversion container does not belong to its physical owner")
	}
	store := owner.refStore
	store.ensureGCTopology()
	store.mu.Lock()
	defer store.mu.Unlock()
	if c := store.gcDomains.conversionContainers[&storage[0]]; c != nil {
		if c.owner != owner || c.closed {
			return nil, fmt.Errorf("conversion container ownership mismatch")
		}
		return c, nil
	}
	c := &gcConversionContainer{owner: owner, store: store, storage: storage, cell: cell, slots: make(map[uint64]*gcConversionReceipt)}
	if store.gcDomains.conversionContainers == nil {
		store.gcDomains.conversionContainers = make(map[*byte]*gcConversionContainer)
	}
	store.gcDomains.conversionContainers[&storage[0]] = c
	return c, nil
}

func (s *gcExternConversionState) conversionIdentity(word uint64) (*gcConversionIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.core == nil {
		return nil, fmt.Errorf("conversion publisher unavailable")
	}
	entry, err := s.dataEntryForExternLocked(word)
	if err != nil || entry == nil {
		return nil, err
	}
	if entry.uses == ^uint32(0) {
		return nil, fmt.Errorf("GC extern conversion ownership overflow")
	}
	store := s.store
	store.mu.Lock()
	identity := store.gcDomains.conversions[word]
	store.mu.Unlock()
	if identity != nil {
		return identity, nil
	}
	state := s.core.owner.managedFunctionCollector(s.core.domain)
	identity = &gcConversionIdentity{store: store, domain: s.core.domain, word: word, publicExtern: entry.publicExternWord, ref: entry.ref, floating: entry.rootSlot, publisher: s, receipts: make(map[*gcConversionReceipt]struct{})}
	store.mu.Lock()
	if store.gcDomains.domainForCollectorLocked(identity.domain.collector) != identity.domain {
		store.mu.Unlock()
		return nil, fmt.Errorf("conversion domain retired")
	}
	if store.gcDomains.conversions == nil {
		store.gcDomains.conversions = make(map[uint64]*gcConversionIdentity)
	}
	store.gcDomains.conversions[word] = identity
	store.mu.Unlock()
	state.graph.epoch.Add(1)
	return identity, nil
}

// prepare acquires a distinct root and domain claim before any visible write.
// source may be nil for an identity already registered by another publisher.
func (c *gcConversionContainer) prepare(index, word uint64, source *gcExternConversionState) (*gcConversionTransaction, error) {
	if c == nil || c.closed {
		return nil, fmt.Errorf("conversion container retired")
	}
	limit := uint64(1)
	if !c.cell {
		limit = uint64(binary.LittleEndian.Uint32(c.storage))
	}
	return c.prepareLimit(index, word, limit, source)
}
func (c *gcConversionContainer) prepareLimit(index, word, limit uint64, source *gcExternConversionState) (*gcConversionTransaction, error) {
	if c == nil || c.closed {
		return nil, fmt.Errorf("conversion container retired")
	}
	if c.owner != nil {
		if managedFunctionOwnerClaimed(c.owner) {
			return nil, fmt.Errorf("conversion owner claimed")
		}
		c.owner.lifeMu.Lock()
		live := !c.owner.resourcesClosed
		c.owner.lifeMu.Unlock()
		if !live {
			return nil, fmt.Errorf("conversion owner released")
		}
	} else if !c.hostLive() {
		return nil, fmt.Errorf("host conversion owner released")
	}
	if c.cell && limit != 1 || !c.cell && (len(c.storage) < 8 || limit > uint64(binary.LittleEndian.Uint32(c.storage[4:])) || limit > uint64((len(c.storage)-8)/8)) {
		return nil, fmt.Errorf("conversion storage extent invalid")
	}
	if index >= limit {
		return nil, fmt.Errorf("conversion container slot out of bounds")
	}
	tx := &gcConversionTransaction{container: c, index: index, old: c.slots[index]}
	if tx.old != nil && tx.old.identity.word == word {
		tx.next = tx.old
		return tx, nil
	}
	if word == 0 {
		return tx, nil
	}
	c.store.mu.Lock()
	identity := c.store.gcDomains.conversions[word]
	c.store.mu.Unlock()
	if identity == nil && source != nil {
		if source.store != c.store {
			return nil, fmt.Errorf("conversion crosses Runtime")
		}
		var err error
		identity, err = source.conversionIdentity(word)
		if err != nil {
			return nil, err
		}
	}
	if identity == nil {
		if _, ok := c.store.resolveExternref(word); !ok {
			return nil, fmt.Errorf("unknown opaque conversion identity")
		}
		return tx, nil
	}
	domain := identity.domain
	c.store.mu.Lock()
	if c.store.gcDomains.domainForCollectorLocked(domain.collector) != domain || domain.claims == ^uint32(0) {
		c.store.mu.Unlock()
		return nil, fmt.Errorf("conversion domain has no receipt authority")
	}

	if c.owner != nil {
		registration := c.store.instances[c.owner]
		if registration == nil || registration.resourcesReleased {
			c.store.mu.Unlock()
			return nil, fmt.Errorf("conversion physical owner is unregistered")
		}
		if !registration.dynamicInvocationDomains {
			c.store.mu.Unlock()
			return nil, fmt.Errorf("conversion physical owner lacks pre-admission")
		}
	}
	domain.claims++
	domain.conversionClaims++
	c.store.mu.Unlock()
	stateValue, ok := managedFunctionCollectors.Load(domain.collector)
	if !ok {
		panic("conversion collector graph absent")
	}
	state := stateValue.(*managedFunctionCollector)
	root, err := acquireReceiptRoot(domain, identity.ref)
	if err != nil {
		queueConversionClaim(state.graph, domain)
		return nil, err
	}
	if c.owner != nil {
		err = state.bindPersistentSlot(c.owner, gc.SlotTable, root)
	}
	if err != nil {
		clearReceiptRoot(domain, root)
		// bindPersistentSlot changes no ownership metadata on failure. The
		// acquired root was either new or already detached by this domain.
		domain.receiptFreeSlots = append(domain.receiptFreeSlots, root)
		queueConversionClaim(state.graph, domain)
		return nil, err
	}
	receipt := &gcConversionReceipt{identity: identity, container: c, index: index, root: root}
	if c.owner == nil {
		if state.conversionHostSlots == nil {
			state.conversionHostSlots = make(map[uint32]*gcConversionContainer)
		}
		state.conversionHostSlots[root] = c
		state.graph.epoch.Add(1)
	}
	identity.receipts[receipt] = struct{}{}
	tx.next = receipt
	return tx, nil
}

// commit publishes only after prepare. A rejecting callback must leave storage
// unchanged. Bulk callers prepare their entire range before committing any cell.
func (tx *gcConversionTransaction) commit(publish func() error) error {
	if tx == nil || tx.done {
		return fmt.Errorf("conversion transaction already consumed")
	}
	defer tx.rollback()
	if tx.container.slots[tx.index] != tx.old {
		tx.rollback()
		return fmt.Errorf("conversion transaction changed physical slot")
	}
	if tx.container.closed {
		tx.rollback()
		return fmt.Errorf("conversion container retired")
	}
	if err := publish(); err != nil {
		tx.rollback()
		return err
	}

	tx.done = true
	if tx.next != nil && tx.next.identity.publisher != nil {
		identity := tx.next.identity
		if identity.ref.IsObj() {
			stateValue, _ := managedFunctionCollectors.Load(identity.domain.collector)
			state := stateValue.(*managedFunctionCollector)
			if state.conversionFloating == nil {
				state.conversionFloating = make(map[uint32]*gcConversionIdentity)
			}
			state.conversionFloating[identity.floating] = identity
		}
	}
	if tx.next == nil {
		delete(tx.container.slots, tx.index)
	} else {
		tx.container.slots[tx.index] = tx.next
	}
	if tx.old != tx.next {
		tx.old.release()
	}
	return nil
}
func (tx *gcConversionTransaction) rollback() {
	if tx == nil || tx.done {
		return
	}
	tx.done = true
	if tx.next != tx.old {
		tx.next.release()
	}
}
func queueConversionClaim(g *managedFunctionGraph, domain *gcStoreDomain) {
	g.mu.Lock()
	g.receiptReleases = append(g.receiptReleases, domain)
	g.mu.Unlock()
	g.pendingMetadata.Store(true)
	g.pendingCollection.Store(true)
}
func (r *gcConversionReceipt) release() {
	if r == nil || r.released {
		return
	}
	r.released = true
	identity := r.identity
	store, domain := identity.store, identity.domain
	store.mu.Lock()
	registered := store.gcDomains.domainForCollectorLocked(domain.collector) == domain
	store.mu.Unlock()
	// Store retirement may have consumed the domain. Its pointer is then metadata
	// only; do not touch the closed heap or infer authority from a remaining owner.
	if registered {
		clearReceiptRoot(domain, r.root)
	}
	delete(identity.receipts, r)
	if r.container.slots[r.index] == r {
		delete(r.container.slots, r.index)
	}
	if stateValue, ok := managedFunctionCollectors.Load(domain.collector); ok {
		state := stateValue.(*managedFunctionCollector)
		detachReceiptRoot(state, domain, r.root, registered)
		if registered {
			queueConversionClaim(state.graph, domain)
		}
	}
	if len(identity.receipts) == 0 && identity.publisher == nil {
		store.mu.Lock()
		delete(store.gcDomains.conversions, identity.word)
		store.mu.Unlock()
	}
}
func (c *gcConversionContainer) release() {
	if c == nil || c.closed {
		return
	}
	c.closed = true
	for _, receipt := range c.slots {
		receipt.release()
	}
	c.store.mu.Lock()
	if len(c.storage) != 0 && c.store.gcDomains.conversionContainers[&c.storage[0]] == c {
		delete(c.store.gcDomains.conversionContainers, &c.storage[0])
	}
	c.store.mu.Unlock()
	c.storage = nil
	c.owner = nil
}
func drainConversionClaims(g *managedFunctionGraph) {
	g.mu.Lock()
	pending := g.receiptReleases
	g.receiptReleases = nil
	g.mu.Unlock()

	for _, domain := range pending {
		topology := g.store.ensureGCTopology()
		topology.Lock()
		g.store.mu.Lock()
		closeCollector := false
		if topology.domainForCollectorLocked(domain.collector) == domain {
			if domain.conversionClaims == 0 || domain.claims == 0 {
				g.store.mu.Unlock()
				topology.Unlock()
				panic("conversion claim journal underflow")
			}
			domain.conversionClaims--
			domain.claims--
			if domain.refs == 0 && domain.claims == 0 {
				closeCollector = topology.unlinkDomainLocked(domain)
			}
		}
		g.store.mu.Unlock()
		topology.Unlock()
		if closeCollector {
			domain.collector.Close()
		}
	}

}
func conversionAddClaimedDomainsLocked(store *referenceStore, set *gcInvocationDomainSet) {
	if store.gcDomains == nil {
		return
	}
	// A released receipt leaves the opaque directory before its deferred claim
	// is consumed. Enumerate registered authority, including that final interval.
	for domain := store.gcDomains.first; domain != nil; domain = domain.next {
		if domain.conversionClaims != 0 {
			set.add(domain)
		}
	}
}
func closeGCConversionCoreLocked(s *gcExternConversionState) {
	if s.core == nil {
		return
	}
	store := s.store
	store.mu.Lock()
	if s.core.domain.conversionPublishers == 0 {
		panic("conversion publisher authority underflow")
	}
	s.core.domain.conversionPublishers--
	for _, entry := range s.entries {
		identity := store.gcDomains.conversions[entry.externWord]
		if identity == nil || identity.publisher != s {
			continue
		}
		identity.publisher = nil
		if v, ok := managedFunctionCollectors.Load(identity.domain.collector); ok {
			delete(v.(*managedFunctionCollector).conversionFloating, identity.floating)
		}
		if len(identity.receipts) == 0 {
			delete(store.gcDomains.conversions, identity.word)
		}
	}
	store.mu.Unlock()
	s.core = nil
}
func publishGCConversionTable(s *gcRefTestTableState, table, index, old, word uint64, publish func() error) error {
	if s.Conversion.core == nil && s.conversionContainers == nil {
		return s.Conversion.publishExternReplacement(old, word, publish)
	}
	if s.conversionContainers == nil {
		s.conversionContainers = make([]*gcConversionContainer, s.TableCount)
	}
	container := s.conversionContainers[table]
	if container == nil {
		var err error
		container, err = newGCConversionContainer(s.Conversion.core.owner, s.Descriptors[table])
		if err != nil {
			return err
		}
		s.conversionContainers[table] = container
	}
	tx, err := container.prepare(index, word, s.Conversion)
	if err != nil {
		return err
	}
	return tx.commit(publish)
}
func releaseGCConversionTables(s *gcRefTestTableState) {
	for _, c := range s.conversionContainers {
		c.release()
	}
	s.conversionContainers = nil
}
func clearUnreachableConversionRoots(d *managedFunctionMarkDomain) error {
	// Receipt roots are conditional on the physical owner. Release dead receipts
	// into the post-topology journal before local collection can invalidate refs.
	store := d.state.graph.store
	store.mu.Lock()
	var identities []*gcConversionIdentity
	for _, identity := range store.gcDomains.conversions {
		if identity.domain.collector == d.state.collector {
			identities = append(identities, identity)
		}
	}
	store.mu.Unlock()
	for _, identity := range identities {
		for receipt := range identity.receipts {
			member := d.state.tableOwners[receipt.root]
			if !(receipt.container.owner == nil && conversionHostRootIsStrong(receipt.container)) && !d.mark.live[member] {
				receipt.release()
			}
		}
		ref := identity.ref
		index := uint32(ref) >> 1
		word, bit := int(index>>6), uint64(1)<<(index&63)
		if !ref.IsObj() && len(identity.receipts) != 0 {
			continue
		}
		if ref.IsObj() && word < len(d.seen) && d.seen[word]&bit != 0 {
			continue
		}
		if s := identity.publisher; s != nil {
			s.mu.Lock()
			for i := range s.entries {
				entry := &s.entries[i]
				if entry.externWord == identity.word {
					if entry.hasRoot {
						if err := d.state.collector.SetTableSlot(entry.rootSlot, gc.Null()); err != nil {
							s.mu.Unlock()
							return err
						}
					}
					*entry = gcExternConversionEntry{}
					s.count--
					break
				}
			}
			s.mu.Unlock()
			identity.publisher = nil
			delete(d.state.conversionFloating, identity.floating)
		}
		if len(identity.receipts) == 0 {
			store.mu.Lock()
			delete(store.gcDomains.conversions, identity.word)
			store.mu.Unlock()
		}
	}
	return nil
}

func gcConversionCollectorRegistered(s *gcExternConversionState) bool {
	if s == nil || s.core == nil {
		return true
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	return s.store.gcDomains.domainForCollectorLocked(s.collector) == s.core.domain
}
func releaseGCConversionOwner(owner *Instance) {
	if owner == nil || owner.refStore == nil {
		return
	}
	store := owner.refStore
	store.ensureGCTopology()
	store.mu.Lock()
	if store.gcDomains == nil {
		store.mu.Unlock()
		return
	}
	var containers []*gcConversionContainer
	for _, c := range store.gcDomains.conversionContainers {
		if c.owner == owner {
			containers = append(containers, c)
		}
	}
	store.mu.Unlock()
	for _, c := range containers {
		c.release()
	}
}

// Physical teardown has no live invocation. Freeze all receipt-domain readers
// while clearing roots; release before native resources or callbacks are retired.
func lockGCConversionRetirement(in *Instance) func() {
	if in == nil || in.refStore == nil {
		return func() {}
	}
	in.refStore.mu.Lock()
	needed := in.refStore.gcDomains != nil && (len(in.refStore.gcDomains.conversionContainers) != 0 || in.existingGCExternConversionState() != nil)
	in.refStore.mu.Unlock()
	if !needed {
		return func() {}
	}
	topology := in.refStore.ensureGCTopology()
	topology.Lock()
	return topology.Unlock
}

// Store unlink owns terminal retirement; collector callbacks run after unlock.
// Invalidate receipts as metadata before relinquishing registered heap authority.
func retireGCConversionStoreLocked(store *referenceStore) {
	if store.gcDomains == nil {
		return
	}
	for _, identity := range store.gcDomains.conversions {
		for receipt := range identity.receipts {
			if state, ok := managedFunctionCollectors.Load(identity.domain.collector); ok {
				delete(state.(*managedFunctionCollector).conversionHostSlots, receipt.root)
			}
			receipt.released = true
			if receipt.container.slots[receipt.index] == receipt {
				delete(receipt.container.slots, receipt.index)
			}
		}
		clear(identity.receipts)
		identity.publisher = nil
	}
	for domain := store.gcDomains.first; domain != nil; domain = domain.next {
		domain.conversionClaims = 0
	}
	store.gcDomains.conversions = nil
	for _, c := range store.gcDomains.conversionContainers {
		c.closed = true
		c.owner = nil
		c.storage = nil
		clear(c.slots)
	}
	store.gcDomains.conversionContainers = nil
}

// migrate transfers the directory key of one physical allocation while retaining
// every receipt object/index. The caller owns full publication authority and
// updates every native/exported descriptor alias in publish, which must leave
// them unchanged on error. Host table.grow uses reserved capacity and needs no
// relocation; this adapter is for an allocation replacement.
func (c *gcConversionContainer) migrate(storage []byte, publish func() error) error {
	if c == nil || c.closed || c.cell || len(storage) < 8 {
		return fmt.Errorf("invalid conversion table migration")
	}
	old := c.storage
	n, capacity := binary.LittleEndian.Uint32(storage), binary.LittleEndian.Uint32(storage[4:])
	if n != binary.LittleEndian.Uint32(old) || n > capacity || uint64(capacity)*8+8 > uint64(len(storage)) {
		return fmt.Errorf("conversion migration shape mismatch")
	}
	for i := uint64(0); i < uint64(n); i++ {
		if binary.LittleEndian.Uint64(old[8+i*8:]) != binary.LittleEndian.Uint64(storage[8+i*8:]) {
			return fmt.Errorf("conversion migration changed a slot")
		}
	}
	if _, err := conversionContainerForStore(c.store, c.owner, old); err != nil {
		return err
	}
	c.store.mu.Lock()
	conflict := c.store.gcDomains.conversionContainers[&storage[0]]
	c.store.mu.Unlock()
	if conflict != nil && conflict != c {
		return fmt.Errorf("conversion migration aliases another owner")
	}
	if err := publish(); err != nil {
		return err
	}
	c.store.mu.Lock()
	delete(c.store.gcDomains.conversionContainers, &old[0])
	c.store.gcDomains.conversionContainers[&storage[0]] = c
	c.storage = storage
	c.store.mu.Unlock()
	return nil
}
