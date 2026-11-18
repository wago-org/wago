// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "os"

var floatMapDirectionGuardEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_DIRECTION_GUARD") != "1"

var floatMapPacketDependenceEnabled = os.Getenv("WAGO_ARM64_NO_FLOAT_MAP_PACKET_DEPENDENCE") != "1"

// Each emitted group loads its expression, stores one Q register, then begins
// the next group. Native unrolling never holds stores across these boundaries.
const floatMapVectorBytes = 16

// Before native-state changes, reject a known forward dependence inside one
// vector group. The guarded no-wrap address domain makes these widened offsets
// exact for equal symbolic bases. Invariant loads have separate whole-loop
// disjointness guards and never use this packet-local proof.
func (p *floatMapPlan) hasShortForwardDependence(loads []int) bool {
	dst := p.addresses[p.dest]
	for _, x := range loads {
		src := p.addresses[x]
		if src.base != dst.base || src.index != dst.index {
			continue
		}
		d, s := uint64(dst.bias)+dst.offset, uint64(src.bias)+src.offset
		if d > s && d-s < floatMapVectorBytes {
			return true
		}
	}
	return false
}
