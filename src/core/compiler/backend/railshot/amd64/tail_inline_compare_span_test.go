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
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestInlineTailComparePreservedSpan(t *testing.T) {
	cases := []struct {
		name string
		code []byte
		ok   bool
	}{
		{"empty", nil, true},
		{"lea", []byte{0x41, 0x8d, 0x7b, 1}, true},
		{"lea64", []byte{0x49, 0x8d, 0x7b, 1}, true},
		{"scaled", []byte{0x4b, 0x8d, 0xbc, 0x53, 1, 2, 3, 4}, true},
		{"rip", []byte{0x48, 0x8d, 0x3d, 1, 2, 3, 4}, true},
		{"noBase", []byte{0x48, 0x8d, 0x3c, 0x25, 1, 2, 3, 4}, true},
		{"mov", []byte{0x48, 0x89, 0xc7, 0x8b, 0xf0}, true},
		{"writeLeft", []byte{0x44, 0x8d, 0x53, 1}, false},
		{"writeRight32", []byte{0x8d, 0x6b, 1}, false},
		{"movWriteLeft", []byte{0x41, 0x89, 0xc2}, false},
		{"movWriteRight", []byte{0x8b, 0xe8}, false},
		{"flags", []byte{0x83, 0xc7, 1}, false},
		{"load", []byte{0x8b, 0x3b}, false},
		{"store", []byte{0x89, 0x3b}, false},
		{"branch", []byte{0xeb, 0}, false},
		{"call", []byte{0xe8, 0, 0, 0, 0}, false},
		{"leaReg", []byte{0x8d, 0xc7}, false},
		{"prefix", []byte{0x66, 0x8d, 0x7b, 1}, false},
		{"shortSib", []byte{0x8d, 0x3c}, false},
		{"shortDisp", []byte{0x8d, 0x7b}, false},
		{"shortRip", []byte{0x8d, 0x3d, 0, 0, 0}, false},
		{"shortRex", []byte{0x48}, false},
		{"overBound", bytes.Repeat([]byte{0x89, 0xc7}, 9), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inlineTailComparePreserved(tc.code, maskOf(R10, RBP)); got != tc.ok {
				t.Fatalf("got %t want %t: %x", got, tc.ok, tc.code)
			}
		})
	}
}

func TestInlineTailCompareReuseWithoutConstantFacts(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, immediate := range []bool{false, true} {
			a := &encoder.Asm{}
			right := &elem{st: storage{kind: stLocalReg, reg: RBP}}
			if immediate {
				right.st = storage{kind: stConst, cval: 7}
				a.AluRI(cmpDigit, R10, 7, wide)
			} else if wide {
				a.Cmp64(R10, RBP)
			} else {
				a.Cmp32(R10, RBP)
			}
			end := a.Len()
			fr := ctrlFrame{controlSite: a.JccPlaceholder(encoder.CondNE)}
			f := fn{a: a, policy: currentCodegenPolicy(), inlineTailCompare: inlineTailCompare{active: true, lastCompare: 0, reuseResume: -1}}
			a.PatchRel32(fr.controlSite, a.Len())
			f.rememberInlineTailCompare(&fr, a.Len())
			a.LeaDispW(RDI, R11, 1, wide)
			if !f.tryReuseInlineTailCompare(R10, right, wide) {
				t.Fatalf("wide=%t immediate=%t no reuse", wide, immediate)
			}
			if f.tryReuseInlineTailCompare(R10, right, wide) {
				t.Fatal("reused a consumed edge")
			}
			want := maskOf(R10, RBP)
			if immediate {
				want = maskOf(R10)
			}
			if got := inlineTailCompareOperands(a.B[:end]); got != want {
				t.Fatalf("operands %x want %x", got, want)
			}
		}
	}
}

func TestInlineTailDynamicCompareReuse(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	results := []wasm.ValType{wasm.I32}
	params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
	body := []byte{0,
		0x20, 0, 0x45, 0x04, 0x40, 0x41, 5, 0x0f, 0x0b,
		0x20, 1, 0x20, 2, 0x46, 0x04, 0x40, 0x41, 11, 0x0f, 0x0b,
		0x20, 1, 0x20, 2, 0x49, 0x04, 0x40,
		0x20, 0, 0x41, 1, 0x6b, 0x20, 1, 0x41, 1, 0x6a, 0x20, 2, 0x12, 1,
		0x0b, 0x41, 5, 0x0b}
	m := modFuncs(t, funcDef{params, results, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, 0x0b}}, funcDef{params, results, body})
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
	if err != nil {
		t.Fatal(err)
	}
	if c.CodeImage != nil {
		defer c.CodeImage.Close()
	}
	for _, n := range []uint64{0, 1, 2, 8, 31} {
		for _, left := range []uint64{0, 1, 7, 0x7fffffff, 0x80000000, 0xffffffff} {
			for _, right := range []uint64{0, 1, 6, 8, 0x80000000, 0xffffffff} {
				x, want := uint32(left), uint64(5)
				for i := uint64(0); i < n; i++ {
					if x == uint32(right) {
						want = 11
						break
					}
					if x < uint32(right) {
						x++
					} else {
						break
					}
				}
				if got := runCompiledAmd64u(t, c, n, left|0xdeadbeef00000000, right|0xcafebabe00000000); got != want {
					t.Fatalf("n=%d left=%x right=%x got=%d want=%d", n, left, right, got, want)
				}
			}
		}
	}
	if diagnosticsEnabled && stats.Funcs[0].Peephole["inline-tail-compare-reuse"] == 0 {
		t.Fatalf("dynamic tail body did not reuse its comparison: %+v", stats.Funcs[0].Peephole)
	}
}

func TestInlineTailCompareReuseInvalidatedAtControlEntry(t *testing.T) {
	for _, kind := range []ctrlKind{cfBlock, cfLoop, cfIf} {
		f := fn{inlineTailCompare: inlineTailCompare{active: true, lastCompare: 0, reuseResume: 0}}
		f.pushCtrl(&ctrlFrame{kind: kind})
		if f.inlineTailCompare.reuseResume != -1 {
			t.Fatalf("kind=%v retained predecessor flags", kind)
		}
	}
}
