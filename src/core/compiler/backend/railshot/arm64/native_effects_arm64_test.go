// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func TestNativeExitClobberProof(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*enc.Asm)
		want bool
	}{
		{"scratch dies at return", func(a *enc.Asm) { a.Ret() }, true},
		{"value returned", func(a *enc.Asm) { a.MovReg64(X0, X10); a.Ret() }, false},
		{"value stored", func(a *enc.Asm) { a.Store64(X10, linMemReg, 0); a.Ret() }, false},
		{"value overwritten", func(a *enc.Asm) { a.MovImm64(X10, 0); a.MovReg64(X0, X10); a.Ret() }, true},
		{"one branch uses value", func(a *enc.Asm) {
			b := a.Cbnz32(X1)
			a.MovImm64(X10, 0)
			a.Ret()
			alt := a.Len()
			a.MovReg64(X0, X10)
			a.Ret()
			a.PatchBranch19(b, alt)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &enc.Asm{}
			tc.emit(a)
			if got := spanExitClobbersDeadWith(a.B, 0, 1<<uint32(X10), nil, linearSpanExitWord); got != tc.want {
				t.Fatalf("proof = %v, want %v", got, tc.want)
			}
		})
	}
}
