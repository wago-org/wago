// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

// DefinedType resolves one flattened module-local type index from this view.
// The returned descriptor is detached from runtime-owned metadata: callers may
// mutate its slices without affecting the compiled module. Type indexes inside
// the descriptor remain local to this ModuleView and may be resolved by calling
// DefinedType again.
//
// This is intentionally narrower than exposing the underlying Compiled module.
// Instantiation interceptors and observers can use it to validate exact imported
// reference signatures while retaining the read-only authority of ModuleView.
func (v ModuleView) DefinedType(index uint32) (DefinedTypeDescriptor, bool) {
	if v.compiled == nil || uint64(index) >= uint64(len(v.compiled.Types)) {
		return DefinedTypeDescriptor{}, false
	}
	cloned := cloneDefinedTypeDescriptors(v.compiled.Types[index : index+1])
	return cloned[0], true
}
