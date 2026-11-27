// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"encoding/binary"

	"github.com/wago-org/wago/internal/buildfeatures"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const (
	gcTableFill     uint32 = 48
	gcTableCopy     uint32 = 49
	gcTableGrow     uint32 = 50
	gcTableInit     uint32 = 51
	gcTableElemDrop uint32 = 52

	gcTableSetForImage      = gcHelperMaskForImage&(1<<gcStructTableSet) != 0
	gcTableFillForImage     = buildfeatures.ReferenceTypes != 0 && gcHelperMaskForImage&(1<<gcTableFill) != 0
	gcTableGrowForImage     = buildfeatures.ReferenceTypes != 0 && gcHelperMaskForImage&(1<<gcTableGrow) != 0
	gcTableCopyForImage     = collectorTableBulkBuilt && gcHelperMaskForImage&(1<<gcTableCopy) != 0
	gcTableInitForImage     = collectorTableBulkBuilt && gcHelperMaskForImage&(1<<gcTableInit) != 0
	gcTableElemDropForImage = collectorTableBulkBuilt && gcHelperMaskForImage&(1<<gcTableElemDrop) != 0

	collectorTableHelpersForImage = gcTableSetForImage || gcTableFillForImage || gcTableGrowForImage ||
		gcTableCopyForImage || gcTableInitForImage || gcTableElemDropForImage
	compactTableAllocationForImage = compactExternTablesForImage &&
		(gcTableSetForImage || gcTableFillForImage || gcTableGrowForImage || gcTableCopyForImage || gcTableInitForImage)
)

func (in *Instance) dispatchGCTableHelperLocked(helper uint32, args, results []uint64, domain *gcStoreDomain, roots gc.RootSet) bool {
	if gcTableSetForImage && helper == gcStructTableSet {
		if compactExternTablesForImage && len(args) == 3 && in.storeCompactExternTable(args[2], args[0], args[1], domain, roots) {
			return true
		}
		if wasmGCBuilt || ownedExceptionReferencesRuntime() {
			in.dispatchCollectorTableSetLocked(args)
			return true
		}
		return false
	}
	tableWrite := gcTableFillForImage && helper == gcTableFill || gcTableGrowForImage && helper == gcTableGrow
	tableCopy := gcTableCopyForImage && helper == gcTableCopy || gcTableInitForImage && helper == gcTableInit
	tableDrop := gcTableElemDropForImage && helper == gcTableElemDrop
	if compactExternTablesForImage && tableWrite && in.dispatchCompactExternTableWrite(helper, args, results, domain, roots) ||
		(compactExternTablesForImage && tableCopy || compactExternElementsForImage && tableDrop) &&
			in.dispatchCompactExternTableBulk(helper, args, results, domain, roots) {
		return true
	}
	if (tableWrite || tableCopy || tableDrop) && (wasmGCBuilt || ownedExceptionReferencesRuntime()) {
		in.dispatchCollectorTableOpsLocked(helper, args, results)
		return true
	}
	return false
}

func (in *Instance) dispatchCollectorTableSetLocked(args []uint64) {
	if len(args) != 3 {
		panic(gcHelperFailuref("gc ref.test table-set helper args = %v, want index/ref/table", args))
	}
	table, index := args[2], args[0]
	if wasmGCBuilt {
		if state := in.existingGCRefTestTableState(); state != nil {
			if table >= uint64(len(state.Descriptors)) || index >= uint64(binary.LittleEndian.Uint32(state.Descriptors[table])) {
				panic(gcHelperTrap(coreruntime.TrapIndirectOutOfBounds))
			}
			if err := state.setTable(in.gc, table, index, args[1]); err != nil {
				panic(gcHelperFailure(err))
			}
			return
		}
	}
	if table >= uint64(in.c.tableCount()) || !isGCRefValType(in.c.tableElementType(int(table))) {
		panic(gcHelperFailuref("gc ref.test table state is unavailable"))
	}
	descriptor := in.tableDescriptor(int(table))
	if len(descriptor) < 8 {
		panic(gcHelperFailuref("generic GC table %d descriptor is unavailable", table))
	}
	if index >= uint64(binary.LittleEndian.Uint32(descriptor)) {
		panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
	}
	word := args[1]
	ref := gc.Ref(uint32(word))
	if word != uint64(ref) {
		panic(gcHelperFailuref("generic GC table set contains non-compact reference %#x", word))
	}
	binary.LittleEndian.PutUint64(descriptor[8+index*8:], word)
	in.gc.WriteBarrierRoot(ref)
}

func (in *Instance) dispatchCollectorTableOpsLocked(helper uint32, args, results []uint64) {
	switch {
	case gcTableFillForImage && helper == gcTableFill:
		if len(args) != 4 {
			panic(gcHelperFailuref("gc table fill helper arity = %d, want 4", len(args)))
		}
		descriptor, length, _, exact := in.gcRootedTableDescriptor(args[3])
		start, count := args[0], args[2]
		if start+count < start || start+count > uint64(length) {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		ref := in.gcRootedTableReference(args[1], exact)
		for i := uint64(0); i < count; i++ {
			binary.LittleEndian.PutUint64(descriptor[8+(start+i)*8:], uint64(ref))
		}
		in.gc.WriteBarrierRoot(ref)
	case gcTableGrowForImage && helper == gcTableGrow:
		if len(args) != 3 || len(results) < 1 {
			panic(gcHelperFailuref("gc table grow helper arity = %d/%d, want 3/at-least-1", len(args), len(results)))
		}
		descriptor, length, capacity, exact := in.gcRootedTableDescriptor(args[2])
		ref := in.gcRootedTableReference(args[0], exact)
		delta := args[1]
		newLength := uint64(length) + delta
		if newLength < uint64(length) || newLength > uint64(capacity) {
			results[0] = uint64(^uint32(0))
			break
		}
		for i := uint64(length); i < newLength; i++ {
			binary.LittleEndian.PutUint64(descriptor[8+i*8:], uint64(ref))
		}
		in.gc.WriteBarrierRoot(ref)
		// Publish length only after every newly visible root has been initialized.
		binary.LittleEndian.PutUint32(descriptor, uint32(newLength))
		results[0] = uint64(length)
	default:
		if gcTableCopyForImage || gcTableInitForImage || gcTableElemDropForImage {
			in.dispatchCollectorTableBulkHelperLocked(helper, args)
		}
	}
}
