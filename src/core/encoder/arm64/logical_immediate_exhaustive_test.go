// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package arm64

import (
	"math/bits"
	"testing"
)

// Decode the architectural logical-immediate fields independently of the
// encoder. Multiple field combinations may describe the same mask.
func decodeTestLogicalImmediate(n, immr, imms uint32, width int) (uint64, bool) {
	if width == 32 && n != 0 {
		return 0, false
	}
	length := bits.Len32(n<<6|(^imms&63)) - 1
	if length < 1 {
		return 0, false
	}
	size := 1 << length
	if size > width {
		return 0, false
	}
	levels := uint32(size - 1)
	s, r := imms&levels, immr&levels
	if s == levels {
		return 0, false
	}
	mask := ^uint64(0)
	if size < 64 {
		mask = uint64(1)<<size - 1
	}
	value := uint64(1)<<(s+1) - 1
	if r != 0 {
		value = (value>>r | value<<uint(size-int(r))) & mask
	}
	for filled := size; filled < width; filled *= 2 {
		value |= value << filled
	}
	return value, true
}

func TestLogicalImmediateAllArchitecturalMasks(t *testing.T) {
	for _, width := range []int{32, 64} {
		for n := uint32(0); n < 2; n++ {
			for r := uint32(0); r < 64; r++ {
				for s := uint32(0); s < 64; s++ {
					value, valid := decodeTestLogicalImmediate(n, r, s, width)
					if !valid {
						continue
					}
					en, er, es, ok := encodeLogicalImm(value, width == 64)
					got, decoded := decodeTestLogicalImmediate(en, er, es, width)
					if !ok || !decoded || got != value {
						t.Fatalf("width %d mask %#x encoded %d:%d:%d (ok %v) decoded %#x (ok %v)", width, value, en, er, es, ok, got, decoded)
					}
				}
			}
		}
	}
}
