//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Private ownership groundwork; public imported-table admission remains closed.
// A caller refreshes under nativeExecutionMu before entry and every resume,
// retains this set through cold decoration, then releases outside that mutex.
// Refresh merges new targets without dropping suspended activations' old roots.
// Container-held Go pointers alone are not a physical activation lifetime.
type rv64NativeTableTargets struct {
	owners [257]*Instance
	count  int
}

func (s *rv64NativeTableTargets) add(owner *Instance) error {
	if owner == nil {
		return fmt.Errorf("RV64 table target owner is nil")
	}
	for _, existing := range s.owners[:s.count] {
		if existing == owner {
			return nil
		}
	}
	if s.count == len(s.owners) {
		return fmt.Errorf("RV64 table target owner bound")
	}
	// Existing table/direct attachments authorize use after logical Close. The
	// container lock or a retained predecessor still owns physical storage here.
	if !owner.retainResourceRootForFinalization() {
		return fmt.Errorf("RV64 table target lost physical resources")
	}
	s.owners[s.count] = owner
	s.count++
	return nil
}

func (s *rv64NativeTableTargets) release() {
	// Releasing the last retired root can reenter finalization/native locking.
	for s.count != 0 {
		s.count--
		owner := s.owners[s.count]
		s.owners[s.count] = nil
		owner.releaseResourceRoot()
	}
}

func (s *rv64NativeTableTargets) owner(identity uintptr) *Instance {
	if identity == 0 {
		return nil
	}
	for _, owner := range s.owners[:s.count] {
		if owner.nativeContext == identity {
			return owner
		}
	}
	return nil
}

func (s *rv64NativeTableTargets) tableRoots(table *Table, edges *int) error {
	if table == nil || table.owner == nil {
		return fmt.Errorf("RV64 retained table has no owner")
	}
	table.owner.mu.Lock()
	defer table.owner.mu.Unlock()
	table.mu.Lock()
	defer table.mu.Unlock()
	if table.owner.closed || table.closed || table.owner.addr64 || table.owner.elementType != ValFuncRef || len(table.desc) < 8 {
		return fmt.Errorf("RV64 retained table storage/type is invalid")
	}
	if table.owner.instance != nil {
		if err := s.add(table.owner.instance); err != nil {
			return err
		}
	}
	for owner := range table.retained {
		*edges++
		if *edges > 4096 {
			return fmt.Errorf("RV64 table attachment edge bound")
		}
		if err := s.add(owner); err != nil {
			return err
		}
	}
	return nil
}

func (s *rv64NativeTableTargets) refresh(root *Instance) error {
	if s.count != 0 && s.owners[0] != root {
		return fmt.Errorf("RV64 table target set cannot change its root")
	}
	if err := s.add(root); err != nil {
		return err
	}
	edges := 0
	for cursor := 0; cursor < s.count; cursor++ {
		owner := s.owners[cursor]
		if owner.c == nil || owner.jm == nil || owner.base == 0 || owner.nativeContext == 0 || owner.hostEvents != nil || !owner.c.nativeContextTrapABI {
			return fmt.Errorf("RV64 table target requires a retained native framed owner")
		}
		if err := owner.c.validateCached(); err != nil {
			return err
		}
		if owner.c.tableCount() > 1 {
			return fmt.Errorf("RV64 table target table directory bound")
		}
		for index := range owner.c.Imports {
			edges++
			if edges > 4096 {
				return fmt.Errorf("RV64 table attachment edge bound")
			}
			if export, native := rv64NativeFunctionAttachment(owner, index); native && export != nil {
				if err := s.add(export.inst); err != nil {
					return err
				}
			}
		}
		for index := 0; index < owner.c.tableImportCount(); index++ {
			table, ok := owner.imports.table(owner.c.tableImportBindingKey(index))
			if !ok {
				return fmt.Errorf("RV64 retained imported table is missing")
			}
			if err := s.tableRoots(table, &edges); err != nil {
				return err
			}
		}
		owner.lifeMu.Lock()
		table := owner.table
		owner.lifeMu.Unlock()
		for table != nil {
			edges++
			if edges > 4096 {
				return fmt.Errorf("RV64 table attachment edge bound")
			}
			if err := s.tableRoots(table, &edges); err != nil {
				return err
			}
			owner.lifeMu.Lock()
			table = table.next
			owner.lifeMu.Unlock()
		}
	}
	// Only after acquiring every reachable physical root do we examine rows.
	// Raw refSlot/context fields never authorize dereferencing unknown storage.
	for _, owner := range s.owners[:s.count] {
		if owner.c.tableCount() == 0 {
			continue
		}
		if err := s.validateTable(owner.tableDescriptor(0)); err != nil {
			return err
		}
	}
	return nil
}

func (s *rv64NativeTableTargets) validateTable(table []byte) error {
	if len(table) < 8 {
		return fmt.Errorf("RV64 table descriptor extent")
	}
	size, capacity := binary.LittleEndian.Uint32(table), binary.LittleEndian.Uint32(table[4:])
	if size > capacity || capacity > railshot.MaxOwnedTableEntries || uint64(8)+uint64(capacity)*coreruntime.TableEntryBytes > uint64(len(table)) {
		return fmt.Errorf("RV64 table descriptor capacity/extent")
	}
	for index := uint32(0); index < size; index++ {
		row := table[8+int(index)*coreruntime.TableEntryBytes : 8+int(index+1)*coreruntime.TableEntryBytes]
		if binary.LittleEndian.Uint64(row) == 0 {
			var empty [coreruntime.TableEntryBytes]byte
			if !bytes.Equal(row, empty[:]) {
				return fmt.Errorf("RV64 null table row has nonzero identity")
			}
			continue
		}
		if !s.authenticatedRow(row) {
			return fmt.Errorf("RV64 table row lacks a qualified retained canonical owner")
		}
	}
	return nil
}

func (s *rv64NativeTableTargets) authenticatedRow(row []byte) bool {
	descriptor := binary.LittleEndian.Uint64(row[coreruntime.TableEntryRefSlotOffset:])
	if descriptor == 0 || descriptor&7 != 0 {
		return false
	}
	for _, owner := range s.owners[:s.count] {
		// Membership uses the owner's actual allocation before reading any candidate.
		base := uint64(offHeapSlicePtr(owner.funcRefDescs))
		if base == 0 || descriptor < base+coreruntime.FuncRefDescBytes || descriptor-base >= uint64(len(owner.funcRefDescs)) {
			continue
		}
		offset := descriptor - base
		if offset%coreruntime.FuncRefDescBytes != 0 || offset+coreruntime.FuncRefDescBytes > uint64(len(owner.funcRefDescs)) {
			return false
		}
		function := int(offset/coreruntime.FuncRefDescBytes) - 1
		local := function - owner.c.NumImports
		if local < 0 || local >= len(owner.c.Entry) || local >= len(owner.c.Funcs) || function >= len(owner.c.FuncTypeID) {
			return false
		}
		code := owner.c.Entry[local]
		if code < 0 || code&15 != 0 || uint64(code) >= uint64(len(owner.c.code)) {
			return false
		}
		canonical := owner.funcRefDescs[int(offset) : int(offset)+coreruntime.FuncRefDescBytes]
		if !bytes.Equal(row, canonical[:coreruntime.TableEntryBytes]) {
			return false
		}
		return binary.LittleEndian.Uint64(canonical) == uint64(owner.base)+uint64(code) &&
			binary.LittleEndian.Uint64(canonical[coreruntime.TableEntrySigKeyOffset:]) == owner.c.FuncTypeID[function] &&
			binary.LittleEndian.Uint64(canonical[coreruntime.TableEntryHomeLinMemOffset:]) == uint64(owner.jm.LinMemBase())|abi.FuncRefLocalWrapperHomeTag &&
			binary.LittleEndian.Uint64(canonical[coreruntime.TableEntryRefSlotOffset:]) == descriptor &&
			binary.LittleEndian.Uint64(canonical[coreruntime.FuncRefContextOffset:]) == uint64(owner.nativeContext)
	}
	return false
}
