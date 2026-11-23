// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLoopFreePinLeavesPollRegisterAvailable(t *testing.T) {
	saved := loopFreePinEnabled
	loopFreePinEnabled = true
	defer func() { loopFreePinEnabled = saved }()

	body := []byte{
		0x20, 1, // local.get 1
		0x20, 1, // local.get 1
		0x6a,    // i32.add
		0x0d, 0, // br_if 0
		0x0b, // end
	}
	f := fn{
		nLocals:         2,
		locals:          []localDef{{reg: RDI, state: lsStackReg}, {reg: regNone, state: lsMem}},
		localType:       []machineType{mtI32, mtI32},
		pinnedLocals:    []int{0},
		pinnedLocalMask: regMask(0).add(RDI),
	}
	for _, want := range []Reg{RSI, RBP} {
		plan, _ := f.planLoopPinExchange(wasm.NewReader(body))
		if plan != loopPinEntry(-1, 1, want) {
			t.Fatalf("interruptible=%t: plan %#x, want a free pin in %v", f.interruptible, plan, want)
		}
		f.interruptible = true
	}
}
