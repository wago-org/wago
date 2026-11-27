// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func admitLegacyTable64IndirectTail(m *wasm.Module) bool {
	return admitOwnedTwoTable64IndirectTail(m, true, true) ||
		admitMixedTable64ActiveIndirectTail(m, true, true) ||
		admitImportedTable64IndirectTail(m, true, true)
}

func admitLegacyTable64(m *wasm.Module, production, importedTables bool) bool {
	return admitStateOnlyTable64(m, production, importedTables) ||
		admitOwnedTable64ActiveElement(m, production, importedTables) ||
		admitMixedTable64ActiveElement(m, production, importedTables) ||
		admitMixedTable64DualActiveElements(m, production, importedTables) ||
		admitMixedTable64DualActiveVectors(m, production, importedTables) ||
		admitOwnedTwoTable64Size(m, production, importedTables) ||
		admitMixedTable64Size(m, production, importedTables) ||
		admitMixedTable64GetSet(m, production, importedTables) ||
		admitMixedTable64Grow(m, production, importedTables) ||
		admitMixedTable64Fill(m, production, importedTables) ||
		admitMixedTable64Copy(m, production, importedTables) ||
		admitMixedTable64CrossCopy(m, production, importedTables) ||
		admitMixedTable64InitDrop(m, production, importedTables) ||
		admitMixedTable64Indirect(m, production, importedTables) ||
		admitOwnedTwoTable64GetSet(m, production, importedTables) ||
		admitOwnedTwoTable64Grow(m, production, importedTables) ||
		admitOwnedTwoTable64Fill(m, production, importedTables) ||
		admitOwnedTwoTable64Copy(m, production, importedTables) ||
		admitOwnedTwoTable64CrossCopy(m, production, importedTables) ||
		admitOwnedTwoTable64InitDrop(m, production, importedTables) ||
		admitOwnedTwoTable64Indirect(m, production, importedTables) ||
		admitOwnedTable64Size(m, production, importedTables) ||
		admitOwnedTable64Grow(m, production, importedTables) ||
		admitOwnedTable64GetSet(m, production, importedTables) ||
		admitOwnedTable64Indirect(m, production, importedTables) ||
		admitImportedTable64Indirect(m, production, importedTables) ||
		admitOwnedTable64Fill(m, production, importedTables) ||
		admitOwnedTable64Copy(m, production, importedTables) ||
		admitOwnedTable64InitDrop(m, production, importedTables) ||
		admitImportedTable64Size(m, production, importedTables) ||
		admitImportedTable64GetSet(m, production, importedTables) ||
		admitImportedTable64Grow(m, production, importedTables) ||
		admitImportedTable64Fill(m, production, importedTables) ||
		admitImportedTable64Copy(m, production, importedTables) ||
		admitImportedTable64InitDrop(m, production, importedTables)
}

func admitLegacyIndexedElements(m *wasm.Module, production, importedTables bool) bool {
	return admitMixedTable64DualActiveElements(m, production, importedTables) ||
		admitMixedTable64DualActiveVectors(m, production, importedTables) ||
		admitMixedTable64ActiveIndirect(m, production, importedTables)
}
