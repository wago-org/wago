// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64 && !tinygo

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/rand"
	"reflect"
	"testing"
)

func checkIterativeHintParity(t *testing.T, body []byte) {
	t.Helper()
	makeScanner := func() byteBodyScanner {
		h := newFuncHints(8, 4)
		h.localEvents = &shared.LocalEventTape{}
		elig := newGlobalEligibilityTracker(4)
		accum := new(shared.GlobalHintAccumulator)
		accum.Reset(4)
		return byteBodyScanner{r: byteScanReader{Reader: wasm.ReaderFrom(body)}, h: h, nLocals: 8, nGlobals: 4, elig: &elig, globalHints: accum, entryPrefix: true, classifier: wasm.NewModuleInstructionClassifier(nil, true), collectLoopIntConsts: true}
	}
	a, b := makeScanner(), makeScanner()
	ac, at, ae := a.scanExpr(0, 0, -1, false)
	bc, bt, be := b.scanExprRecursive(0, 0, -1, false)
	a.h = finishGlobalHints(a.h, a.globalHints)
	b.h = finishGlobalHints(b.h, b.globalHints)
	if ac != bc || at != bt || a.r.off() != b.r.off() || !reflect.DeepEqual(ae, be) || !reflect.DeepEqual(a.h, b.h) || !reflect.DeepEqual(a.elig, b.elig) || a.entryPrefix != b.entryPrefix || a.entrySeen != b.entrySeen || a.loopIntConsts != b.loopIntConsts || a.loopIntConstN != b.loopIntConstN {
		t.Fatalf("body len=%d hints differ at offsets%d/%d terms%x/%x calls%v/%v errors%v/%v\n%+v\n%+v", len(body), a.r.off(), b.r.off(), at, bt, ac, bc, ae, be, a.h, b.h)
	}
	var as, bs [4]int64
	ga := globalScoreByteScanner{r: byteScanReader{Reader: wasm.ReaderFrom(body)}, nGlobals: 4, add: func(g uint32, v int64) { as[g] += v }, classifier: wasm.NewModuleInstructionClassifier(nil, true)}
	gb := globalScoreByteScanner{r: byteScanReader{Reader: wasm.ReaderFrom(body)}, nGlobals: 4, add: func(g uint32, v int64) { bs[g] += v }, classifier: wasm.NewModuleInstructionClassifier(nil, true)}
	at, ae = ga.scanExpr(0, 0, false)
	bt, be = gb.scanExprRecursive(0, 0, false)
	if at != bt || ga.r.off() != gb.r.off() || !reflect.DeepEqual(ae, be) || as != bs {
		t.Fatalf("global score parity len=%d offsets%d/%d terms%x/%x errors%v/%v scores%v/%v", len(body), ga.r.off(), gb.r.off(), at, bt, ae, be, as, bs)
	}
}

func TestIterativeHintsRecursiveParity(t *testing.T) {
	rng := rand.New(rand.NewSource(713))
	var expr func(int) []byte
	expr = func(depth int) []byte {
		var b []byte
		for i := 0; i < 1+rng.Intn(5); i++ {
			switch rng.Intn(10) {
			case 0, 1, 2, 3:
				if depth < 5 {
					op := []byte{2, 3, 4, 0x1f}[rng.Intn(4)]
					b = append(b, op, 0x40)
					if op == 0x1f {
						b = append(b, 0)
					}
					b = append(b, expr(depth+1)...)
					if op == 4 && rng.Intn(2) == 0 {
						b = append(b, 5)
						b = append(b, expr(depth+1)...)
					}
					b = append(b, 0x0b)
				}
			case 4:
				b = append(b, 0x20, byte(rng.Intn(8)), 0x74)
			case 5:
				b = append(b, 0x23, byte(rng.Intn(4)))
			case 6:
				b = append(b, 0x24, byte(rng.Intn(4)))
			case 7:
				b = append(b, 0x10, 0)
			case 8:
				b = append(b, 0x41, byte(rng.Intn(128)), 0x20, 0, 0x74)
			case 9:
				b = append(b, 0x01)
			}
		}
		return b
	}
	for i := 0; i < 1000; i++ {
		b := append(expr(0), 0x0b)
		checkIterativeHintParity(t, b)
		if len(b) > 1 {
			checkIterativeHintParity(t, b[:rng.Intn(len(b))])
		}
	}
	for _, b := range [][]byte{nil, {5}, {2, 0x40, 5}, {4, 0x40, 5, 5}, {3, 0x40, 0x23, 0, 0x10, 0, 0x0b, 0x0b}, {0x1f, 0x40, 0, 0x0b, 0x0b}} {
		checkIterativeHintParity(t, b)
	}
}

func TestIterativeHintsNestingBoundary(t *testing.T) {
	for _, n := range []int{16, 17, 255, 20000, 20001} {
		b := make([]byte, 0, 3*n+1)
		for i := 0; i < n; i++ {
			b = append(b, 2, 0x40)
		}
		for i := 0; i <= n; i++ {
			b = append(b, 0x0b)
		}
		checkIterativeHintParity(t, b)
	}
}
