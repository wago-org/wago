// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build windows && (amd64 || arm64) && wago_guardpage

package runtime

import "testing"

func TestGuardedLinearMemoryUsesWindowsAllocationAlignment(t *testing.T) {
	j, err := NewJobMemoryGuarded(0, wasmPageBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	if got := j.reserveBase + uintptr(j.linOff); got%wasmPageBytes != 0 {
		t.Fatalf("linear-memory base %#x is not %d-byte aligned", got, wasmPageBytes)
	}
}
