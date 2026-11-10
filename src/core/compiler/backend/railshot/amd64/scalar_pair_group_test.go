// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func TestScalarSumSwapGroupProvesTemporaryAndWidth(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, regs := range [][3]Reg{{R9, R10, RDI}, {RAX, RDX, R11}, {RBP, R15, R12}, {R13, R8, RBP}} {
			for _, fromA := range []bool{false, true} {
				a, b, temp := regs[0], regs[1], regs[2]
				var body x64.Asm
				mov := func(dst, src Reg) {
					if wide {
						body.MovReg64(dst, src)
					} else {
						body.MovRegReg32(dst, src)
					}
				}
				if fromA {
					mov(temp, a)
					mov(a, b)
					body.AluRR(0x01, b, temp, wide)
				} else {
					mov(temp, b)
					body.AluRR(0x01, b, a, wide)
					mov(a, temp)
				}
				pins := maskOf(a, b, temp)
				got, ok := planScalarSumSwapGroup(body.B, pins)
				if !ok || got != (scalarSumSwapGroup{a, b, temp, wide, fromA}) {
					t.Fatalf("regs=%v wide=%t tempFromA=%t got=%+v ok=%t bytes=%x", regs, wide, fromA, got, ok, body.B)
				}
				for n := 0; n < len(body.B); n++ {
					if _, ok := planScalarSumSwapGroup(body.B[:n], pins); ok {
						t.Fatalf("accepted partial recurrence=%x", body.B[:n])
					}
				}
				if _, ok := planScalarSumSwapGroup(append(append([]byte{}, body.B...), 0x90), pins); ok {
					t.Fatal("accepted trailing unmodeled instruction")
				}
				if _, ok := planScalarSumSwapGroup(body.B, 0); ok {
					t.Fatal("accepted unpinned pair state")
				}
			}
		}
	}
	// The opposite ModRM direction is a distinct valid encoder form.
	if g, ok := planScalarSumSwapGroup([]byte{0x8b, 0xfa, 0x03, 0xd0, 0x8b, 0xc7}, maskOf(RAX, RDX)); !ok || g.a != RAX || g.b != RDX || g.temp != RDI || g.wide || g.tempFromA {
		t.Fatalf("reverse ModRM direction: %+v ok=%t", g, ok)
	}
	for _, body := range [][]byte{
		{0x89, 0xd7, 0x29, 0xc2, 0x89, 0xf8},       // subtraction is not this recurrence
		{0x89, 0xd7, 0x01, 0xc2, 0x48, 0x89, 0xf8}, // mixed widths
		{0x89, 0xd7, 0x01, 0xfa, 0x89, 0xf8},       // doubles b instead of adding a
		{0x89, 0xc7, 0x01, 0xc2, 0x89, 0xf8},       // final a is old a, not old b
		{0x89, 0xd7, 0x11, 0xc2, 0x89, 0xf8},       // ADC observes incoming flags
		{0x89, 0xe7, 0x01, 0xc2, 0x89, 0xf8},       // reads the stack pointer
	} {
		if _, ok := planScalarSumSwapGroup(body, maskOf(RAX, RDX, RDI)); ok {
			t.Fatalf("accepted unsupported register transfer=%x", body)
		}
	}

}

func TestScalarSumSwapGroupAdmissionAndRollback(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, eqz, sub, add, xor := wasm.I32, byte(0x41), byte(0x45), byte(0x6b), byte(0x6a), byte(0x73)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, k, eqz, sub, add, xor = wasm.I64, 0x42, 0x50, 0x7d, 0x7c, 0x85
			fold = func(x uint64) uint64 { return x }
		}
		body := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x03, 1,
			0x21, 2, 0x21, 1, 0x21, 0, 0x20, 0, eqz, 0x04, 2,
			0x20, 1, 0x20, 2, 0x05,
			0x20, 0, k, 1, sub, 0x20, 2, 0x20, 1, 0x20, 2, add,
			0x0c, 1, 0x0b, 0x0b, xor, 0x0b}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, body},
			funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, []byte{0, 0x20, 1, 0x20, 2, 0x0b}},
			funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
		a, b := uint64(3), uint64(5)
		for i := 0; i < 17; i++ {
			a, b = b, fold(a+b)
		}
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"inline": false, "scalar-loop-unroll": enabled, "prepared-direct-entry": false, "value-facts": false}}
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				if got := runCompiledAmd64u(t, cm, 17, 3, 5); got != a^b {
					t.Fatalf("wide=%t enabled=%t workers=%d got=%x want=%x", wide, enabled, workers, got, a^b)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-tuple-sum-swap-group4"] != 0) != (enabled && !profileEnabled) {
					t.Fatalf("wide=%t enabled=%t workers=%d admission=%v", wide, enabled, workers, stats.Funcs[0].Peephole)
				}
				if cm.CodeImage != nil {
					cm.CodeImage.Close()
				}
			}
		}
	}
}

func TestScalarSumSwapGroupPreservesAllNativeRegisters(t *testing.T) {
	for _, wide := range []bool{false, true} {
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			fold = func(x uint64) uint64 { return x }
		}
		for _, regs := range [][3]Reg{{R9, R10, RDI}, {RAX, RDX, R11}, {RBP, R15, R12}, {R13, R8, RBP}} {
			for _, fromA := range []bool{false, true} {
				g := scalarSumSwapGroup{regs[0], regs[1], regs[2], wide, fromA}
				for output, resultReg := range regs {
					var a x64.Asm
					a.Load64(g.a, RDI, 0)
					a.Load64(g.b, RDI, 8)
					a.Load64(g.temp, RDI, 16) // RDI may itself be the temporary.
					f := fn{a: &a}
					f.emitScalarSumSwapGroup(g)
					a.Store64(RCX, 0, resultReg)
					a.Ret()
					cm := x64.CompiledModule{Code: a.B, Entry: []int{0}}
					for _, seed := range [][3]uint64{{0, 1, 7}, {0xfffffff0, 0x80000001, ^uint64(0)}, {0x8000000000000001, ^uint64(0), 17}} {
						x, y, temp := fold(seed[0]), fold(seed[1]), fold(seed[2])
						for i := 0; i < 4; i++ {
							temp = y
							if fromA {
								temp = x
							}
							x, y = y, fold(x+y)
						}
						want := [3]uint64{x, y, temp}[output]
						if got := runCompiledAmd64u(t, &cm, seed[:]...); got != want {
							t.Fatalf("wide=%t regs=%v tempFromA=%t output=%d seed=%x got=%x want=%x", wide, regs, fromA, output, seed, got, want)
						}
					}
				}
			}
		}
	}
}

func TestScalarSumSwapGroupPreservesObservableTemporary(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, k, eqz, sub, add, xor := wasm.I32, byte(0x41), byte(0x45), byte(0x6b), byte(0x6a), byte(0x73)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, k, eqz, sub, add, xor = wasm.I64, 0x42, 0x50, 0x7d, 0x7c, 0x85
			fold = func(x uint64) uint64 { return x }
		}
		for _, fromA := range []bool{false, true} {
			source := byte(2)
			left, right := byte(1), byte(3)
			if fromA {
				source, left, right = 1, 3, 2
			}
			body := []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x03, 1,
				0x21, 2, 0x21, 1, 0x21, 0, 0x20, 0, eqz, 0x04, 2,
				0x20, 1, 0x20, 2, 0x05,
				0x20, source, 0x21, 3,
				0x20, 0, k, 1, sub, 0x20, 2, 0x20, left, 0x20, right, add,
				0x0c, 1, 0x0b, 0x0b, xor, 0x20, 3, xor, 0x0b}
			m := modFuncs(t,
				funcDef{[]wasm.ValType{typ, typ, typ, typ}, []wasm.ValType{typ}, body},
				funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, []byte{0, 0x20, 1, 0x20, 2, 0x0b}},
				funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					var stats ModuleStats
					opts := CompileOptions{Workers: workers, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"inline": false, "scalar-loop-unroll": enabled, "prepared-direct-entry": false, "value-facts": false}}
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					for _, count := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 17, 31, 64, 79, 97} {
						for _, seed := range [][3]uint64{{0, 1, 7}, {0xfffffff0, 0x80000001, ^uint64(0)}, {0x8000000000000001, ^uint64(0), 17}} {
							a, b, temp := fold(seed[0]), fold(seed[1]), fold(seed[2])
							for n := uint64(0); n < count; n++ {
								temp = b
								if fromA {
									temp = a
								}
								a, b = b, fold(a+b)
							}
							argCount := count
							if !wide {
								argCount |= 0xdeadbeef00000000
							}
							if got := runCompiledAmd64u(t, cm, argCount, seed[0], seed[1], seed[2]); got != a^b^temp {
								t.Fatalf("wide=%t tempFromA=%t enabled=%t workers=%d count=%d seed=%x got=%x want=%x", wide, fromA, enabled, workers, count, seed, got, a^b^temp)
							}
						}
					}
					if diagnosticsEnabled && (!enabled || profileEnabled) && stats.Funcs[0].Peephole["scalar-tuple-sum-swap-group4"] != 0 {
						t.Fatal("sum/swap reduction bypassed optimization/profile gate")
					}
					if cm.CodeImage != nil {
						cm.CodeImage.Close()
					}
				}
			}
		}
	}
}
