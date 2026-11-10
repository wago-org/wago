// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && tinygo && wago_lean && wago_minimal

package atomicfile

import "testing"

func TestMinimalTinyBuildOnlyOptionsFailClosed(t *testing.T) {
	file, reservation, err := createRetainedReplacementTemp("ignored", true)
	if err == nil || file != nil || reservation.valid() {
		t.Fatalf("retained staging = (%v, valid %t, %v), want unavailable", file, reservation.valid(), err)
	}
	if _, err := probeUmaskMode("ignored", 0o644, true); err == nil {
		t.Fatal("umask probing unexpectedly succeeded")
	}
}
