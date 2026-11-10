// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_guardpage && (((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo))

package runtime

import "testing"

func TestClosedGuardedMemoryDropsReservation(t *testing.T) {
	j, err := NewJobMemoryGuarded(65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if j.mem != nil || j.reserveBase != 0 || j.reserveLen != 0 || j.linOff != 0 || j.linLen != 0 {
		t.Fatal("closed guarded memory retains a native reservation")
	}
	if err := j.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
