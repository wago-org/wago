// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRotateByteFilterPreservesThreshold(t *testing.T) {
	saved := rotateByteFilterEnabled
	defer func() { rotateByteFilterEnabled = saved }()
	for _, count := range []int{0, 1, 127, 128, 129, 256} {
		for _, op := range []byte{0x77, 0x78, 0x89, 0x8a} {
			for _, fake := range []bool{false, true} {
				unit := []byte{op}
				if fake {
					// These rotate-looking bytes are inside signed integer immediates.
					unit = []byte{0x41, op, 0}
				}
				body := append(bytes.Repeat(unit, count), 0x0b)
				rotateByteFilterEnabled = false
				want := denseRotateBody(body, wasm.ModuleInstructionClassifier{})
				if want != (!fake && count >= denseRorxOpCrossover) {
					t.Fatalf("unexpected reference result: count=%d op=%x immediate=%t result=%t", count, op, fake, want)
				}
				rotateByteFilterEnabled = true
				if got := denseRotateBody(body, wasm.ModuleInstructionClassifier{}); got != want {
					t.Fatalf("count=%d op=%x immediate=%t got=%t want=%t", count, op, fake, got, want)
				}
			}
		}
	}
}
