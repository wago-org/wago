// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func railshotUsesGuestGlobalStoreHelper(*Compiled) bool { return false }

func (*Instance) retainGuestGlobalStoreDescriptor(uint32, []uint64, []uint64) (*Instance, error) {
	return nil, nil
}

func (*Instance) dispatchGuestGlobalStore(uint32, []uint64, []uint64) bool { return false }

func (*Instance) dispatchGuestGlobalStoreWithOwner(uint32, []uint64, []uint64, *Instance) bool {
	return false
}
