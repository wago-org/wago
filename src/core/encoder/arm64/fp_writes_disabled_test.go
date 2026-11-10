// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_regalloccheck

package arm64

import "testing"

func TestFPWritesOrdinaryObserverIsInert(t *testing.T) {
	var a Asm
	if old := a.ObserveFPWrites(func(uint32) { t.Fatal("ordinary build invoked FP observer") }); old != nil {
		t.Fatal("ordinary build retained FP observer")
	}
	a.Fadd(3, 4, 5, true)
	a.NeonInsD(3, X0, 1)
	a.Bl()
	if old := a.ObserveFPWrites(nil); old != nil {
		t.Fatal("ordinary build retained FP observer")
	}
}
