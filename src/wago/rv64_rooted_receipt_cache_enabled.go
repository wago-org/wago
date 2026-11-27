// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_gc || !wago_no_exception_handling)

package wago

const rv64RootedReceiptsBuilt = true

type rv64RootedReceiptCache struct {
	receipt *rv64RootedReceipt
}

func (m *validateMemo) rootedReceipt() *rv64RootedReceipt {
	if m == nil {
		return nil
	}
	return m.rv64RootedReceiptCache.receipt
}

func (m *validateMemo) setRootedReceipt(receipt *rv64RootedReceipt) {
	m.rv64RootedReceiptCache.receipt = receipt
}
