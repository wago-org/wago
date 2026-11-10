// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func swarOwnedTableType(table *wasm.TableType) error {
	if table == nil {
		return nil
	}
	if table.Limits.Addr64 || table.Limits.Min > MaxOwnedTableEntries ||
		(table.Limits.HasMax && (table.Limits.Max < table.Limits.Min || table.Limits.Max > MaxOwnedTableEntries)) ||
		!wasm.EqualValType(wasm.RefVal(table.Ref), wasm.FuncRef) {
		return unsupported("SWAR private nullable funcref table32 declaration/bound")
	}
	return nil
}
