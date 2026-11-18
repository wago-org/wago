// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

import (
	"math/bits"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// ScanModularWordLoads looks only through effects that cannot mutate memory or
// trap except for plain loads. It proves a dense set of adjacent word accesses
// using the same unchanged local, with a strict per-function scan budget.
func ScanModularWordLoads(r *wasm.Reader, local uint32, localBase int, first uint8, work *uint32, workLimit uint32) uint64 {
	const limit = 192
	scan := *r
	mask := uint64(1) << (first / 4)
	stage := uint8(0)
	var candidate uint32
	for n := 0; n < limit && *work < workLimit && scan.HasNext(); n++ {
		// Sparse probes rarely amortize their setup; stop before scanning a long
		// unrelated expression unless another matching load appears nearby.
		if n == 24 && bits.OnesCount64(mask) < 2 {
			return 0
		}
		(*work)++
		op, err := scan.Byte()
		if err != nil {
			break
		}
		switch {
		case op == 0x20:
			x, err := scan.U32()
			if err != nil {
				return 0
			}
			if uint64(x)+uint64(localBase) == uint64(local) {
				if stage == 4 {
					stage = 2 // constant, local, add
				} else {
					stage = 1
				}
			} else {
				stage = 0
			}
		case op == 0x21 || op == 0x22:
			x, err := scan.U32()
			if err != nil || uint64(x)+uint64(localBase) == uint64(local) {
				return mask
			}
			stage = 0
		case op == 0x41:
			v, err := scan.I32()
			if err != nil {
				return 0
			}
			if uint32(v) < 256 && uint32(v)&3 == 0 {
				candidate = uint32(v)
				if stage == 1 {
					stage = 2 // local, constant, add
				} else {
					stage = 4
				}
			} else {
				stage = 0
			}
		case op == 0x6a:
			if stage == 2 {
				stage = 3
			} else {
				stage = 0
			}
		case op >= 0x28 && op <= 0x35:
			align, err := scan.U32()
			if err != nil {
				return 0
			}
			mem := uint32(0)
			if align >= 64 && align < 128 {
				mem, err = scan.U32()
				if err != nil {
					return 0
				}
			}
			off, err := scan.U32()
			if err != nil || mem != 0 {
				return mask
			}
			if op == 0x28 && off == 0 && (stage == 1 || stage == 3) {
				if stage == 1 {
					candidate = 0
				}
				mask |= uint64(1) << (candidate / 4)
			}
			stage = 0
		case op == 0x01 || op == 0x1a || op == 0x1b,
			op >= 0x45 && op <= 0x6c, op >= 0x71 && op <= 0x7e,
			op >= 0x83 && op <= 0x8a, op == 0xa7 || op == 0xac || op == 0xad:
			stage = 0
		case op == 0x42:
			if _, err := scan.I64(); err != nil {
				return 0
			}
			stage = 0
		default:
			return mask
		}
	}
	return mask
}

// DenseModularWordSpan locates the minimum profitable contiguous word span.
func DenseModularWordSpan(mask uint64) (uint8, uint8, bool) {
	// Setup needs several checks to amortize; short groups keep the scalar checks.
	if bits.OnesCount64(mask) < 5 {
		return 0, 0, false
	}
	lo, hi := bits.TrailingZeros64(mask), bits.Len64(mask)-1
	// Dense means that if the sequence crosses 2^32, one access ends in the
	// final four bytes of memory; this makes the wrap case exactly testable.
	count := hi - lo + 1
	if count == 64 && mask != ^uint64(0) || count < 64 && mask>>lo != (uint64(1)<<count)-1 {
		return 0, 0, false
	}
	return uint8(lo * 4), uint8(hi * 4), true
}
