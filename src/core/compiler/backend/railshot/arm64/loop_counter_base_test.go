// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func counterBaseFixture(wide bool) *fn {
	f := gpClassEmitter()
	f.policy, f.hasLoop = currentCodegenPolicy(), true
	f.loopSeedSite = 4
	f.pinnedLocalMask = regMask(0).add(X1).add(X2).add(X3)
	f.ctrl = []ctrlFrame{{kind: cfBlock}, {kind: cfLoop, controlSite: 4}}
	sf := uint32(0)
	if wide {
		sf = 1 << 31
	}
	for _, word := range []uint32{nopWord, sf | 0x1b017c29, sf | 0x6b02013f, 0x54000008, 0x34000001,
		sf | 0x1ac1084a, sf | 0x1b018949, 0x350000c9, sf | 0x1b017c2a, sf | 0x6b02015f,
		0x1a9f07e9, sf | 0x11000529, sf | 0x0b090063, sf | 0x11000421} {
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
	}
	f.appendFrameEnd(&f.ctrl[0], 12, true)
	f.sc.trapSites[trapDivZero] = []trapSite{{branch: 16, function: 7, pc: 113}}
	return f
}

func TestScalarBranchLoopCounterBase(t *testing.T) {
	if profileEnabled {
		t.Skip("source profiling disables copying")
	}
	for _, wide := range []bool{false, true} {
		f := counterBaseFixture(wide)
		if !f.unrollScalarBranches(&f.ctrl[1]) {
			t.Fatal("loop rejected")
		}
		seed := rdWord(f.a.B, 0)
		sf := uint32(0)
		if wide {
			sf = 1 << 31
		}
		if seed != sf|0x2a0103eb {
			t.Fatalf("seed=%08x, want MOV base,counter", seed)
		}
		if len(f.a.B) != 216 {
			t.Fatalf("code grew to %d", len(f.a.B))
		}
		for phase := 0; phase < 4; phase++ {
			at := 4 + 52*phase + 48
			if w := rdWord(f.a.B, at); w != sf|0x11000161|uint32(phase+1)<<10 {
				t.Fatalf("phase %d tail=%08x", phase, w)
			}
			trap := f.sc.trapSites[trapDivZero][phase]
			if trap.branch != uint32(16+52*phase) || trap.function != 7 || trap.pc != 113 {
				t.Fatalf("trap changed: %+v", trap)
			}
		}
		if target, ok := branchTarget(212, rdWord(f.a.B, 212)); !ok || target != 0 {
			t.Fatalf("backedge=%d %v", target, ok)
		}
	}
}

func TestLoopCounterBaseRejectsUnsafeShapes(t *testing.T) {
	if profileEnabled {
		t.Skip("source profiling disables copying")
	}
	for _, mutate := range []func(*fn){
		func(f *fn) { f.loopSeedSite = 0 },
		func(f *fn) { f.loopSeedSite = 8 },
		func(f *fn) { wrWord(f.a.B, 0, 0x11000421) },
		func(f *fn) { f.ctrl[1].height = 1 },
		func(f *fn) { f.intervalControl = true },
		func(f *fn) { wrWord(f.a.B, 52, 0x11000821) },
		func(f *fn) { wrWord(f.a.B, 24, 0x11000421) },
		func(f *fn) { wrWord(f.a.B, 28, 0x350000e9) }, // bypass final increment
		func(f *fn) { wrWord(f.a.B, 28, 0x35ffff49) }, // backward internal edge
		func(f *fn) { f.pinnedLocalMask = f.pinnedLocalMask.remove(X1) },
		func(f *fn) {
			for _, r := range [...]Reg{X11, X12, X13, X14, X15} {
				f.reserved = f.reserved.add(r)
			}
		},
	} {
		f := counterBaseFixture(false)
		mutate(f)
		before := append([]byte{}, f.a.B...)
		f.unrollScalarBranches(&f.ctrl[1])
		if rdWord(f.a.B, 0) != rdWord(before, 0) || f.stats != nil && f.stats.Peephole["scalar-loop-counter-base"] != 0 {
			t.Fatal("unsafe counter base admitted")
		}
	}
}

func TestLoopCounterBaseRetainsWrapAndTraps(t *testing.T) {
	admitted := false
	for _, wide := range []bool{false, true} {
		typ, bt, k, mul, gt, ge, rem, eqz, ne, add, xor := wasm.I32, byte(0x7f), byte(0x41), byte(0x6c), byte(0x4b), byte(0x4f), byte(0x70), byte(0x45), byte(0x47), byte(0x6a), byte(0x73)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, mul, gt, ge, rem, eqz, ne, add, xor = wasm.I64, 0x7e, 0x42, 0x7e, 0x56, 0x5a, 0x82, 0x50, 0x52, 0x7c, 0x85
			fold = func(v uint64) uint64 { return v }
		}
		// Two inverse XORs preserve the input and place the loop after real
		// entry alignment, exercising the snapshot rather than only fallback.
		body := []byte{1, 2, bt, 0x20, 0, 0x20, 1, xor, 0x20, 1, xor, 0x21, 0,
			0x02, 0x40, 0x03, 0x40,
			0x20, 3, 0x20, 2, ge, 0x0d, 1,
			0x20, 0, 0x20, 0, mul, 0x20, 1, gt, 0x0d, 1,
			0x20, 1, 0x20, 0, rem, eqz, 0x04, 0x40,
			0x20, 4, k, 1, 0x20, 0, 0x20, 0, mul, 0x20, 1, ne}
		if wide {
			body = append(body, 0xad)
		}
		body = append(body, add, add, 0x21, 4, 0x0b,
			0x20, 3, k, 1, add, 0x21, 3,
			0x20, 0, k, 1, add, 0x21, 0, 0x0c, 0, 0x0b, 0x0b,
			0x20, 4, 0x20, 0, xor, 0x20, 3, xor, 0x0b)
		m := modFuncs(t, funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, body})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				for _, input := range [][2]uint64{{1, 64}, {3, 18}, {0, 1}, {^uint64(0), ^uint64(0)}, {0xfffffff0, ^uint64(0)}, {0x80000000, 0}} {
					for count := uint64(0); count < 18; count++ {
						d, v, i, sum := fold(input[0]), fold(input[1]), uint64(0), uint64(0)
						traps := false
						for i < count {
							sq := fold(d * d)
							if sq > v {
								break
							}
							if d == 0 {
								traps = true
								break
							}
							if v%d == 0 {
								sum = fold(sum + 1)
								if sq != v {
									sum = fold(sum + 1)
								}
							}
							i++
							d = fold(d + 1)
						}
						a, b := input[0], input[1]
						if !wide {
							a |= 0xdeadbeef00000000
							b |= 0xcafebabe00000000
						}
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"scalar-loop-unroll": enabled, "value-facts": false}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, a, b, count)
						if traps {
							var e *rt.TrapError
							if !errors.As(err, &e) || e.Code != rt.TrapDivZero {
								t.Fatalf("wrong trap: %v", err)
							}
						} else if err != nil || got != fold(sum^d^i) {
							t.Fatalf("wide=%v on=%v input=%x count=%d got=%x want=%x err=%v", wide, enabled, input, count, got, fold(sum^d^i), err)
						}
						if diagnosticsEnabled && stats.Funcs[0].Peephole["scalar-loop-counter-base"] != 0 {
							if !enabled || profileEnabled {
								t.Fatal("counter base selected in disabled mode")
							}
							admitted = true
						}
					}
				}
			}
		}
	}
	if diagnosticsEnabled && !profileEnabled && !admitted {
		t.Fatal("execution fixtures never selected counter base")
	}
}
