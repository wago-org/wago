//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// A cross-descriptor copy can move a canonical row in either direction between
// an imported table and an owned exported table. Before releasing the
// invocation's candidate roots, transfer the exact producer acquisition to
// both possible destinations. The existing dependency graph handles cycles
// and owner-close adoption; the final prune also releases roots overwritten by
// null or by another producer.
func (s *rv64NativeTableTargets) publishCrossCopyReferenceRows() {
	if s.count == 0 {
		return
	}
	writer := s.owners[0]
	if writer == nil || !rv64MixedTable64CrossCopy(writer.c) {
		return
	}
	var cleanup resourceCleanup
	var destinations [2]*Table
	if imported, ok := writer.imports.table(writer.c.tableImportBindingKey(0)); ok {
		destinations[0] = imported
	}
	destinations[1] = railshotEnsureOwnedFuncrefTable(writer, 1)
	for _, destination := range destinations {
		if destination == nil {
			continue
		}
		for _, producer := range s.owners[:s.count] {
			destination.retainProducerInstanceModeWithCleanup(producer, true, &cleanup)
		}
		cleanup.run(destination.pruneRetainedInstances)
	}
	cleanup.finish()
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

// A global can supply a descriptor outside the writer's own function arena.
// Publish its actual owner before removing any suspended-writer promise. The
// scope still pins every candidate; release must run outside nativeExecutionMu.
func (s *rv64NativeTableTargets) publishImportedReferenceRows() {
	hasGlobals := false
	for _, owner := range s.owners[:s.count] {
		if len(owner.importGlobalReferences()) != 0 {
			hasGlobals = true
			break
		}
	}
	if !hasGlobals {
		return
	}
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	for index, table := range s.writers {
		if table == nil {
			continue
		}
		duplicate := false
		for _, previous := range s.writers[:index] {
			duplicate = duplicate || previous == table
		}
		if !duplicate {
			s.publishHostTableReferenceRows(table)
		}
	}
}

// No physical root is released under native or container locks. Host-created
// destinations cannot introduce a backedge into an instance storage owner.
func (s *rv64NativeTableTargets) publishHostTableReferenceRows(table *Table) {
	table.owner.mu.Lock()
	defer table.owner.mu.Unlock()
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.owner.instance != nil || table.owner.closed || table.closed || len(table.desc) < 8 {
		return
	}
	size, capacity := uint64(binary.LittleEndian.Uint32(table.desc)), uint64(binary.LittleEndian.Uint32(table.desc[4:]))
	if size > capacity || capacity > railshot.MaxOwnedTableEntries || 8+capacity*coreruntime.TableEntryBytes > uint64(len(table.desc)) {
		return // A failed refresh may leave an invalid table in the scope.
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
				// Publication is deferred to the scope-release prune, after the
				// native and container locks held by this scan are gone.
				railshotRecordTableDescriptorDependencyLocked(table, owner, state, nil)
			}
			state.precise = true
			break
		}
	}
}
