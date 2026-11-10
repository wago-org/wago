// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "testing"

func TestCallSafePinPoolExcludesCallerSavedRegistersAfterReservations(t *testing.T) {
	pool := []Reg{X19, X20, X21, X23, X24, X9, X10, X11, X15, X27}
	got := callSafePinPool(pool)
	want := [...]Reg{X19, X20, X21, X23, X24}
	if len(got) != len(want) {
		t.Fatalf("safe pool = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("safe pool = %v, want %v", got, want)
		}
	}
}
