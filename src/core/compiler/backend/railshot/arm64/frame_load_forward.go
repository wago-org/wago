// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

// Control and helper opcodes end the proof even when they emit no native word.
// Every admitted native word is independently checked before forwarding.
func frameForwardOpcode(op byte) bool {
	return op == 0x01 || op == 0x1a || op == 0x1b || op >= 0x20 && op <= 0x24 || op >= 0x28 && op <= 0x3e || op >= 0x41 && op <= 0xc4
}

func (f *fn) rememberFrameStore(local int, reg Reg) {
	f.frameForwardEnd = 0
	f.frameForwardLocalLive = false
	if !f.opt(optStoreLoadFwd) || f.moduleEH || len(f.customInstructions) != 0 || (f.localType[local] != mtI32 && f.localType[local] != mtI64) || reg == SP || f.a.Len() < 4 {
		return
	}
	source, offset, wide, ok := spStoreImm(rdWord(f.a.B, f.a.Len()-4))
	if ok && wide && source == reg && int32(offset) == f.localOff(local) {
		f.frameForwardEnd, f.frameForwardOff, f.frameForwardReg = uint32(f.a.Len()), int32(offset), reg
		f.frameForwardLocal, f.frameForwardLocalLive = local, true
	}
}

func (f *fn) frameForwardSource(offset int32) (Reg, bool) {
	if f.frameForwardEnd == 0 || f.frameForwardOff != offset || !f.opt(optStoreLoadFwd) {
		return regNone, false
	}
	start, end := int(f.frameForwardEnd), f.a.Len()
	if end < start || end-start > 48 {
		f.frameForwardEnd = 0
		return regNone, false
	}
	source := f.frameForwardReg
	for pc := start; pc < end; pc += 4 {
		writes, ok := frameForwardWord(rdWord(f.a.B, pc))
		if !ok || writes&(uint32(1)<<source|uint32(1)<<linMemReg) != 0 {
			f.frameForwardEnd = 0
			return regNone, false
		}
	}
	return source, true
}

func (f *fn) forwardFrameLoad(dst Reg, offset int32) bool {
	source, ok := f.frameForwardSource(offset)
	if !ok {
		return false
	}
	if dst != source {
		f.a.MovReg64(dst, source)
	}
	f.stats.peep("frame-load-forward")
	return true
}

func frameForwardWord(w uint32) (uint32, bool) {
	// Plain scalar unsigned-offset loads may read frame homes. Stores to SP
	// decline, including another width/overlapping slot. Other stores must
	// have the pinned linear-memory base; arbitrary native pointers may alias
	// the frame. No writeback admitted.
	if w&0x3b000000 == 0x39000000 && w&(1<<26) == 0 && w>>22&3 <= 1 {
		if w&(1<<22) != 0 {
			return (uint32(1) << (w & 31)) & 0x7fffffff, true
		}
		return 0, w>>5&31 == uint32(linMemReg)
	}
	// The only admitted register-offset store has the pinned linear-memory base.
	// Linear memory cannot alias a native frame. Exclude atomics and writeback.
	if w&0x3fe00c00 == 0x38200800 && w>>5&31 == uint32(linMemReg) {
		return 0, true
	}
	x, ok := guardedIntegerWord(w)
	return x.writes, ok
}
