// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"sync/atomic"
	"testing"
)

func TestSealingMappedCodeDoesNotLeaveAnExecutableRange(t *testing.T) {
	code, _, err := MapCode([]byte{0xc3})
	if err != nil {
		t.Fatal(err)
	}
	start := slicePtr(code)
	if err := SealCode(code); err != nil {
		_ = Unmap(code)
		t.Fatal(err)
	}
	if err := Unmap(code); err != nil {
		t.Fatal(err)
	}
	executableCodeMu.Lock()
	defer executableCodeMu.Unlock()
	for i := range executableCodeRanges {
		if atomic.LoadUintptr(&executableCodeRanges[i].start) == start {
			t.Fatal("unmapped code remains in the executable range registry")
		}
	}
}
