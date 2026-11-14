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

// The previous exhaustive search remains a test-only oracle for emitted fields.
func slowTestLogicalImmediate(imm uint64, is64 bool) (uint32, uint32, uint32, bool) {
	if !is64 {
		imm &= 0xffffffff
		imm |= imm << 32
	}
	if imm == 0 || imm == ^uint64(0) {
		return 0, 0, 0, false
	}
	size := 64
	for size > 2 {
		size /= 2
		mask := uint64(1)<<size - 1
		if imm&mask != (imm>>size)&mask {
			size *= 2
			break
		}
	}
	mask := ^uint64(0)
	if size < 64 {
		mask = uint64(1)<<size - 1
	}
	e := imm & mask
	ones := bits.OnesCount64(e)
	if ones == 0 || ones == size {
		return 0, 0, 0, false
	}
	run := uint64(1)<<ones - 1
	for k := 0; k < size; k++ {
		if ((run<<k)|(run>>uint(size-k)))&mask == e {
			n := uint32(0)
			if size == 64 {
				n = 1
			}
			return n, uint32((size - k) % size), (^uint32((size<<1)-1))&0x3f | uint32(ones-1), true
		}
	}
	return 0, 0, 0, false
}

func TestLogicalImmediateFieldsMatchSearch(t *testing.T) {
	check := func(value uint64, wide bool) {
		t.Helper()
		n, r, s, ok := encodeLogicalImm(value, wide)
		wn, wr, ws, want := slowTestLogicalImmediate(value, wide)
		if n != wn || r != wr || s != ws || ok != want {
			t.Fatalf("value %#x wide=%v got %d:%d:%d/%v want %d:%d:%d/%v", value, wide, n, r, s, ok, wn, wr, ws, want)
		}
	}
	for _, width := range []int{32, 64} {
		for n := uint32(0); n < 2; n++ {
			for r := uint32(0); r < 64; r++ {
				for s := uint32(0); s < 64; s++ {
					if value, ok := decodeTestLogicalImmediate(n, r, s, width); ok {
						check(value, width == 64)
					}
				}
			}
		}
	}
	x := uint64(0x9e3779b97f4a7c15)
	for i := 0; i < 100000; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		check(x, false)
		check(x, true)
	}
	for _, x := range []uint64{0, 1, ^uint64(0), 0xffffffff} {
		check(x, false)
		check(x, true)
	}
}
