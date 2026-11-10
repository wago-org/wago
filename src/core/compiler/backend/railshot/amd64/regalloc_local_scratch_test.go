// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && wago_regalloccheck

package amd64

import (
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/amd64"
)

func localScratchCheckFunction(t *testing.T) *fn {
	t.Helper()
	f := &fn{
		a: &enc.Asm{}, s: newStack(),
		nLocals: 3, localType: []machineType{mtI32, mtI32, mtF64},
		locals: []localDef{
			{typ: mtI32, reg: R12, state: lsStackReg},
			{typ: mtI32, reg: R13, state: lsStackReg},
			{typ: mtF64, reg: 3, isFloat: true, state: lsStackReg},
		},
		pinnedLocals:    []int{0, 1, 2},
		pinnedLocalMask: maskOf(R12).add(R13), fpinnedLocalMask: maskOf(3),
	}
	t.Cleanup(f.checkEndLifetimes)
	return f
}

func TestRegallocLocalScratchRejectsRelinquishedPin(t *testing.T) {
	f := localScratchCheckFunction(t)
	// Leave only R13 available for the allocator's pinned-local fallback.
	for _, reg := range gpAlloc {
		if reg != R13 {
			f.pinned = f.pinned.add(reg)
		}
	}
	saved := f.checkProtectLocalPins()
	defer checkRestoreRead(f, saved)
	reg := f.allocRegOrNone(0)
	if reg != R13 || f.locals[1].state != lsMem || f.regUser[reg] != nil {
		t.Fatalf("did not borrow an untracked local pin: reg=%d state=%d", reg, f.locals[1].state)
	}
	f.pinned = f.pinned.add(reg)
	// A fast path can allocate raw carriers this way. Changing local metadata
	// cannot permit a write when its exit skips ordinary local recovery.
	requireAllocationFailure(t, "read-only GP input overwritten", func() { f.a.MovImm32(reg, 99) })
}

func TestRegallocLocalScratchAllowsOnlyDeclaredOutputs(t *testing.T) {
	f := localScratchCheckFunction(t)
	saved := f.checkProtectLocalPins()
	defer checkRestoreRead(f, saved)
	requireAllocationFailure(t, "read-only GP input overwritten", func() { f.a.MovRegReg32(R12, RAX) })
	f.checkAllowLocalPinWrite(saved, 0)
	f.a.MovRegReg32(R12, RAX)
	requireAllocationFailure(t, "read-only GP input overwritten", func() { f.a.MovImm32(R13, 1) })
	requireAllocationFailure(t, "read-only FP input overwritten", func() { f.a.FAdd(3, 4, true) })
	f.checkAllowLocalPinWrite(saved, 2)
	f.a.FAdd(3, 4, true)
	// The GP and FP register banks must not release each other's pins.
	requireAllocationFailure(t, "read-only GP input overwritten", func() { f.a.MovImm32(R13, 1) })
}

func TestRegallocLocalScratchPreservesOuterReadGuards(t *testing.T) {
	for _, wide := range []bool{false, true} {
		f := localScratchCheckFunction(t)
		outer := checkProtectRead(f, R12, false, wide)
		saved := f.checkProtectLocalPins()
		f.checkAllowLocalPinWrite(saved, 0)
		requireAllocationFailure(t, "read-only GP input overwritten", func() { f.a.MovImm32(R12, 1) })
		checkRestoreRead(f, saved)
		f.a.MovImm32(R13, 1)
		requireAllocationFailure(t, "read-only GP input overwritten", func() { f.a.MovImm32(R12, 1) })
		checkRestoreRead(f, outer)
		f.a.MovImm32(R12, 1)
	}
}

func TestRegallocLocalScratchRestoresOnPanic(t *testing.T) {
	f := localScratchCheckFunction(t)
	gpWrites, fpWrites := 0, 0
	f.a.ObserveGPWrites(func(uint32) { gpWrites++ })
	f.a.ObserveFPWrites(func(uint32) { fpWrites++ })
	requireAllocationFailure(t, "read-only GP input overwritten", func() {
		saved := f.checkProtectLocalPins()
		defer checkRestoreRead(f, saved)
		f.a.MovImm32(RAX, 1)
		f.a.FAdd(4, 5, true)
		f.a.MovImm32(R12, 1)
	})
	if gpWrites != 1 || fpWrites != 1 || f.readCheck != (regallocReadMask{}) {
		t.Fatalf("scope lost outer observers or retained masks: GP=%d FP=%d masks=%+v", gpWrites, fpWrites, f.readCheck)
	}
	f.a.MovImm32(R12, 1)
	f.a.FAdd(3, 4, true)
	if gpWrites != 2 || fpWrites != 2 {
		t.Fatalf("outer observers were not restored: GP=%d FP=%d", gpWrites, fpWrites)
	}
}
