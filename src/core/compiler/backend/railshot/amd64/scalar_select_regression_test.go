// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func unpredictableScalarSelectModule(t testing.TB) *wasm.Module {
	body := []byte{1, 1, 0x7f, 0x03, 0x40,
		0x20, 2, 0x20, 0, 0x41, 0, 0x48, 0x04, 0x7f,
		0x20, 0, 0x41, 3, 0x6c, 0x05, 0x20, 0, 0x41, 7, 0x6a, 0x0b, 0x6a, 0x21, 2,
		0x20, 0, 0x41, 5, 0x6c, 0x41, 1, 0x6a, 0x21, 0,
		0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x41, 0, 0x47, 0x0d, 0, 0x0b, 0x20, 2, 0x0b}
	return modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body})
}

func TestScalarLoopUnpredictableSelectKeepsCmovByDefault(t *testing.T) {
	saved := pureSelectEnabled
	pureSelectEnabled = false
	defer func() { pureSelectEnabled = saved }()
	m := unpredictableScalarSelectModule(t)
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"scalar-loop-unroll": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		if c.CodeImage != nil {
			defer c.CodeImage.Close()
		}
		if diagnosticsEnabled {
			p := stats.Funcs[0].Peephole
			want := enabled && !profileEnabled && (nativeCompactionDisabled || !nativeCompactionEnabled)
			if (p["scalar-cmov-unroll4"] != 0) != want || p["scalar-select-branch-unroll4"] != 0 || p["scalar-lazy-select-unroll4"] != 0 {
				t.Fatalf("enabled=%t default selection policy: %v", enabled, p)
			}
		}
		for _, count := range []uint32{1, 2, 3, 4, 5, 31, 10000} {
			seed, sum := uint32(13), uint32(0)
			for i := uint32(0); i < count; i++ {
				v := seed + 7
				if int32(seed) < 0 {
					v = seed * 3
				}
				sum += v
				seed = seed*5 + 1
			}
			got := runCompiledAmd64u(t, c, 0xdeadbeef0000000d, uint64(count)|0xcafebabe00000000)
			if got != uint64(sum) {
				t.Fatalf("enabled=%t count=%d got=%x want=%x", enabled, count, got, sum)
			}
		}
	}
}

func TestScalarSelectFallbackRequiresOptIn(t *testing.T) {
	if profileEnabled {
		t.Skip("unrolling excludes profiling")
	}
	saved := pureSelectEnabled
	defer func() { pureSelectEnabled = saved }()
	body := []byte{0x85, 0xff, 0x0f, 0x44, 0xc6} // TEST; CMOVE, with no lazy arms.
	var out [128]byte
	if _, ok := scalarLazySelectBranchBody(body, out[:], 0); ok {
		t.Fatal("fixture unexpectedly uses lazy-arm path")
	}
	if _, ok := scalarSelectBranchBody(body, out[:]); !ok {
		t.Fatal("fixture does not exercise CMOV fallback")
	}
	for _, optIn := range []bool{false, true} {
		pureSelectEnabled = optIn
		var stats CodegenStats
		f := &fn{a: &x64.Asm{B: append([]byte{}, body...)}, policy: currentCodegenPolicy(), ctrl: []ctrlFrame{{kind: cfLoop}}, stats: &stats}
		if f.compactNative() {
			t.Skip("short branches exclude compact finalization")
		}
		got := f.tryScalarPosttestLoop(&f.ctrl[0], 0, 0, condNE)
		if !got {
			t.Fatalf("opt-in=%t admitted=%t", optIn, got)
		}
		if !optIn {
			for phase := 0; phase < 4; phase++ {
				at := phase * (len(body) + 6)
				if !bytes.Equal(body, f.a.B[at:at+len(body)]) {
					t.Fatal("default unrolling rewrote CMOV")
				}
			}
		}
		if diagnosticsEnabled && (stats.Peephole["scalar-select-branch-unroll4"] != 0) != optIn {
			t.Fatal("CMOV fallback branch conversion ignored opt-in")
		}
		if diagnosticsEnabled && stats.Peephole["scalar-lazy-select-unroll4"] != 0 {
			t.Fatal("fixture used lazy-arm path")
		}
	}
}
