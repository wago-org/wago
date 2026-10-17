//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"sync"
	"sync/atomic"
	"unsafe"
)

// A host resource owns the collector claim directly. It must not retain an
// importer, which can itself retain the host resource until physical close.
type compactExternHostSlot struct {
	owner *compactExternHostRootRow
	cell  uintptr
}

type compactExternHostSlotPool struct {
	free       []uint32
	publicFree []uint32
	owners     map[uint32]compactExternHostSlot
}

type compactExternHostRootRow struct {
	logicalClosed atomic.Bool
	store         *referenceStore
	domain        *gcStoreDomain
	desc          []byte
	slots         map[uintptr]uint32
}

type compactExternHostTable struct {
	compactExternHostRootRow
	table *Table
}

var compactExternHostTableCount atomic.Uint32

var compactExternHostTables struct {
	sync.RWMutex
	rows map[*tableOwner]*compactExternHostTable
}

// store.mu freezes the counted resource set. Registry operations never hold a
// registry lock while acquiring store.mu. A row disappearing during physical
// detach makes this check conservative until storeObjectClosed retries it.
func railshotClosedCompactExternHostObjectsLocked(store *referenceStore, count uint32) bool {
	if count == 0 || compactExternHostTableCount.Load() == 0 {
		return false
	}
	var closed uint32
	compactExternHostTables.RLock()
	for _, row := range compactExternHostTables.rows {
		if row.store == store && row.logicalClosed.Load() {
			closed++
		}
	}
	compactExternHostTables.RUnlock()
	compactExternHostGlobals.RLock()
	for _, row := range compactExternHostGlobals.rows {
		if row.store == store && row.logicalClosed.Load() {
			closed++
		}
	}
	compactExternHostGlobals.RUnlock()
	return closed == count
}

// unpinExternref holds store.mu and can run inside native cleanup. Wake the
// existing outer cleanup driver rather than releasing collector roots here.
func railshotCompactExternUnpinnedAfterClose(store *referenceStore) {
	if value, ok := managedFunctionGraphs.Load(store); ok {
		value.(*managedFunctionGraph).pendingMetadata.Store(true)
	}
}

func compactExternHostTableRow(owner *tableOwner) *compactExternHostTable {
	compactExternHostTables.RLock()
	row := compactExternHostTables.rows[owner]
	compactExternHostTables.RUnlock()
	return row
}
func railshotAttachCompactExternHostTable(t *Table, store *referenceStore, collector *gc.Collector) error {
	if t == nil || t.owner == nil {
		return nil
	}
	if row := compactExternHostTableRow(t.owner); row != nil && row.logicalClosed.Load() {
		return fmt.Errorf("host extern table is logically closed")
	}
	if collector == nil {
		return nil
	}
	owner := t.owner
	owner.mu.Lock()
	host := owner.instance == nil && owner.elementType == ValExternRef
	owner.mu.Unlock()
	if !host {
		return nil
	}
	if row := compactExternHostTableRow(owner); row != nil {
		if row.store != store || row.domain.collector != collector {
			return fmt.Errorf("host extern table belongs to another collector domain")
		}
		return nil
	}
	if store == nil {
		return fmt.Errorf("host extern table has no reference store")
	}
	store.mu.Lock()
	domain := store.gcDomains.domainForCollectorLocked(collector)
	if domain == nil || domain.private || domain.claims == ^uint32(0) {
		store.mu.Unlock()
		return fmt.Errorf("host extern table has no live collector domain")
	}
	domain.claims++
	store.mu.Unlock()
	keep := false
	defer func() {
		if !keep {
			store.releaseUnclaimedGCCollector(collector)
		}
	}()
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closed || owner.instance != nil || owner.store != store || owner.elementType != ValExternRef {
		return fmt.Errorf("host extern table owner changed before domain acquisition")
	}
	t.mu.Lock()
	desc := t.desc
	valid := !t.closed && len(desc) >= 8
	t.mu.Unlock()
	if !valid {
		return fmt.Errorf("host extern table has no native storage")
	}
	compactExternHostTables.Lock()
	defer compactExternHostTables.Unlock()
	if row := compactExternHostTables.rows[owner]; row != nil {
		if row.store != store || row.domain.collector != collector {
			return fmt.Errorf("host extern table belongs to another collector domain")
		}
		return nil
	}
	if compactExternHostTables.rows == nil {
		compactExternHostTables.rows = make(map[*tableOwner]*compactExternHostTable)
	}
	compactExternHostTables.rows[owner] = &compactExternHostTable{table: t, compactExternHostRootRow: compactExternHostRootRow{store: store, domain: domain, desc: desc}}
	compactExternHostTableCount.Add(1)
	keep = true
	return nil
}
func railshotCompactExternHostTableCollector(t *Table, store *referenceStore) (*gc.Collector, error) {
	if t == nil || t.owner == nil {
		return nil, nil
	}
	row := compactExternHostTableRow(t.owner)
	if row == nil {
		return nil, nil
	}
	if row.store != store {
		return nil, fmt.Errorf("host extern table requires the same reference store")
	}
	return row.domain.collector, nil
}

// Called while tableOwner.mu protects the published physical descriptor.
func railshotCompactExternHostTableMatches(t *Table, store *referenceStore, collector *gc.Collector) bool {
	row := compactExternHostTableRow(t.owner)
	return row != nil && row.store == store && row.domain.collector == collector && len(t.desc) == len(row.desc) && len(t.desc) >= 8 && &t.desc[0] == &row.desc[0]
}
func railshotReleaseCompactExternHostTable(owner *tableOwner) {
	compactExternHostTables.Lock()
	row := compactExternHostTables.rows[owner]
	delete(compactExternHostTables.rows, owner)
	if len(compactExternHostTables.rows) == 0 {
		compactExternHostTables.rows = nil
	}
	compactExternHostTables.Unlock()
	if row == nil {
		return
	}
	compactExternHostTableCount.Add(^uint32(0))
	releaseCompactExternHostRoots(&row.compactExternHostRootRow)
}

func releaseCompactExternHostRoots(row *compactExternHostRootRow) {
	defer row.store.releaseUnclaimedGCCollector(row.domain.collector)
	row.domain.mu.Lock()
	defer row.domain.mu.Unlock()
	state, _ := compactExternCollectors.Load(row.domain.collector)
	for _, slot := range row.slots {
		if err := row.domain.collector.SetGlobalSlot(slot, gc.Null()); err != nil {
			panic(gcHelperFailure(err))
		}
		if state != nil && state.hostSlots != nil {
			delete(state.hostSlots.owners, slot)
			state.hostSlots.free = append(state.hostSlots.free, slot)
		}
	}
	row.slots = nil
	row.desc = nil
}
func (s *compactExternCollector) storePhysicalPrepared(owner *Instance, host *tableOwner, key uintptr, value *compactExternPreparedWord) bool {
	if owner != nil {
		return s.storePrepared(owner, key, value)
	}
	row := compactExternHostTableRow(host)
	if row == nil || row.store != s.store || row.domain.collector != s.collector {
		panic(gcHelperFailuref("extern table has no exact physical root owner"))
	}
	return s.storeHostPrepared(&row.compactExternHostRootRow, key, value, 8)
}

func (s *compactExternCollector) storeHostPrepared(row *compactExternHostRootRow, key uintptr, value *compactExternPreparedWord, first uintptr) bool {
	if row == nil || row.store != s.store || row.domain == nil || row.domain.collector != s.collector {
		panic(gcHelperFailuref("extern host cell has no exact collector owner"))
	}
	if len(row.desc) < 8 {
		panic(gcHelperFailuref("extern host table storage was released"))
	}
	base := uintptr(unsafe.Pointer(&row.desc[0]))
	if key < base+first || key-base >= uintptr(len(row.desc)) || (key-base)&7 != 0 {
		panic(gcHelperFailuref("extern host table cell is outside storage"))
	}
	slot, exists := row.slots[key]
	if !exists {
		if value.word == 0 {
			return false
		}
		if s.hostSlots == nil {
			s.hostSlots = &compactExternHostSlotPool{}
		}
		free := s.hostSlots.free
		if len(free) != 0 {
			slot = free[len(free)-1]
			s.hostSlots.free = free[:len(free)-1]
			if err := s.collector.SetGlobalSlot(slot, value.ref); err != nil {
				panic(gcHelperFailure(err))
			}
		} else {
			var err error
			slot, err = s.collector.NewCheckedClassifiedGlobalSlot(value.ref, gc.RootForeignInstance)
			if err != nil {
				panic(gcHelperFailure(err))
			}
		}
		if row.slots == nil {
			row.slots = make(map[uintptr]uint32)
		}
		row.slots[key] = slot
		if s.hostSlots.owners == nil {
			s.hostSlots.owners = make(map[uint32]compactExternHostSlot)
		}
		s.hostSlots.owners[slot] = compactExternHostSlot{owner: row, cell: key}
	} else if err := s.collector.SetGlobalSlot(slot, value.ref); err != nil {
		panic(gcHelperFailure(err))
	}
	if value.pin != 0 {
		s.retirePersistentPin(value.pin)
		value.pin = 0
	}
	return true
}
func (s *compactExternCollector) hostTableForCell(key uintptr) *tableOwner {
	compactExternHostTables.RLock()
	defer compactExternHostTables.RUnlock()
	for owner, row := range compactExternHostTables.rows {
		if row.store == s.store && row.domain.collector == s.collector && len(row.desc) >= 8 {
			base := uintptr(unsafe.Pointer(&row.desc[0]))
			if key >= base+8 && key-base < uintptr(len(row.desc)) && (key-base)&7 == 0 {
				return owner
			}
		}
	}
	return nil
}

func compactExternAddClaimedDomainsLocked(store *referenceStore, set *gcInvocationDomainSet) {
	if compactExternHostTableCount.Load() == 0 || store.gcDomains == nil {
		return
	}
	for domain := store.gcDomains.first; domain != nil; domain = domain.next {
		set.add(domain)
	}
}
func compactExternHostRootIsStrong(collector *gc.Collector, index uint32) bool {
	if compactExternHostTableCount.Load() == 0 {
		return true
	}
	state, ok := compactExternCollectors.Load(collector)
	if !ok || state.hostSlots == nil {
		return true
	}
	row := state.hostSlots.owners[index].owner
	return row == nil || !row.logicalClosed.Load()
}
func railshotFinishCompactExternHostTableDetach(table *Table) {
	if table == nil || table.owner == nil {
		return
	}
	row := compactExternHostTableRow(table.owner)
	if row == nil || !row.logicalClosed.Load() {
		return
	}
	table.owner.mu.Lock()
	ready := table.owner.importers == 0 && table.owner.nativeLeases == 0 && !table.owner.closed
	table.owner.mu.Unlock()
	if ready {
		if err := row.table.Close(); err != nil {
			panic(err)
		}
	}
}

// Exact attachment rows, not the caller's imports map, account for every host
// counter. Unregistered construction, live importers, and unproved collector
// domains keep the ordinary Close rejection.
func compactExternClosedHostTableImporters(table *Table, row *compactExternHostTable, publish bool) ([]*Instance, bool) {
	row.store.mu.Lock()
	owners := make([]*Instance, 0, len(row.store.instances))
	for owner, entry := range row.store.instances {
		if entry != nil && !entry.resourcesReleased {
			owners = append(owners, owner)
		}
	}
	row.store.mu.Unlock()
	table.owner.mu.Lock()
	defer table.owner.mu.Unlock()
	if table.owner.closed || table.owner.nativeLeases != 0 || table.owner.importers <= 0 {
		return nil, false
	}
	count := 0
	result := owners[:0]
	for _, owner := range owners {
		rows := rv64ExistingTableDependencyRows(owner)
		if rows == nil {
			continue
		}
		matches := 0
		rows.mu.Lock()
		rows.rows.each(func(entry *rv64TableDependencyRow) {
			if entry.state == rv64TableDependencyAttached && !entry.counterCommitted && entry.snapshot.owner == table.owner && entry.store == row.store && entry.root.producer == nil {
				matches++
			}
		})
		rows.mu.Unlock()
		if matches == 0 {
			continue
		}
		owner.lifeMu.Lock()
		closed := owner.closed && !owner.resourcesClosed
		owner.lifeMu.Unlock()
		if !closed || owner.gc != row.domain.collector || owner.c == nil || !owner.c.compactExternFrameRoots() {
			return nil, false
		}
		count += matches
		result = append(result, owner)
	}
	valid := count == table.owner.importers && len(result) != 0
	if valid && publish {
		table.mu.Lock()
		table.closed = true
		table.mu.Unlock()
		row.logicalClosed.Store(true)
	}
	return result, valid
}
func railshotTryCloseCompactExternHostTable(table *Table) (bool, error) {
	if table == nil || table.owner == nil {
		return false, nil
	}
	row := compactExternHostTableRow(table.owner)
	if row == nil {
		return false, nil
	}
	table.owner.mu.Lock()
	closed := table.owner.closed
	importers, leases := table.owner.importers, table.owner.nativeLeases
	table.owner.mu.Unlock()
	if closed || importers == 0 {
		return false, nil
	}
	if row.logicalClosed.Load() {
		return true, nil
	}
	if leases != 0 {
		return false, nil
	}
	owners, ok := compactExternClosedHostTableImporters(table, row, false)
	if !ok {
		return false, nil
	}
	requester := owners[0]
	var cleanup resourceCleanup
	defer func() {
		if value := recover(); value != nil {
			cleanup.panicked = true
			cleanup.value = value
		}
		cleanup.run(func() { drainManagedFunctionStoreReclaims(row.store) })
		cleanup.finish()
	}()
	lease := requester.lockManagedFunctionCollectionInvocation(newInvocationID())
	defer func() { cleanup.run(lease.unlock) }()
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	owners, ok = compactExternClosedHostTableImporters(table, row, true)
	if !ok {
		return false, nil
	}
	// All domains and native execution are frozen before the external host root
	// changes. Existing physical attachments remain valid through owner.closed.
	var state *managedFunctionCollector
	func() {
		row.domain.mu.Lock()
		defer row.domain.mu.Unlock()
		state = requester.managedFunctionCollector(row.domain)
	}()
	state.graph.epoch.Add(1)
	_, err := collectManagedFunctionGraph(requester, requester.publicGCState(), true)
	return true, err
}

// After the owner claims commit, no dead importer can resurrect these cells.
// Clear their roots and native words before a local pass can see stale refs.
func (d *managedFunctionMarkDomain) clearUnreachableHostExternRoots() error {
	state, ok := compactExternCollectors.Load(d.state.collector)
	if !ok || state.hostSlots == nil {
		return nil
	}
	for slot, cell := range state.hostSlots.owners {
		row := cell.owner
		if !row.logicalClosed.Load() {
			continue
		}
		ref, err := d.state.collector.CheckedGlobalSlot(slot)
		if err != nil {
			return err
		}
		if ref.IsNull() {
			continue
		}
		index := uint32(ref) >> 1
		word, bit := index>>6, uint64(1)<<(index&63)
		if int(word) < len(d.seen) && d.seen[word]&bit != 0 {
			continue
		}
		if err := d.state.collector.SetGlobalSlot(slot, gc.Null()); err != nil {
			return err
		}
		binary.LittleEndian.PutUint64(unsafe.Slice((*byte)(offHeapPtr(cell.cell)), 8), 0)
	}
	return nil
}

// The caller holds tableOwner.mu through its importer-count commit. This
// closes the interval after the early validation and before acquisition.
func railshotCompactExternHostTableOpen(owner *tableOwner) bool {
	if compactExternHostTableCount.Load() == 0 {
		return true
	}
	row := compactExternHostTableRow(owner)
	return row == nil || !row.logicalClosed.Load()
}
