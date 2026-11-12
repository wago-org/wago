// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func railshotManagedVoidTableEntry(*Instance, uint32) (uintptr, bool, error) {
	return 0, false, nil
}

func railshotManagedTableDescriptor(in *Instance) []byte {
	return in.tableDescriptor(0)
}
