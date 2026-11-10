// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !wago_regalloccheck

package amd64

import (
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestRegallocCheckOrdinaryFlushAllocations(t *testing.T) {
	f := fn{a: &encoder.Asm{B: make([]byte, 0, 4096)}, s: newStack()}
	emit := func() {
		f.a.B = f.a.B[:0]
		f.s.reset()
		for i := 0; i < 24; i++ {
			f.pushValue(storage{kind: stConst, typ: mtI64, cval: int64(i + 1)})
		}
		f.flush()
		f.moveSlots(4, 0, 20)
	}
	emit()
	if got := testing.AllocsPerRun(100, emit); got != 0 {
		t.Fatalf("ordinary warmed flush allocated %g times, want zero", got)
	}
}
