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

func regionFactsTestFunction() fn {
	f := fn{nLocals: 4, pinnedLocals: []int{0, 2}, locals: make([]localDef, 4), localType: []machineType{mtI32, mtI32, mtI32, mtI32}}
	for i := range f.locals {
		f.locals[i].state = lsMem
		f.locals[i].reg = regNone
	}
	f.locals[0].reg = R8
	f.locals[2].reg = R9
	return f
}

func TestLoopRegionFactsDifferential(t *testing.T) {
	fixtures := [][]byte{
		{0x20, 1, 0x20, 1, 0x20, 1, 0x21, 1, 0x0d, 0, 0x0b},
		{0x43, 0, 0, 0, 0, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x43, 0, 0, 0, 0, 0x0d, 0, 0x0b},
		{0x02, 0x40, 0x0b, 0x0b},       // broader call-free language
		{0x10, 0, 0x0b},                // call
		{0x20, 1, 0x0d, 1, 0x0b},       // nonlocal backedge
		{0x20, 1, 0x0d, 0, 0x01, 0x0b}, // work after backedge
		{0x41, 0x80},                   // malformed immediate
		append(bytes.Repeat([]byte{0x01}, 94), 0x0d, 0, 0x0b),
		append(bytes.Repeat([]byte{0x01}, 95), 0x0d, 0, 0x0b),
	}
	// Vary writes, literal identities, and scanner-budget cutoffs.
	for local := byte(0); local < 4; local++ {
		for count := 0; count < 24; count++ {
			body := []byte{0x21, local}
			for i := 0; i < count; i++ {
				body = append(body, 0x43, byte(i%17), 0, 0, 0)
			}
			fixtures = append(fixtures, append(body, 0x0d, 0, 0x0b))
		}
	}
	for n, body := range fixtures {
		f := regionFactsTestFunction()
		r := wasm.ReaderFrom(body)
		var facts loopRegionFacts
		p, w := f.planLoopPinExchangeFacts(&r, &facts)
		p0, w0 := f.planLoopPinExchange(&r)
		if p != p0 || w != w0 {
			t.Fatalf("case %d pin plan changed", n)
		}
		for _, want := range []bool{false, true} {
			for _, initial := range []int{0, maxCallFreeLoopLookaheadBytes - len(body), maxCallFreeLoopLookaheadBytes - len(body) + 1, maxCallFreeLoopLookaheadBytes} {
				a, b := initial, initial
				ok, wr, st, has := scanLoopCallFreeRegion(&r, f.classifier, false, f.pinnedLocals, want, &a, &facts)
				ok0, wr0, st0, has0 := scanLoopCallFreeDetails(&r, f.classifier, false, f.pinnedLocals, want, &b)
				if ok != ok0 || wr != wr0 || st != st0 || has != has0 || a != b {
					t.Fatalf("case %d wantFloat=%v initial=%d mismatch: %v/%x/%v/%v/%d vs %v/%x/%v/%v/%d", n, want, initial, ok, wr, st, has, a, ok0, wr0, st0, has0, b)
				}
			}
		}
	}
}

func BenchmarkLoopRegionFacts(b *testing.B) {
	body := append(bytes.Repeat([]byte{0x20, 1, 0x41, 1, 0x6a, 0x21, 1}, 16), 0x0d, 0, 0x0b)
	for _, shared := range []bool{false, true} {
		name := "independent"
		if shared {
			name = "shared"
		}
		b.Run(name, func(b *testing.B) {
			f := regionFactsTestFunction()
			r := wasm.ReaderFrom(body)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				budget := 0
				var facts loopRegionFacts
				var ptr *loopRegionFacts
				if shared {
					ptr = &facts
				}
				f.planLoopPinExchangeFacts(&r, ptr)
				scanLoopCallFreeRegion(&r, f.classifier, false, f.pinnedLocals, true, &budget, &facts)
			}
		})
	}
}
