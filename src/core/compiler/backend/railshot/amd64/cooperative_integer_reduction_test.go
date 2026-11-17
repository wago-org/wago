// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && wago_experimental_cooperative_integer_reduction

package amd64

import (
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestCooperativeIntegerReductionResults(t *testing.T) {
	old, enabled := cooperativeIntegerReductionEnabled, integerReductionLoopEnabled
	defer func() { cooperativeIntegerReductionEnabled, integerReductionLoopEnabled = old, enabled }()
	integerReductionLoopEnabled = true
	for _, signed := range []bool{false, true} {
		for result := uint32(0); result < 5; result++ {
			m := integerReductionFixture(t, signed, result)
			cooperativeIntegerReductionEnabled = false
			scalar, err := CompileModuleWith(m, CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: shared.AMD64SSE41})
			if err != nil {
				t.Fatal(err)
			}
			cooperativeIntegerReductionEnabled = true
			var stats ModuleStats
			vector, err := CompileModuleWith(m, CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: shared.AMD64SSE41, Stats: optionalTestStats(&stats)})
			if err != nil {
				t.Fatal(err)
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["cooperative-integer-reduction"] != 1 {
				t.Fatal("not selected", stats.Funcs[0].Peephole)
			}
			for _, start := range []uint64{0, 0x7ffffff0, 0xfffffff0} {
				for _, n := range []uint64{0, 1, 2, 3, 4, 7, 15, 16, 17, 31, 32, 33, 34, 35, 63, 64, 65, 129} {
					want := runCompiledAmd64u(t, scalar, n, start, ^uint64(0)-17)
					got := runCompiledAmd64u(t, vector, n, start, ^uint64(0)-17)
					if got != want {
						t.Fatal(signed, result, start, n, got, want)
					}
				}
			}
			scalar.CodeImage.Close()
			vector.CodeImage.Close()
		}
	}
}

func TestCooperativeIntegerReductionPolls(t *testing.T) {
	old, enabled := cooperativeIntegerReductionEnabled, integerReductionLoopEnabled
	defer func() { cooperativeIntegerReductionEnabled, integerReductionLoopEnabled = old, enabled }()
	cooperativeIntegerReductionEnabled, integerReductionLoopEnabled = true, true
	cm, err := CompileModuleWith(integerReductionFixture(t, false, 2), CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: shared.AMD64SSE41})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	sites := cooperativePollSites(cm.Code)
	if len(sites) != 4 {
		t.Fatalf("want entry/header/scalar fallback/packet polls, got %v", sites)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	args, trap, results, count := arena.Alloc(24), arena.Alloc(runtime.TrapBufferBytes), arena.Alloc(8), arena.Alloc(8)
	for _, tc := range []struct {
		n            uint64
		site, visits int
	}{{0, 0, 1}, {1, 1, 1}, {1, 2, 1}, {15, 2, 15}, {16, 3, 8}, {17, 3, 8}, {17, 2, 1}, {32, 3, 16}} {
		for _, visit := range []int{1, tc.visits, tc.visits + 1} {
			clear(args)
			clear(trap)
			clear(results)
			clear(count)
			binary.LittleEndian.PutUint64(args, tc.n)
			code := cooperativeDelayedPollCode(t, cm.Code, tc.site, count, visit)
			mapping, entry, e := runtime.MapCode(code)
			if e != nil {
				t.Fatal(e)
			}
			e = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
			runtime.Unmap(mapping)
			if visit <= tc.visits {
				// Multiple same-function interrupt sites share a trap stub, so
				// the existing ABI intentionally reports no exact source PC.
				var te *runtime.TrapError
				if !errors.As(e, &te) || te.Code != runtime.TrapInterrupted {
					t.Fatalf("n%d site%d visit%d: %v", tc.n, tc.site, visit, e)
				}
			} else if e != nil {
				t.Fatal(e)
			} else if got, want := binary.LittleEndian.Uint64(results), tc.n+21*tc.n*(tc.n-1)/2; got != want {
				t.Fatalf("successful patched path n%d result%d want%d", tc.n, got, want)
			}
			want := visit
			if want > tc.visits {
				want = tc.visits
			}
			if got := binary.LittleEndian.Uint32(count); got != uint32(want) {
				t.Fatalf("n%d site%d count%d want%d", tc.n, tc.site, got, want)
			}
		}
	}
}

func TestCooperativeIntegerReductionOriginalArtifact(t *testing.T) {
	old := cooperativeIntegerReductionEnabled
	defer func() { cooperativeIntegerReductionEnabled = old }()
	m, err := wasm.DecodeModule(integerReductionOriginal)
	if err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{false, true} {
		cooperativeIntegerReductionEnabled = on
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Interruptible: true, AMD64FeaturesSet: true, AMD64Features: shared.AMD64SSE41, Stats: optionalTestStats(&stats)})
		if err != nil {
			t.Fatal(err)
		}
		if on && diagnosticsEnabled && stats.Funcs[0].Peephole["cooperative-integer-reduction"] != 1 {
			t.Fatal("original artifact not selected", stats.Funcs[0].Peephole)
		}
		if got := runCompiledAmd64u(t, cm, 4096); got != 4156416353 {
			t.Fatal(on, got)
		}
		cm.CodeImage.Close()
	}
}
