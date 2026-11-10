// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestPretestLoopLatchExecutionWithRotates(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, constant, rotate, multiply, add := wasm.I32, byte(0x41), byte(0x77), byte(0x6c), byte(0x6a)
		if wide {
			typ, constant, rotate, multiply, add = wasm.I64, 0x42, 0x89, 0x7e, 0x7c
		}
		body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x20, 0, 0x4f, 0x0d, 1,
			0x20, 1, constant, 5, rotate, constant, 29, multiply, 0x20, 2}
		if wide {
			body = append(body, 0xad) // extend_i32_u
		}
		body = append(body, add, 0x21, 1, 0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32, typ}, []wasm.ValType{typ}, body)
		for _, enabled := range []bool{false, true} {
			for _, n := range []uint64{0, 1, 2, 3, 4, 5, 17} {
				for _, seed := range []uint64{0, 1, 0xdeadbeef80000000, ^uint64(0)} {
					want := seed
					if !wide {
						want = uint64(uint32(want))
					}
					for i := uint64(0); i < n; i++ {
						if wide {
							want = bits.RotateLeft64(want, 5)*29 + i
						} else {
							want = uint64(bits.RotateLeft32(uint32(want), 5)*29 + uint32(i))
						}
					}
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"scalar-loop-unroll": enabled}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, n|0xa5a5a5a500000000, seed)
					if !wide {
						got = uint64(uint32(got))
					}
					if err != nil || got != want {
						t.Fatalf("wide=%v enabled=%v n=%d seed=%x got=%x want=%x err=%v", wide, enabled, n, seed, got, want, err)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["pretest-loop-latch"] > 0) != (enabled && !profileEnabled) {
						t.Fatalf("wide=%v enabled=%v stats=%v", wide, enabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}

func TestPretestLoopLatchGuardAndOwnerProof(t *testing.T) {
	for _, variant := range []string{"cmp32", "cmp64", "immediate32", "immediate64", "cbz32", "cbnz32", "cbz64", "cbnz64", "scratch", "shifted", "load", "patched", "unowned", "result-owner", "float", "spilled", "private", "calling", "stack-reg", "lazy-zero", "typed", "height", "compact", "regional", "reserve", "range", "exit-history"} {
		f := gpClassEmitter()
		f.policy = currentCodegenPolicy()
		f.nLocals = 2
		f.locals = []localDef{{reg: X0}, {reg: X1}}
		f.localType = []machineType{mtI32, mtI32}
		f.ctrl = []ctrlFrame{{kind: cfBlock}, {kind: cfLoop, controlSite: 0}}
		words := []uint32{0x6b01001f, 0x54000002} // CMP W0,W1; B.HS done
		positive := true
		switch variant {
		case "cmp32":
		case "cmp64":
			words[0] |= 1 << 31
		case "immediate32":
			words[0] = 0x71001c1f // CMP W0,#7
		case "immediate64":
			words[0] = 0xf1001c1f
		case "cbz32", "cbnz32", "cbz64", "cbnz64":
			words = []uint32{0x34000000}
			if variant == "cbnz32" || variant == "cbnz64" {
				words[0] |= 1 << 24
			}
			if variant == "cbz64" || variant == "cbnz64" {
				words[0] |= 1 << 31
			}
		default:
			positive = false
		}
		exit := len(words) - 1
		switch variant {
		case "scratch":
			words[0] |= 2 << 5
		case "shifted":
			words[0] |= 1 << 10
		case "load":
			words[0] = 0xb9400000
		case "patched":
			words[exit] |= 1 << 5
		case "result-owner":
			f.ctrl[0].branchN = 1
		case "float":
			f.locals[0].isFloat = true
		case "spilled":
			f.locals[0].state = lsMem
		case "private":
			f.nLocals = 1
		case "calling":
			f.makesCalls = true
		case "stack-reg":
			f.usesCalls = true
		case "lazy-zero":
			f.lazyZero = true
		case "typed":
			f.ctrl[1].paramN = 1
		case "height":
			f.ctrl[1].height = 1
		case "compact":
			f.policy.CompactNative = true
		case "regional":
			f.intervalControl = true
		case "reserve":
			f.a.ReserveBranch19 = true
		}
		if variant == "exit-history" {
			f.a.B = make([]byte, 4096)
			f.ctrl[1].controlSite = 4096
		}
		for _, word := range words {
			f.a.B = binary.LittleEndian.AppendUint32(f.a.B, word)
		}
		if variant != "unowned" {
			f.appendFrameEnd(&f.ctrl[0], f.ctrl[1].controlSite+exit*4, true)
		}
		// Body bytes, including position-dependent control, must stay in place.
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, 0x14000002)
		f.a.B = binary.LittleEndian.AppendUint32(f.a.B, 0xb9000000)
		if variant == "range" {
			f.a.B = append(f.a.B, make([]byte, 1<<20)...)
		}
		if variant == "exit-history" {
			for i := 0; i < 1024; i++ {
				f.appendFrameEnd(&f.ctrl[0], i*4, true)
			}
		}
		before := append([]byte(nil), f.a.B...)
		got := f.tryPretestLoopLatch(&f.ctrl[1])
		if got != (positive && !profileEnabled) {
			t.Fatalf("variant=%s accepted=%t", variant, got)
		}
		if !bytes.Equal(before, f.a.B[:len(before)]) || !got && len(f.a.B) != len(before) {
			t.Fatalf("variant=%s modified the existing stream", variant)
		}
		if got {
			site := len(before) + exit*4
			branch := binary.LittleEndian.Uint32(f.a.B[site:])
			displacement := int32(branch<<8) >> 13
			if target := site + int(displacement)*4; target != (exit+1)*4 {
				t.Fatalf("variant=%s latch targets %d", variant, target)
			}
			_, second, _ := f.frameEndSites(&f.ctrl[0])
			if second != uint32(len(f.a.B)-4+1) {
				t.Fatalf("variant=%s missing forward exit %x", variant, second)
			}
		}
	}
}
