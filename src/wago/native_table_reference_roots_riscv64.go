//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// A cross-descriptor copy can move a canonical row in either direction between
// an imported table and an owned exported table. Before releasing the
// invocation's candidate roots, transfer the exact producer acquisition to
// both possible destinations. The existing dependency graph handles cycles
// and owner-close adoption; the final prune also releases roots overwritten by
// null or by another producer.
func (s *rv64NativeTableTargets) publishCrossCopyReferenceRows() {
	if s.count < 2 {
		return
	}
	var cleanup resourceCleanup
	for _, storageOwner := range s.owners[:s.count] {
		for index := 0; index < storageOwner.c.tableCount(); index++ {
			if _, imported := storageOwner.c.tableImportAt(index); imported || storageOwner.c.tableElementType(index) != ValFuncRef {
				continue
			}
			destination := s.ownedTablePublicationDestination(storageOwner, index)
			if destination == nil {
				continue
			}
			for _, producer := range s.owners[:s.count] {
				destination.mu.Lock()
				state := destination.retained[producer]
				retained := state != nil && state.precise
				destination.mu.Unlock()
				if !retained {
					destination.retainProducerInstanceModeWithCleanup(producer, true, &cleanup)
				}
			}
			cleanup.run(destination.pruneRetainedInstances)
		}
	}
	cleanup.finish()
}

// Every exceptional path releases native serialization before cleanup runs.
func (s *rv64NativeTableTargets) ownedTablePublicationDestination(storageOwner *Instance, index int) *Table {
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	desc := rv64RetainedTableDescriptor(storageOwner, index)
	storageOwner.lifeMu.Lock()
	var destination *Table
	for table := storageOwner.table; table != nil; table = table.next {
		if len(desc) != 0 && len(table.desc) != 0 && &table.desc[0] == &desc[0] {
			destination = table
			break
		}
	}
	storageOwner.lifeMu.Unlock()
	foreign := s.hasPublishedForeignTableRow(storageOwner, desc)
	if destination == nil && foreign {
		destination = railshotEnsureOwnedFuncrefTable(storageOwner, index)
	} else if destination != nil && !foreign {
		destination.mu.Lock()
		needsPrune := false
		for producer, state := range destination.retained {
			// Scope release already removes temporary writer promises. Only
			// persistent foreign rows need this owned-table pruning pass.
			if producer != storageOwner && (state.precise || len(state.proxyDescriptors) != 0) {
				needsPrune = true
				break
			}
		}
		destination.mu.Unlock()
		if !needsPrune {
			return nil
		}
	}
	return destination
}

// Called under native serialization, with every candidate physically retained.
// Unknown row pointers are never dereferenced to discover their alleged owner.
func (s *rv64NativeTableTargets) hasPublishedForeignTableRow(storageOwner *Instance, desc []byte) bool {
	if len(desc) < 8 {
		return false
	}
	size, capacity := uint64(binary.LittleEndian.Uint32(desc)), uint64(binary.LittleEndian.Uint32(desc[4:]))
	if size > capacity || 8+capacity*coreruntime.TableEntryBytes > uint64(len(desc)) {
		return false
	}
	for index := uint64(0); index < size; index++ {
		row := desc[8+index*coreruntime.TableEntryBytes : 8+(index+1)*coreruntime.TableEntryBytes]
		if binary.LittleEndian.Uint64(row) == 0 {
			continue
		}
		descriptor := binary.LittleEndian.Uint64(row[coreruntime.TableEntryRefSlotOffset:])
		if _, own := storageOwner.funcrefDescriptorIndex(descriptor); !own && s.authenticatedRow(row) {
			return true
		}
	}
	return false
}

// Owned export handles are normally lazy. Cross-copy publication cannot depend
// on API lookup order, so materialize and link the exact holder while the
// native scope still pins every candidate descriptor owner. A later
// ExportedTable call finds this same handle under lifeMu.
func railshotEnsureOwnedFuncrefTable(in *Instance, index int) *Table {
	if in == nil || in.c == nil || index < 0 || index >= in.c.tableCount() {
		panic("RV64 cross-copy owned table index")
	}
	if _, imported := in.c.tableImportAt(index); imported || in.c.tableElementType(index) != ValFuncRef {
		panic("RV64 cross-copy owned funcref table type")
	}
	desc := in.tableDescriptor(index)
	if len(desc) < 8 {
		panic("RV64 cross-copy owned table descriptor")
	}
	exact, err := in.c.tableExactType(index)
	if err != nil {
		panic(err)
	}
	in.lifeMu.Lock()
	for table := in.table; table != nil; table = table.next {
		if len(table.desc) != 0 && &table.desc[0] == &desc[0] {
			in.lifeMu.Unlock()
			return table
		}
	}
	def := in.c.tableDef(index)
	owner := &tableOwner{store: in.refStore, instance: in, elementType: ValFuncRef, valueType: exact, types: in.c.Types, hasValueType: true, declaredHasMax: def.HasMax, addr64: def.Addr64}
	table := &Table{desc: desc, owner: owner, next: in.table}
	railshotLinkOwnedTableDescriptorHolderLocked(in, table)
	in.table = table
	in.lifeMu.Unlock()
	in.markNativeControlShared()
	return table
}

// Publish only rows outside the writer's store before removing its suspended
// promise. Same-store rows are reconciled during ordinary instance finalization;
// eager publication here would hide a later cross-store writer proxy.
func (s *rv64NativeTableTargets) publishWriterReferenceRows(writer rv64TableWriter, foreign bool) {
	table := writer.table
	if table == nil || int(writer.owner) >= s.count {
		return
	}
	owner := s.owners[writer.owner]
	if owner == nil || foreign && !s.hasForeignWriterRow(table, owner) || !foreign && !s.hasNonWriterRow(table, owner) {
		return
	}
	var cleanup resourceCleanup
	store := owner.refStore
	var dependencies tableDescriptorDependencyBatch
	if foreign {
		dependencies = s.prepareForeignWriterProxy(table, owner)
		store = nil
	}
	table.retainDescriptorOwnersForFinalizationWithCleanup(store, owner, &cleanup)
	cleanup.run(func() { railshotFlushTableDescriptorDependencies(&dependencies) })
	cleanup.finish()
}

// A copy creates at least one row that no persistent precise or proxy root
// covered on entry. Same-store rows can resolve their producer directly. A
// foreign row or a global proxy requires the writer across its complete table
// set so a later source close cannot break the proxy chain. Read-only callers
// see only rows that their containers already cover.
func (s *rv64NativeTableTargets) writerPublicationMode() (publish, foreign bool) {
	for _, writer := range s.writers {
		table := writer.table
		if table == nil || int(writer.owner) >= s.count {
			continue
		}
		owner := s.owners[writer.owner]
		if owner == nil {
			continue
		}
		nativeExecutionMu.Lock()
		table.owner.mu.Lock()
		table.mu.Lock()
		if !table.owner.closed && !table.closed && len(table.desc) >= 8 {
			size, capacity := uint64(binary.LittleEndian.Uint32(table.desc)), uint64(binary.LittleEndian.Uint32(table.desc[4:]))
			if size <= capacity && 8+capacity*coreruntime.TableEntryBytes <= uint64(len(table.desc)) {
				for index := uint64(0); index < size; index++ {
					row := table.desc[8+index*coreruntime.TableEntryBytes : 8+(index+1)*coreruntime.TableEntryBytes]
					if binary.LittleEndian.Uint64(row) == 0 {
						continue
					}
					descriptor := binary.LittleEndian.Uint64(row[coreruntime.TableEntryRefSlotOffset:])
					var candidateOwner *Instance
					for _, candidate := range s.owners[:s.count] {
						canonical, valid := rv64PublishedDescriptor(candidate, descriptor)
						if valid && bytes.Equal(row, canonical[:coreruntime.TableEntryBytes]) {
							candidateOwner = candidate
							break
						}
					}
					if candidateOwner == owner {
						continue
					}
					covered := false
					for root, state := range table.retained {
						if state == nil {
							continue
						}
						if _, ok := state.proxyDescriptors[descriptor]; ok || state.precise && root.reachesFuncrefDescriptor(descriptor) {
							covered = true
							break
						}
					}
					if !covered {
						publish = true
						foreign = foreign || candidateOwner == nil || owner.refStore == nil || candidateOwner.refStore != owner.refStore
					}
				}
			}
		}
		table.mu.Unlock()
		table.owner.mu.Unlock()
		nativeExecutionMu.Unlock()
	}
	for _, writer := range s.writers {
		if int(writer.owner) >= s.count {
			continue
		}
		owner := s.owners[writer.owner]
		if owner == nil {
			continue
		}
		for index := range owner.c.GlobalImports {
			if index >= len(owner.globalCells) {
				break
			}
			global := owner.globalCells[index]
			if global == nil || global.owner == nil {
				continue
			}
			global.owner.mu.Lock()
			state := global.owner.retained[owner]
			proxied := state != nil && len(state.proxyDescriptors) != 0
			global.owner.mu.Unlock()
			if proxied {
				return true, true
			}
		}
	}
	return publish, foreign
}

func (s *rv64NativeTableTargets) hasForeignWriterRow(table *Table, writer *Instance) bool {
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	table.owner.mu.Lock()
	defer table.owner.mu.Unlock()
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.owner.closed || table.closed || len(table.desc) < 8 {
		return false
	}
	size, capacity := uint64(binary.LittleEndian.Uint32(table.desc)), uint64(binary.LittleEndian.Uint32(table.desc[4:]))
	if size > capacity || 8+capacity*coreruntime.TableEntryBytes > uint64(len(table.desc)) {
		return false
	}
	for index := uint64(0); index < size; index++ {
		row := table.desc[8+index*coreruntime.TableEntryBytes : 8+(index+1)*coreruntime.TableEntryBytes]
		if binary.LittleEndian.Uint64(row) == 0 {
			continue
		}
		descriptor := binary.LittleEndian.Uint64(row[coreruntime.TableEntryRefSlotOffset:])
		resolved := false
		for _, candidate := range s.owners[:s.count] {
			canonical, valid := rv64PublishedDescriptor(candidate, descriptor)
			if !valid || !bytes.Equal(row, canonical[:coreruntime.TableEntryBytes]) {
				continue
			}
			resolved = true
			if candidate != writer && (writer.refStore == nil || candidate.refStore != writer.refStore) {
				return true
			}
		}
		if !resolved {
			return true
		}
	}
	return false
}

func (s *rv64NativeTableTargets) hasNonWriterRow(table *Table, writer *Instance) bool {
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	table.owner.mu.Lock()
	defer table.owner.mu.Unlock()
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.owner.closed || table.closed || len(table.desc) < 8 {
		return false
	}
	size, capacity := uint64(binary.LittleEndian.Uint32(table.desc)), uint64(binary.LittleEndian.Uint32(table.desc[4:]))
	if size > capacity || 8+capacity*coreruntime.TableEntryBytes > uint64(len(table.desc)) {
		return false
	}
	for index := uint64(0); index < size; index++ {
		row := table.desc[8+index*coreruntime.TableEntryBytes : 8+(index+1)*coreruntime.TableEntryBytes]
		if binary.LittleEndian.Uint64(row) == 0 {
			continue
		}
		descriptor := binary.LittleEndian.Uint64(row[coreruntime.TableEntryRefSlotOffset:])
		if _, own := writer.funcrefDescriptorIndex(descriptor); !own {
			return true
		}
	}
	return false
}

// A prior precise root in a source table cannot authenticate a copied row in a
// foreign writer's store. Replace it with the writer's unresolved proxy only
// after the scope has proved that a foreign copy occurred.
func (s *rv64NativeTableTargets) prepareForeignWriterProxy(table *Table, writer *Instance) tableDescriptorDependencyBatch {
	var dependencies tableDescriptorDependencyBatch
	nativeExecutionMu.Lock()
	table.mu.Lock()
	for root, state := range table.retained {
		if root == writer || state == nil || !state.precise || writer.refStore != nil && root.refStore == writer.refStore {
			continue
		}
		state.precise = false
		if state.activeWriters == 0 && len(state.proxyDescriptors) == 0 {
			delete(table.retained, root)
			railshotRetireTableDescriptorDependencyLocked(table, root, state, &dependencies)
		}
	}
	table.mu.Unlock()
	nativeExecutionMu.Unlock()
	return dependencies
}

func (t *Table) pruneRetainedInstancesRV64() {
	if t == nil {
		return
	}
	railshotLockNativeTableScan()
	var dependencies tableDescriptorDependencyBatch
	t.mu.Lock()
	if !t.closed && len(t.desc) >= 8 && len(t.retained) != 0 {
		current := t.funcrefDescriptorsLocked()
		var proxied map[uint64]struct{}
		for _, state := range t.retained {
			for descriptor := range state.proxyDescriptors {
				if _, live := current[descriptor]; live {
					if proxied == nil {
						proxied = make(map[uint64]struct{})
					}
					proxied[descriptor] = struct{}{}
				}
			}
		}
		for root, state := range t.retained {
			state.precise = false
			for descriptor := range current {
				if _, covered := proxied[descriptor]; !covered && root.reachesFuncrefDescriptor(descriptor) {
					state.precise = true
					break
				}
			}
			for descriptor := range state.proxyDescriptors {
				if _, live := current[descriptor]; !live {
					delete(state.proxyDescriptors, descriptor)
				}
			}
			if state.activeWriters == 0 && !state.precise && len(state.proxyDescriptors) == 0 {
				delete(t.retained, root)
				railshotRetireTableDescriptorDependencyLocked(t, root, state, &dependencies)
			} else if state.precise || len(state.proxyDescriptors) != 0 {
				railshotPublishTableDescriptorDependencyLocked(t, state, &dependencies)
			}
		}
	}
	t.mu.Unlock()
	railshotUnlockNativeTableScan()
	railshotFlushTableDescriptorDependencies(&dependencies)
}

// publishHostTableReferenceRows publishes exact owners during module
// construction, when frozen attachments already pin every candidate.
func (s *rv64NativeTableTargets) publishHostTableReferenceRows(table *Table) {
	table.owner.mu.Lock()
	defer table.owner.mu.Unlock()
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.owner.instance != nil || table.owner.closed || table.closed || len(table.desc) < 8 {
		return
	}
	size, capacity := uint64(binary.LittleEndian.Uint32(table.desc)), uint64(binary.LittleEndian.Uint32(table.desc[4:]))
	if size > capacity || 8+capacity*coreruntime.TableEntryBytes > uint64(len(table.desc)) {
		return
	}
	for index := uint64(0); index < size; index++ {
		row := table.desc[8+index*coreruntime.TableEntryBytes : 8+(index+1)*coreruntime.TableEntryBytes]
		if binary.LittleEndian.Uint64(row) == 0 {
			continue
		}
		descriptor := binary.LittleEndian.Uint64(row[coreruntime.TableEntryRefSlotOffset:])
		for _, owner := range s.owners[:s.count] {
			canonical, valid := rv64PublishedDescriptor(owner, descriptor)
			if !valid || !bytes.Equal(row, canonical[:coreruntime.TableEntryBytes]) {
				continue
			}
			state := table.retained[owner]
			if state == nil {
				if !owner.retainResourceRootForFinalization() {
					panic("RV64 scoped descriptor owner lost physical storage")
				}
				if table.retained == nil {
					table.retained = make(map[*Instance]*retainedInstanceRoot)
				}
				state = &retainedInstanceRoot{}
				table.retained[owner] = state
				railshotRecordTableDescriptorDependencyLocked(table, owner, state, nil)
			}
			state.precise = true
			break
		}
	}
}
