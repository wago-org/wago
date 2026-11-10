// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

// Use the three explicit register operands of LSLV/LSRV/ASRV. A borrowed
// source or count need not be copied, even when the destination aliases it.
func (f *fn) condenseVariableShift(node *elem, dest Reg, kind shiftKind, wide bool) Reg {
	left, right := f.s.arg0(node), f.s.arg1(node)
	f.materializeRead(left)
	saved := f.pinned
	count, countOwned := f.materializeRead(right)
	f.pinned = saved.add(count)
	source, sourceOwned := f.materializeRead(left)
	f.pinned = f.pinned.add(source)
	if dest == regNone {
		if sourceOwned {
			dest = source
		} else if countOwned {
			dest = count
		} else {
			dest = f.allocReg(maskOf(source, count))
		}
	}
	switch kind {
	case shLSL:
		f.lslv(dest, source, count, wide)
	case shLSR:
		f.lsrv(dest, source, count, wide)
	case shASR:
		f.asrv(dest, source, count, wide)
	}
	f.pinned = saved
	if sourceOwned && source != dest {
		f.release(source)
	}
	if countOwned && count != dest && count != source {
		f.release(count)
	}
	f.consumeBlockBelow(node)
	f.occupy(node, dest)
	f.stats.peep("variable-shift-three-operand")
	return dest
}
