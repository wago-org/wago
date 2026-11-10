// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package abi

// RV64 public contexts extend the shared 120-byte image. The capability word is
// reserved for instance-owned tail dispatch; the invocation word borrows an
// Engine-owned view only while that root's execution lease is held.
const (
	RV64TailCapabilityOffset          = 120
	RV64InvocationContextOffset       = 128
	RV64InstanceContextBytes          = 136
	RV64InvocationViewBytes           = 64
	RV64InvocationMagic               = uint64(0x5741474f49564331)
	RV64InvocationFenceOffset         = 8
	RV64InvocationTopOffset           = 16
	RV64InvocationArgumentsOffset     = 24
	RV64InvocationArgumentBytesOffset = 32
	RV64InvocationResultsOffset       = 40
	RV64InvocationResultBytesOffset   = 48
	// The legacy 56-byte basedata word is unused on RV64. Unlike prototype
	// context images, this live activation slot is native-lease-owned.
	RV64InvocationViewOffset    = 56
	RV64InvocationFirstRAOffset = 56
	RV64NativeWrapperSaveBytes  = 144
)
