//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"sync"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type compactExternHostGlobal struct {
	compactExternHostRootRow
	global *Global
}

var compactExternHostGlobals struct {
	sync.RWMutex
	rows map[*globalOwner]*compactExternHostGlobal
}

func compactExternHostGlobalRow(owner *globalOwner) *compactExternHostGlobal {
	if owner == nil || compactExternHostTableCount.Load() == 0 {
		return nil
	}
	compactExternHostGlobals.RLock()
	row := compactExternHostGlobals.rows[owner]
	compactExternHostGlobals.RUnlock()
	return row
}

func railshotAttachCompactExternHostGlobal(global *Global, store *referenceStore, collector *gc.Collector) error {
	if global == nil || global.owner == nil {
		return nil
	}
	owner := global.owner
	if row := compactExternHostGlobalRow(owner); row != nil && row.logicalClosed.Load() {
		return fmt.Errorf("host extern global is logically closed")
	}
	if collector == nil {
		return nil
	}
	owner.mu.Lock()
	host := owner.instance == nil && owner.typ == ValExternRef
	compatibleStore := owner.store == store
	owner.mu.Unlock()
	if host && !compatibleStore {
		return fmt.Errorf("host extern global has incompatible reference store")
	}
	if !host {
		return nil
	}
	if row := compactExternHostGlobalRow(owner); row != nil {
		if row.store != store || row.domain.collector != collector {
			return fmt.Errorf("host extern global belongs to another collector domain")
		}
		return nil
	}
	if store == nil {
		return fmt.Errorf("host extern global has no reference store")
	}
	store.mu.Lock()
	domain := store.gcDomains.domainForCollectorLocked(collector)
	if domain == nil || domain.private || domain.claims == ^uint32(0) {
		store.mu.Unlock()
		return fmt.Errorf("host extern global has no live collector domain")
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
	if owner.closed || owner.instance != nil || owner.typ != ValExternRef || owner.store != store || len(global.cell) != 8 {
		return fmt.Errorf("host extern global physical owner changed")
	}
	compactExternHostGlobals.Lock()
	defer compactExternHostGlobals.Unlock()
	if row := compactExternHostGlobals.rows[owner]; row != nil {
		if row.logicalClosed.Load() || row.store != store || row.domain.collector != collector {
			return fmt.Errorf("host extern global domain changed")
		}
		return nil
	}
	if compactExternHostGlobals.rows == nil {
		compactExternHostGlobals.rows = make(map[*globalOwner]*compactExternHostGlobal)
	}
	compactExternHostGlobals.rows[owner] = &compactExternHostGlobal{global: global, compactExternHostRootRow: compactExternHostRootRow{store: store, domain: domain, desc: global.cell}}
	compactExternHostTableCount.Add(1)
	keep = true
	return nil
}

func railshotCompactExternHostGlobalCollector(global *Global, store *referenceStore) (*gc.Collector, error) {
	if global == nil || global.owner == nil {
		return nil, nil
	}
	row := compactExternHostGlobalRow(global.owner)
	if row == nil {
		return nil, nil
	}
	if row.store != store {
		return nil, fmt.Errorf("host extern global requires the same reference store")
	}
	return row.domain.collector, nil
}

// globalOwner.mu protects the exact native cell span.
func railshotCompactExternHostGlobalMatches(global *Global, store *referenceStore, collector *gc.Collector) bool {
	row := compactExternHostGlobalRow(global.owner)
	return row != nil && row.store == store && row.domain.collector == collector && len(global.cell) == 8 && len(row.desc) == 8 && &global.cell[0] == &row.desc[0]
}

func railshotCompactExternHostGlobalOpen(owner *globalOwner) bool {
	row := compactExternHostGlobalRow(owner)
	return row == nil || !row.logicalClosed.Load()
}

func railshotReleaseCompactExternHostGlobal(owner *globalOwner) {
	compactExternHostGlobals.Lock()
	row := compactExternHostGlobals.rows[owner]
	delete(compactExternHostGlobals.rows, owner)
	if len(compactExternHostGlobals.rows) == 0 {
		compactExternHostGlobals.rows = nil
	}
	compactExternHostGlobals.Unlock()
	if row == nil {
		return
	}
	compactExternHostTableCount.Add(^uint32(0))
	releaseCompactExternHostRoots(&row.compactExternHostRootRow)
}

func (s *compactExternCollector) hostGlobalForCell(key uintptr) *compactExternHostGlobal {
	compactExternHostGlobals.RLock()
	defer compactExternHostGlobals.RUnlock()
	for _, row := range compactExternHostGlobals.rows {
		if row.store == s.store && row.domain.collector == s.collector && len(row.desc) == 8 && uintptr(unsafe.Pointer(&row.desc[0])) == key {
			return row
		}
	}
	return nil
}

func railshotValidCompactExternHostGlobalWord(global *Global, store *referenceStore, collector *gc.Collector, word uint64) bool {
	if global == nil || global.owner == nil || collector == nil {
		return false
	}
	global.owner.mu.Lock()
	matches := railshotCompactExternHostGlobalOpen(global.owner) && railshotCompactExternHostGlobalMatches(global, store, collector)
	global.owner.mu.Unlock()
	if !matches {
		return false
	}
	actual, ref := compactExternWordRoot(store, word)
	return actual == collector && !ref.IsNull()
}

// The public accessor holds native state. The source instance is a temporary
// physical lease only; finishGlobalValueAccess releases it after native unlock.
func compactExternHostAccessRoots(row *compactExternHostGlobal, leases *globalValueRoots) (*Instance, gc.RootSet) {
	row.store.mu.Lock()
	var source *Instance
	for owner, entry := range row.store.instances {
		if entry != nil && !entry.resourcesReleased && owner.gc == row.domain.collector && owner.retainResourceRootForFinalization() {
			source = owner
			break
		}
	}
	row.store.mu.Unlock()
	if source == nil {
		return nil, gc.EmptyRoots{}
	}
	leases.record(source)
	public := source.publicGCState()
	public.mu.Lock()
	public.frameRoots = source.gcCollectFrameRoots(public)
	return source, &public.frameRoots
}

func railshotSetCompactExternHostGlobal(global *Global, value Value, leases *globalValueRoots) (bool, error) {
	if global == nil || global.owner == nil {
		return false, nil
	}
	row := compactExternHostGlobalRow(global.owner)
	if row == nil {
		return false, nil
	}
	if value.typ != ValExternRef {
		return true, fmt.Errorf("host extern global requires externref value")
	}
	row.domain.mu.Lock()
	defer row.domain.mu.Unlock()
	global.owner.mu.Lock()
	valid := !global.owner.closed && !row.logicalClosed.Load() && global.owner.mutable && global.Mutable && global.Type == ValExternRef && railshotCompactExternHostGlobalMatches(global, row.store, row.domain.collector)
	global.owner.mu.Unlock()
	if !valid {
		return true, fmt.Errorf("host extern global owner is closed or changed")
	}
	state, err := compactExternCollectorFor(row.domain.collector, row.store, row.domain)
	if err != nil {
		return true, err
	}
	source, roots := compactExternHostAccessRoots(row, leases)
	if source != nil {
		defer source.publicGCState().mu.Unlock()
	}
	prepared, err := state.preparePersistent(value.bits, roots)
	if err != nil {
		return true, err
	}
	global.owner.mu.Lock()
	defer global.owner.mu.Unlock()
	if global.owner.closed || row.logicalClosed.Load() || !railshotCompactExternHostGlobalMatches(global, row.store, row.domain.collector) {
		return true, fmt.Errorf("host extern global changed before store")
	}
	key := uintptr(unsafe.Pointer(&global.cell[0]))
	state.storeHostPrepared(&row.compactExternHostRootRow, key, &prepared, 0)
	binary.LittleEndian.PutUint64(global.cell, prepared.word)
	return true, nil
}

// The exact attachment rows account for every host counter. Construction rows,
// live importers, and a changed native cell keep the ordinary Close rejection.
func compactExternClosedHostGlobalImporters(global *Global, row *compactExternHostGlobal, publish bool) ([]*Instance, bool) {
	row.store.mu.Lock()
	owners := make([]*Instance, 0, len(row.store.instances))
	for owner, entry := range row.store.instances {
		if entry != nil && !entry.resourcesReleased {
			owners = append(owners, owner)
		}
	}
	row.store.mu.Unlock()
	// A foreign Runtime can invoke a closed producer without owning this
	// store's public entry gate. Parked host activations remain published on
	// the physical producer while their invocation gates are suspended.
	for _, owner := range owners {
		if owner.gc != row.domain.collector {
			continue
		}
		if public := owner.existingPublicGCState(); public != nil {
			public.mu.Lock()
			active := public.hostActivationCount != 0
			public.mu.Unlock()
			if active {
				return nil, false
			}
		}
	}
	global.owner.mu.Lock()
	defer global.owner.mu.Unlock()
	if global.owner.closed || global.owner.importers <= 0 || !railshotCompactExternHostGlobalMatches(global, row.store, row.domain.collector) {
		return nil, false
	}
	count := 0
	result := owners[:0]
	for _, owner := range owners {
		rows := rv64ExistingGlobalDependencyRows(owner)
		if rows == nil {
			continue
		}
		matches := 0
		rows.mu.Lock()
		rows.rows.each(func(entry *rv64GlobalDependencyRow) {
			if entry.state == rv64GlobalDependencyAttached && !entry.counterCommitted && entry.global == global && entry.root.producer == nil {
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
		if !closed || owner.invocationState.Load()&instanceInvocationCount != 0 || owner.gc != row.domain.collector || owner.c == nil || !owner.c.compactExternFrameRoots() {
			return nil, false
		}
		count += matches
		result = append(result, owner)
	}
	valid := count == global.owner.importers && len(result) != 0
	if valid && publish {
		row.logicalClosed.Store(true)
	}
	return result, valid
}

// Close can run from a parked host callback. It must never wait for that
// callback's own domain gate. Acquire an ordered, frozen set without waiting;
// an active invocation leaves the ordinary live-importer error unchanged.
func compactExternTryCloseInvocation(store *referenceStore) (gcInvocationLease, bool) {
	if value, ok := managedFunctionGraphs.Load(store); ok {
		graph := value.(*managedFunctionGraph)
		graph.mu.Lock()
		active := graph.gateStillOwnedLocked(0)
		graph.mu.Unlock()
		if active {
			return gcInvocationLease{}, false
		}
	}
	topology := store.ensureGCTopology()
	if !topology.TryRLock() {
		return gcInvocationLease{}, false
	}
	store.mu.Lock()
	var set gcInvocationDomainSet
	for _, entry := range store.instances {
		if entry != nil && !entry.resourcesReleased {
			set.add(entry.gcDomain)
		}
	}
	compactExternAddClaimedDomainsLocked(store, &set)
	store.mu.Unlock()
	set.sort()
	domains := gcInvocationDomainView{set: &set}
	for i := 0; i < domains.len(); i++ {
		gate := &domains.at(i).invocationMu
		if !gate.state.CompareAndSwap(0, invocationGateHeld) && !gate.state.CompareAndSwap(invocationGateRevoked, invocationGateRevoked|invocationGateHeld) {
			for j := i - 1; j >= 0; j-- {
				domains.at(j).invocationMu.Unlock()
			}
			topology.RUnlock()
			return gcInvocationLease{}, false
		}
	}
	id := newInvocationID()
	domains.claim(id)
	return gcInvocationLease{topology: topology, domains: domains, owner: id, acquired: true, dynamic: true}, true
}

func railshotTryCloseCompactExternHostGlobal(global *Global) (bool, error) {
	if global == nil || global.owner == nil {
		return false, nil
	}
	row := compactExternHostGlobalRow(global.owner)
	if row == nil {
		return false, nil
	}
	global.owner.mu.Lock()
	closed, importers := global.owner.closed, global.owner.importers
	global.owner.mu.Unlock()
	if closed || importers == 0 {
		return false, nil
	}
	if row.logicalClosed.Load() {
		return true, nil
	}
	owners, ok := compactExternClosedHostGlobalImporters(global, row, false)
	if !ok {
		return false, nil
	}
	lease, ok := compactExternTryCloseInvocation(row.store)
	if !ok {
		return false, nil
	}
	var cleanup resourceCleanup
	defer func() {
		if value := recover(); value != nil {
			cleanup.panicked = true
			cleanup.value = value
		}
		cleanup.run(func() { drainManagedFunctionStoreReclaims(row.store) })
		cleanup.finish()
	}()
	defer func() { cleanup.run(lease.unlock) }()
	if !nativeExecutionMu.TryLock() {
		return false, nil
	}
	defer nativeExecutionMu.Unlock()
	owners, ok = compactExternClosedHostGlobalImporters(global, row, true)
	if !ok {
		return false, nil
	}
	requester := owners[0]
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

func railshotFinishCompactExternHostGlobalDetach(global *Global) {
	if global == nil || global.owner == nil {
		return
	}
	row := compactExternHostGlobalRow(global.owner)
	if row == nil || !row.logicalClosed.Load() {
		return
	}
	global.owner.mu.Lock()
	ready := global.owner.importers == 0 && !global.owner.closed
	global.owner.mu.Unlock()
	if ready {
		if err := row.global.Close(); err != nil {
			panic(err)
		}
	}
}
