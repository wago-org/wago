// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin) && arm64

package arm64

import "testing"

func TestLoopIntConstSwitchEquivalentArm64(t *testing.T) {
	m := loopIntConstModuleArm64(t)
	saved := loopIntConstEnabled
	defer func() { loopIntConstEnabled = saved }()
	for _, iterations := range []uintptr{1, 2, 7, 31} {
		loopIntConstEnabled = true
		on := uint32(runArm64Internal2(t, m, 3, iterations))
		loopIntConstEnabled = false
		off := uint32(runArm64Internal2(t, m, 3, iterations))
		if on != off {
			t.Fatalf("iterations=%d: enabled=%#x disabled=%#x", iterations, on, off)
		}
	}
}
