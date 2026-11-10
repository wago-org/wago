// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package shared

import "testing"

func TestGCDispatchReservedBits(t *testing.T) {
	const maximum = (AtomicWaitDispatchBit - 1) >> GCSafepointIDShift
	if GCSafepointIDMax != maximum {
		t.Errorf("maximum=%d, want %d", GCSafepointIDMax, maximum)
	}
	for _, id := range []uint32{0, 1, maximum} {
		payload, ok := EncodeGCDispatch(GCHelperIDMask, id)
		if !ok {
			t.Fatalf("valid safepoint %d rejected", id)
		}
		if payload&(uint32(7)<<29) != 0 {
			t.Fatalf("reserved tags set: %#x", payload)
		}
		helper, got := DecodeGCDispatch(payload)
		if helper != GCHelperIDMask || got != id {
			t.Fatalf("decode=(%d,%d)", helper, got)
		}
	}
	for _, id := range []uint32{maximum + 1, 1 << 22, 1 << 23, ^uint32(0)} {
		if payload, ok := EncodeGCDispatch(0, id); ok {
			t.Errorf("unsafe ID %d encoded as %#x", id, payload)
		}
	}
	if _, ok := EncodeGCDispatch(GCHelperIDMask+1, 0); ok {
		t.Fatal("oversized helper accepted")
	}
}
