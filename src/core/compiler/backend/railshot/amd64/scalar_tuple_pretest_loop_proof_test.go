// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestTupleLoopExitStoresRejectsOtherMemory(t *testing.T) {
	for _, offset := range []int32{0, 120, 128, 4096} {
		encode := func(base Reg, shift int32, wide bool) []byte {
			var a x64.Asm
			if wide {
				a.Store64(base, offset+shift, R9)
				a.Store64(base, offset+shift+8, R10)
			} else {
				a.Store32(base, offset+shift, R9)
				a.Store32(base, offset+shift+8, R10)
			}
			return a.B
		}
		pins := maskOf(R9, R10)
		valid := encode(RSP, 0, true)
		if !tupleLoopExitStores(valid, offset, pins) {
			t.Fatalf("valid stores at %d rejected", offset)
		}
		for _, invalid := range [][]byte{encode(R12, 0, true), encode(RSP, 8, true), encode(RSP, 0, false), append(append([]byte{}, valid...), 0x90)} {
			if tupleLoopExitStores(invalid, offset, pins) {
				t.Fatalf("offset=%d accepted other memory: %x", offset, invalid)
			}
		}
		if tupleLoopExitStores(valid, offset, maskOf(R9)) {
			t.Fatal("accepted an unpinned source")
		}
		for n := 0; n < len(valid); n++ {
			if tupleLoopExitStores(valid[:n], offset, pins) {
				t.Fatalf("accepted truncated stores: %x", valid[:n])
			}
		}
	}
}
