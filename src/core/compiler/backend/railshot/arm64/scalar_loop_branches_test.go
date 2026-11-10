// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScalarBranchLoopPreservesDispatchAndEveryExit(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, add, sub, mul, xor, ge, eqz := wasm.I32, byte(0x7f), byte(0x41), byte(0x6a), byte(0x6b), byte(0x6c), byte(0x73), byte(0x4f), byte(0x45)
		fold := func(x uint64) uint64 { return uint64(uint32(x)) }
		if wide {
			typ, bt, k, add, sub, mul, xor, ge, eqz = wasm.I64, 0x7e, 0x42, 0x7c, 0x7d, 0x7e, 0x85, 0x5a, 0x50
			fold = func(x uint64) uint64 { return x }
		}
		for _, variant := range []string{"ordinary", "two-exits", "outer-exit", "memory", "interruptible", "compact"} {
			body := []byte{1, 1, bt}
			if variant == "outer-exit" {
				body = append(body, 0x02, 0x40)
			}
			body = append(body, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, ge, 0x0d, 1)
			if variant == "two-exits" || variant == "outer-exit" {
				label := byte(1)
				if variant == "outer-exit" {
					label = 2
				}
				body = append(body, 0x20, 2, eqz, 0x0d, label)
			}
			for range 5 { // dispatch, case3, case2, case1, case0
				body = append(body, 0x02, 0x40)
			}
			body = append(body, 0x20, 1)
			if wide {
				body = append(body, 0xa7)
			}
			body = append(body, 0x41, 3, 0x71, 0x0e, 3, 0, 1, 2, 3,
				0x0b, 0x20, 2, k, 3, add, 0x21, 2, 0x0c, 3,
				0x0b, 0x20, 2, 0x20, 1, xor, 0x21, 2, 0x0c, 2,
				0x0b, 0x20, 2, k, 3, mul, k, 1, add, 0x21, 2, 0x0c, 1,
				0x0b, 0x20, 2, 0x20, 1, sub, 0x21, 2, 0x0b)
			if variant == "memory" {
				body = append(body, 0x41, 0, 0x20, 2)
				if wide {
					body = append(body, 0xa7)
				}
				body = append(body, 0x36, 2, 0)
			}
			body = append(body, 0x20, 3, 0x20, 2, add, 0x21, 3, 0x20, 1, k, 1, add, 0x21, 1, 0x0c, 0, 0x0b, 0x0b)
			if variant == "outer-exit" {
				body = append(body, 0x0b)
			}
			body = append(body, 0x20, 3, 0x20, 1, add, 0x20, 2, add, 0x0b)
			params, result := []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}
			m := modFuncs(t, funcDef{params, result, body}, funcDef{params, result, []byte{0, 0x20, 0, 0x0b}})
			if variant == "memory" {
				m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			}
			max := fold(^uint64(0))
			pairs := [][2]uint64{{3, max}, {max, max}, {max, max - 4}, {max, max - 7}}
			for n := uint64(0); n < 18; n++ {
				pairs = append(pairs, [2]uint64{n, 0})
			}
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, pair := range pairs {
						for _, seed := range []uint64{0, 3, max} {
							bound, index, state, sum := pair[0], pair[1], seed, uint64(0)
							for index < bound {
								if (variant == "two-exits" || variant == "outer-exit") && state == 0 {
									break
								}
								switch index & 3 {
								case 0:
									state = fold(state + 3)
								case 1:
									state ^= index
								case 2:
									state = fold(state*3 + 1)
								case 3:
									state = fold(state - index)
								}
								sum = fold(sum + state)
								index = fold(index + 1)
							}
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Interruptible: variant == "interruptible", CompactNative: variant == "compact", Optimizations: map[string]bool{"scalar-loop-unroll": enabled, "value-facts": false}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							a, b := pair[0], pair[1]
							if !wide {
								a |= 0xdeadbeef00000000
								b |= 0xcafebabe00000000
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, a, b, seed)
							if err != nil || got != fold(sum+index+state) {
								t.Fatalf("wide=%t variant=%s enabled=%t workers=%d bounds=%x seed=%x got=%x err=%v want=%x", wide, variant, enabled, workers, pair, seed, got, err, fold(sum+index+state))
							}
							positive := variant == "ordinary" || variant == "two-exits" || variant == "outer-exit"
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-unroll-branches"] != 0) != (enabled && positive && !profileEnabled) {
								t.Fatalf("branch unroll admission wide=%t variant=%s enabled=%t stats=%v", wide, variant, enabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func branchLoopProofFixture() *fn {
	f := gpClassEmitter()
	f.policy, f.hasLoop = currentCodegenPolicy(), true
	f.ctrl = []ctrlFrame{{kind: cfBlock}, {kind: cfLoop, controlSite: 0}}
	for _, word := range []uint32{0x6b0000bf, 0x54000002, 0x34000062, 0x11000c63, 0x14000003, 0x4a050063, 0x14000001, 0x110004a5, 0xd503201f, 0xd503201f} {
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
	}
	f.appendFrameEnd(&f.ctrl[0], 4, true)
	return f
}

func TestScalarBranchLoopCopiesInternalAndOwnedExitTargets(t *testing.T) {
	if profileEnabled {
		t.Skip("unrolling is disabled with source profiling")
	}
	f := branchLoopProofFixture()
	if !f.unrollScalarBranches(&f.ctrl[1]) || len(f.a.B) != 164 {
		t.Fatalf("positive proof rejected; bytes=%d", len(f.a.B))
	}
	for phase := 0; phase < 4; phase++ {
		for _, edge := range [][2]int{{8, 20}, {16, 28}, {24, 28}} {
			site := 40*phase + edge[0]
			word := binary.LittleEndian.Uint32(f.a.B[site:])
			delta := int(int32(word<<6)>>6) * 4
			if edge[0] == 8 {
				delta = int(int32(word<<8)>>13) * 4
			}
			if site+delta != 40*phase+edge[1] {
				t.Fatalf("copy %d internal edge %d targets %d", phase, site, site+delta)
			}
		}
	}
	first, second, overflow := f.frameEndSites(&f.ctrl[0])
	f.patchFrameEndSites(first, second, overflow)
	for phase := 0; phase < 4; phase++ {
		site := phase*40 + 4
		word := binary.LittleEndian.Uint32(f.a.B[site:])
		if site+int(int32(word<<8)>>13)*4 != len(f.a.B) {
			t.Fatalf("copy %d exit was not patched to the common owner end", phase)
		}
	}
}

func TestScalarBranchLoopRejectsUnknownEdgesAndEffects(t *testing.T) {
	if profileEnabled {
		t.Skip("unrolling is disabled with source profiling")
	}
	for _, tc := range []struct {
		at   int
		word uint32
	}{
		{8, 0x34000002},  // unowned unresolved conditional, including a pending trap
		{8, 0x34ffffa2},  // known target before this loop
		{32, 0x94000001}, // BL
		{32, 0xd61f0120}, // indirect BR
		{32, 0xd65f03c0}, // RET
		{32, 0xf9400009}, // load
		{32, 0xf9000009}, // store
		{32, 0x10000009}, // ADR
		{32, 0x90000009}, // ADRP
		{32, 0xb240003f}, // ORR SP,X1,#1 changes the stack pointer
		{32, 0xaa0003fa}, // write runtime context register
	} {
		f := branchLoopProofFixture()
		binary.LittleEndian.PutUint32(f.a.B[tc.at:], tc.word)
		if f.unrollScalarBranches(&f.ctrl[1]) || len(f.a.B) != 40 {
			t.Fatalf("unsafe word %08x at %d admitted or emitted code", tc.word, tc.at)
		}
		first, second, overflow := f.frameEndSites(&f.ctrl[0])
		if first != frameEndConditional|5 || second != 0 || len(overflow) != 0 {
			t.Fatal("rejected loop changed its owner's pending edges")
		}
	}
}

func TestScalarBranchLoopRejectsLinkedReturnSites(t *testing.T) {
	if profileEnabled {
		t.Skip("unrolling is disabled with source profiling")
	}
	f := branchLoopProofFixture()
	body := append([]byte(nil), f.a.B...)
	f.a.B = nil
	f.appendReturnSite(f.a.Branch()) // preceding return: linked-list head 1
	f.a.B = append(f.a.B, body...)
	f.ctrl[1].controlSite = 4
	f.ctrl[0].mergeIndex = 0
	f.sc.ctrlMerges = nil
	f.appendFrameEnd(&f.ctrl[0], 8, true)
	// This pending return's link is 1, which looks like a branch four bytes
	// forward into the loop. Its identity must come from the return-site chain.
	wrWord(f.a.B, 36, 0x14000000)
	f.appendReturnSite(36)
	if f.unrollScalarBranches(&f.ctrl[1]) {
		t.Fatal("linked return site admitted as internal branch")
	}
	if len(f.a.B) != 44 || f.sc.retSiteHead != 10 || rdWord(f.a.B, 36) != 0x14000001 {
		t.Fatal("rejected loop changed its pending return chain")
	}
}

func TestScalarBranchLoopCopiesPreserveInternalBackedgePhase(t *testing.T) {
	if profileEnabled {
		t.Skip("unrolling is disabled with source profiling")
	}
	f := branchLoopProofFixture()
	// An inner loop branches from offset 32 back to offset 8. Copying the
	// 40-byte outer body verbatim shifts this inner header's fetch phase.
	binary.LittleEndian.PutUint32(f.a.B[32:], 0x17fffffa)
	if !f.unrollScalarBranches(&f.ctrl[1]) {
		t.Fatal("nested branch graph was not unrolled")
	}
	stride := (len(f.a.B) - 4) / 4
	for phase := 0; phase < 4; phase++ {
		site := stride*phase + 32
		target, branch := branchTarget(site, binary.LittleEndian.Uint32(f.a.B[site:]))
		if !branch || target != stride*phase+8 || target%16 != 8 {
			t.Fatalf("copy %d backedge at %d targets %d, want original header phase 8 modulo 16", phase, site, target)
		}
		if phase > 0 {
			for at := stride*phase + 40; at < stride*(phase+1); at += 4 {
				if rdWord(f.a.B, at) != nopWord {
					t.Fatalf("copy %d padding at %d is not a NOP", phase, at)
				}
			}
		}
	}
	if stride != 48 {
		t.Fatalf("copy stride=%d, want 48", stride)
	}
}
