// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_regalloccheck

package arm64

// ObserveFPWrites observes possible writes to V0..V31 independently of transfer
// windows. Partial/lane writes count; calls conservatively report every bit.
// Unlike XZR, V31 is a physical destination. Restore the returned observer on
// normal completion and panic.
func (a *Asm) ObserveFPWrites(fn func(uint32)) func(uint32) {
	old := a.fpWriteObserver
	a.fpWriteObserver = fn
	return old
}

func (a *Asm) regallocFPWrites(mask uint32) {
	if mask != 0 && a.fpWriteObserver != nil {
		a.fpWriteObserver(mask)
	}
}
