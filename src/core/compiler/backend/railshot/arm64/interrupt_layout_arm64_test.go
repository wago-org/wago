// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"testing"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestPollFreeLoopHeaderUsesPolicyAlignmentWithoutPollPadding(t *testing.T) {
	for _, align := range []uint8{4, 5, 6} {
		for _, prefix := range []int{0, 4, 12, 16, 20, 28, 32, 60} {
			for _, interruptible := range []bool{false, true} {
				f := fn{a: &a64.Asm{}, interruptible: interruptible, nLocals: 4}
				f.policy.LoopAlignLog2 = align
				f.a.B = make([]byte, prefix)
				f.alignLoopHeader()
				want := (prefix + (1 << align) - 1) &^ ((1 << align) - 1)
				if got := f.a.Len(); got != want {
					t.Fatalf("align=%d prefix=%d interruptible=%t got=%d want=%d", align, prefix, interruptible, got, want)
				}
			}
		}
	}
}

func TestPollFreeLoopHeaderLimitsPaddingForLargeFunctions(t *testing.T) {
	f := fn{a: &a64.Asm{}, nLocals: pollFreeLoopPhaseMaxLocals + 1}
	f.a.B = make([]byte, 4)
	f.alignLoopHeader()
	if got, want := f.a.Len(), 16; got != want {
		t.Fatalf("large-function aligned length = %d, want %d", got, want)
	}
}

func TestScopedFloatConstantLoopHeaderUsesFullFetchBoundary(t *testing.T) {
	for _, prefix := range []int{0, 4, 12, 16, 20, 28} {
		f := fn{
			a:       &a64.Asm{},
			nLocals: 4,
			fconsts: []floatConstReg{{loopDepth: 1}},
		}
		f.a.B = make([]byte, prefix)
		f.alignLoopHeader()
		if got := f.a.Len() % 32; got != 0 {
			t.Fatalf("prefix %d: scoped float loop phase = %d, want 0", prefix, got)
		}
	}
}
