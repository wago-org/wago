// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo && (linux || darwin || windows)

package runtime

import "testing"

func TestPrepareBoundedIntContextBindsStableReentryStackAMD64(t *testing.T) {
	eng, jm, _ := fixture(t)
	linMem := jm.LinMemBase()
	jm.putU64(offTrapStackReentry, 0)
	eng.PrepareBoundedIntContext(linMem)
	if got, want := jm.getU64(offTrapStackReentry), uint64(eng.stackTop-40); got != want {
		t.Fatalf("trap reentry stack = %#x, want %#x", got, want)
	}
}
