// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package abi

// RV64TailTargetsAnchorOffset is reserved in descriptor zero only. Ordinary
// function descriptors keep their structural signature key at this offset.
// The directory is immutable instance-owned data, never artifact metadata.
const (
	RV64TailTargetsAnchorOffset = 8
	RV64TailTargetsMagic        = uint64(0x5741474f54544431)
	RV64TailTargetsVersion      = 1
	RV64TailTargetsHeaderBytes  = 32
	RV64TailTargetsRowBytes     = 32
	RV64TailTargetsHost         = 1
)
