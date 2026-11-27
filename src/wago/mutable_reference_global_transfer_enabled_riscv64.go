// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_mutable_global

package wago

// rv64MutableFuncrefGlobalCopy authenticates the first cross-container guest
// reference write product. Its sole function copies imported mutable funcref
// global zero into imported mutable funcref global one through the parked
// ownership bridge; no reference value escapes through a callable signature.
func rv64MutableFuncrefGlobalCopy(c *Compiled) bool {
	return rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalTransferMetadata(c, "copy") && rv64MutableFuncrefGlobalCopyExecutionCode(c)
}

// rv64MutableFuncrefGlobalMove authenticates the first exact multi-write guest
// reference product. Its sole function copies source to target and then clears
// source, with each write crossing the ordinary parked ownership transaction.
func rv64MutableFuncrefGlobalMove(c *Compiled) bool {
	return rv64MutableFuncrefGlobalsForImage && rv64MutableFuncrefGlobalTransferMetadata(c, "move") && rv64MutableFuncrefGlobalMoveExecutionCode(c)
}
