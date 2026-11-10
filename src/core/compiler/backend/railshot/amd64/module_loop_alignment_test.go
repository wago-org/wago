// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "testing"

func TestCompactLoopAlign32PreservesModuleBaseAMD64(t *testing.T) {
	policy := currentCodegenPolicy()
	if !policy.EnabledOption(optCompactLoopAlign32) {
		t.Skip("compact-loop-align32 disabled")
	}
	for off := 0; off < 64; off++ {
		for _, adapter := range []bool{false, true} {
			pad := functionStartPadding(off, 48, adapter, funcHints{flags: hintHasLoop}, policy)
			if (off+pad)&31 != 0 {
				t.Fatalf("offset=%d adapter=%t padding=%d moves a relative32-byte loop off its fetch-block phase", off, adapter, pad)
			}
		}
	}
}
