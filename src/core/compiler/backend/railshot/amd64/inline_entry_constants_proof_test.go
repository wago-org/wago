// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import "testing"

func TestInlineEntryConstantsStopBeforeReadsAndControl(t *testing.T) {
	types := []machineType{mtI32, mtI32, mtI64}
	for _, tc := range []struct {
		name string
		body []byte
		want uint64
	}{
		{"typed-pair", []byte{0x41, 17, 0x21, 1, 0x42, 1, 0x21, 2, 0x0b}, 6},
		{"read-first", []byte{0x20, 1, 0x41, 17, 0x21, 1}, 0},
		{"control-first", []byte{0x02, 0x40, 0x41, 17, 0x21, 1}, 0},
		{"stop-at-control", []byte{0x41, 17, 0x21, 1, 0x02, 0x40, 0x42, 1, 0x21, 2}, 2},
		{"stop-at-call", []byte{0x41, 17, 0x21, 1, 0x10, 0, 0x42, 1, 0x21, 2}, 2},
		{"wrong-width", []byte{0x42, 17, 0x21, 1}, 0},
		{"parameter-then-local", []byte{0x41, 3, 0x21, 0, 0x41, 17, 0x21, 1}, 2},
		{"tee-and-nop", []byte{0x01, 0x41, 17, 0x22, 1, 0x01, 0x42, 1, 0x21, 2}, 6},
		{"truncated-constant", []byte{0x41, 0x80}, 0},
		{"truncated-index", []byte{0x41, 17, 0x21, 0x80}, 0},
		{"outside-locals", []byte{0x41, 17, 0x21, 3}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inlineEntryConstantLocals(tc.body, 1, types); got != tc.want {
				t.Fatalf("got=%x want=%x", got, tc.want)
			}
		})
	}
}
