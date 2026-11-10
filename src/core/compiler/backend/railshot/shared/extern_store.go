// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// ExternStoreMayCall accounts for receipt publication helpers in modules that
// need no collector of their own. Opaque words are not compact frame roots.
func ExternStoreMayCall(m *wasm.Module, kind wasm.InstrKind, index uint32) bool {
	if m == nil {
		return false
	}
	switch kind {
	case wasm.InstrGlobalSet:
		g, ok := m.GlobalTypeByIndex(index)
		return ok && wasm.ExternReferenceType(wasm.GlobalValueType(g))
	case wasm.InstrTableSet, wasm.InstrTableFill, wasm.InstrTableCopy, wasm.InstrTableGrow, wasm.InstrTableInit:
		for i := 0; i < m.TableCount(); i++ {
			if table, ok := m.TableType(uint32(i)); ok && wasm.ExternReferenceType(wasm.RefVal(table.Ref)) {
				return true
			}
		}
	}
	return false
}
