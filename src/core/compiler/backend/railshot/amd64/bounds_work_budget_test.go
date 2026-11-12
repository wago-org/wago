// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBoundsLookaheadWorkBudget(t *testing.T) {
	data := make([]byte, maxStraightLineRangeOps)
	for i := range data {
		data[i] = 0x01
	}
	f := fn{}
	for i := 0; i < 16; i++ {
		r := wasm.ReaderFrom(data)
		if got := f.straightLineLoadExtent(&r, 1, 0, 4); got != 4 || r.Offset() != 0 {
			t.Fatalf("probe %d: extent=%d offset=%d", i, got, r.Offset())
		}
	}
	if f.boundsScanWork != maxFunctionRangeOps {
		t.Fatalf("work=%d, want %d", f.boundsScanWork, maxFunctionRangeOps)
	}
	r := wasm.ReaderFrom([]byte{0x20, 0, 0x28, 0, 16})
	if got := f.straightLineLoadExtent(&r, 1, 0, 4); got != 4 {
		t.Fatalf("exhausted probe changed extent: %d", got)
	}
}
