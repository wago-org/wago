// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

// MUL reads both sources before writing its destination. Concrete register
// operands therefore need no private copy, including two reads of the same pin.
// Leave deferred and memory operands on the established realization path.
func (f *fn) tryRegisterMultiply(node, left, right *elem, dest Reg) Reg {
	if !f.opt(optThreeOpSink) {
		return regNone
	}
	for _, e := range []*elem{left, right} {
		if e.elemKind() != ekValue {
			return regNone
		}
		switch e.st.kind {
		case stReg, stLocalReg, stGlobReg:
		default:
			return regNone
		}
	}
	savedPins := f.pinned
	rn, ownRN := f.materializeRead(left)
	f.pinned = f.pinned.add(rn)
	rm, ownRM := f.materializeRead(right)
	f.pinned = f.pinned.add(rm)
	rd := dest
	if rd == regNone {
		if ownRN {
			rd = rn
		} else if ownRM {
			rd = rm
		} else {
			rd = f.allocReg(maskOf(rn, rm))
		}
	}
	f.aluRR3(opMul, rd, rn, rm, node.st.typ.is64())
	f.pinned = savedPins
	if ownRN && rn != rd {
		f.release(rn)
	}
	if ownRM && rm != rd && rm != rn {
		f.release(rm)
	}
	f.stats.peep("register-multiply")
	f.consumeBlockBelow(node)
	f.occupy(node, rd)
	return rd
}
