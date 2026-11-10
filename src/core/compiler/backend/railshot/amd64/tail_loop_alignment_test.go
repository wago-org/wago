// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"testing"
)

func TestCompactLoopFunctionStartAlignment(t *testing.T) {
	on, err := optimizationBindings.ResolveSnapshot(map[string]bool{"compact-loop-align32": true}, OptimizationSnapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	off, err := optimizationBindings.ResolveSnapshot(map[string]bool{"compact-loop-align32": false}, OptimizationSnapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, disabled, compact := shared.DefaultCodegenPolicy(on), shared.DefaultCodegenPolicy(off), shared.CompactCodegenPolicy(on)
	for _, kind := range []uint8{layoutHasLoop, layoutHasTailCall} {
		for offset := 0; offset < 128; offset++ {
			// Asm.AlignLoop32 is relative to the function, so the function must also
			// start on a cache-line boundary so peeled headers have stable placement.
			pad := functionStartPaddingFlags(offset, 48, layoutHostAdapter|kind, ordinary)
			if (offset+pad)%64 != 0 || pad < 0 || pad > 63 {
				t.Fatalf("offset=%d pad=%d leaves loop misaligned", offset, pad)
			}
			for _, tc := range []struct {
				body   int
				flags  uint8
				policy CodegenPolicy
				want   int
			}{
				{48, layoutHostAdapter | kind, disabled, (-offset) & 15},
				{65, layoutHostAdapter | kind, ordinary, (-offset) & 15},
				{48, layoutHostAdapter, ordinary, (-offset) & 15},
				{48, layoutHostAdapter | kind, compact, 0},
			} {
				if got := functionStartPaddingFlags(offset, tc.body, tc.flags, tc.policy); got != tc.want {
					t.Fatalf("offset=%d body=%d flags=%x padding=%d want=%d", offset, tc.body, tc.flags, got, tc.want)
				}
			}
		}
	}
}
