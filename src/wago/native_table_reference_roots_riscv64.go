//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

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
			}
			state.precise = true
			break
		}
	}
}
