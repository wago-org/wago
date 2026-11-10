// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
)

const rv64GCStructNumericGlobalsHex = "0061736d01000000018780808000015f027f007f0106978080800002640000410a410bfb00000b64000041144115fb00000b078b808080000202673003000267310301"

var rv64GCStructNumericGlobalsSourceDigest = [sha256.Size]byte{0x03, 0x87, 0xe5, 0x19, 0xfa, 0x92, 0x1b, 0x90, 0x5d, 0x06, 0x57, 0xa6, 0xfa, 0xfb, 0x63, 0x0a, 0xb7, 0xac, 0xaa, 0x3a, 0x62, 0x82, 0xe3, 0x54, 0xb3, 0xf0, 0xf2, 0xe4, 0x5a, 0xdb, 0xfe, 0xee}
