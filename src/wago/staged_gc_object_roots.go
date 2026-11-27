// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "sync/atomic"

// stagedGCObjectRoots holds the roots owned by the Wasm GC array-element and
// reference-table products. Compact externrefs own their roots separately.
type stagedGCObjectRoots struct {
	arrayElements atomic.Pointer[gcArrayElementState]
	refTestTable  atomic.Pointer[gcRefTestTableState]
}

func (in *Instance) existingStagedGCObjectRoots() *stagedGCObjectRoots {
	if !wasmGCBuilt || in == nil {
		return nil
	}
	state := in.pluginState.Load()
	if state == nil {
		return nil
	}
	return state.stagedGCObjects()
}

func (in *Instance) existingGCArrayElementState() *gcArrayElementState {
	if roots := in.existingStagedGCObjectRoots(); roots != nil {
		return roots.arrayElements.Load()
	}
	return nil
}

func (in *Instance) existingGCRefTestTableState() *gcRefTestTableState {
	if roots := in.existingStagedGCObjectRoots(); roots != nil {
		return roots.refTestTable.Load()
	}
	return nil
}

func (in *Instance) existingGCExternConversionState() *gcExternConversionState {
	if table := in.existingGCRefTestTableState(); table != nil {
		return table.Conversion
	}
	return nil
}

// dropStagedGCObjectRoots runs during physical teardown under the same
// collector serialization as the rest of the instance's root cleanup.
func (in *Instance) dropStagedGCObjectRoots() {
	roots := in.existingStagedGCObjectRoots()
	if roots == nil {
		return
	}
	if table := roots.refTestTable.Load(); table != nil {
		table.drop(in.gc)
	}
	if elements := roots.arrayElements.Swap(nil); elements != nil {
		elements.drop(in.gc)
	}
}
