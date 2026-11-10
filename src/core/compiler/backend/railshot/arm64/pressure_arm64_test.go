// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "testing"

func TestCompileMemoryPressureCheckpointRunsOnce(t *testing.T) {
	m := mod1(t, nil, nil, []byte{0x00, 0x01, 0x0b})
	calls := 0
	if _, err := CompileModuleWith(m, CompileOptions{MemoryPressureAt: 1, MemoryPressure: func() { calls++ }}); err != nil {
		t.Fatalf("CompileModuleWith: %v", err)
	}
	if calls != 1 {
		t.Fatalf("memory-pressure calls = %d, want 1", calls)
	}
}
