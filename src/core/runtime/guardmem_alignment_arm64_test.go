// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build wago_guardpage && (linux || darwin) && arm64

package runtime

import "testing"

func TestGuardedLinearMemoryIsWasmPageAligned(t *testing.T) {
	j, err := NewJobMemoryGuarded(wasmPageBytes, 2*wasmPageBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	linMem := j.reserveBase + uintptr(j.linOff)
	if linMem&uintptr(wasmPageBytes-1) != 0 {
		t.Fatalf("guarded linear memory base %#x is not %d-byte aligned", linMem, wasmPageBytes)
	}
	if j.linOff < basedataSize {
		t.Fatalf("guarded linear-memory offset %d overlaps %d-byte basedata", j.linOff, basedataSize)
	}
}
