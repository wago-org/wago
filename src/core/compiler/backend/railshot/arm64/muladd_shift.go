// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "math/bits"

// A coefficient 1+2^k needs no multiplier register. Form its product with one
// shifted add, then add/subtract the accumulator. All arithmetic wraps at the
// Wasm width, including coefficients whose signed carrier has its high bit set.
func (f *fn) tryShiftedMulAdd(node, mul, addend *elem, dest Reg, wide bool) Reg {
	source, constant := f.s.arg0(mul), f.s.arg1(mul)
	if source.elemKind() == ekValue && source.st.kind == stConst {
		source, constant = constant, source
	}
	if constant.elemKind() != ekValue || constant.st.kind != stConst {
		return regNone
	}
	coefficient := uint64(constant.st.cval)
	if !wide {
		coefficient = uint64(uint32(coefficient))
	}
	if coefficient <= 2 || (coefficient-1)&(coefficient-2) != 0 {
		return regNone
	}
	saved := f.pinned
	ra, ownRa := f.mulAddReadOperand(addend)
	f.pinned = f.pinned.add(ra)
	x, ownX := f.mulAddReadOperand(source)
	f.pinned = f.pinned.add(x)
	d := dest
	if d == regNone {
		d = f.allocReg(0)
	}
	product := d
	if product == ra {
		// The accumulator is still needed after forming the product. This
		// also covers x==ra==destination without overwriting the old value.
		product = f.allocReg(maskOf(d))
	}
	f.a.AddShifted(product, x, x, uint8(bits.TrailingZeros64(coefficient-1)), !wide)
	if node.deferredOp() == opSub {
		if wide {
			f.a.Sub64(d, ra, product)
		} else {
			f.a.Sub32(d, ra, product)
		}
	} else if wide {
		f.a.Add64(d, product, ra)
	} else {
		f.a.Add32(d, product, ra)
	}
	f.pinned = saved
	if product != d {
		f.release(product)
	}
	if ownX {
		f.release(x)
	}
	if ownRa && ra != x {
		f.release(ra)
	}
	f.stats.peep("mul-add-shifted-product")
	f.stats.peep("mul-add-fuse")
	f.consumeBlockBelow(node)
	f.occupy(node, d)
	return d
}
