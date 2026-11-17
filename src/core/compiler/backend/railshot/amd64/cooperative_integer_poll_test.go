// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64 && wago_experimental_cooperative_integer_reduction

package amd64

import (
	"bytes"
	"encoding/binary"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
	"math"
	"sort"
	"testing"
	"unsafe"
)

// cooperativeDelayedPollCode changes no original offsets or branch targets. The
// selected MOV-pointer/MOV-cell prefix jumps to appended test instrumentation;
// its original TEST and conditional trap branch remain byte-identical.
func cooperativeDelayedPollCode(t *testing.T, original []byte, site int, counter []byte, visit int) []byte {
	t.Helper()
	if len(counter) < 8 || visit < 1 || visit > math.MaxInt32 {
		t.Fatal("invalid counter/visit")
	}
	type match struct {
		start, pointerEnd, end int
		scratch                Reg
	}
	var matches []match
	for r := Reg(0); r < 16; r++ {
		if r == RSP || r == RBX {
			continue
		}
		a := &encoderamd64.Asm{}
		a.Load64(r, RBX, -offTrapCellPtr)
		pointerEnd := a.Len()
		a.Load32(r, r, 0)
		end := a.Len()
		a.TestSelf(r, false)
		for at := 0; at < len(original); {
			n := bytes.Index(original[at:], a.B)
			if n < 0 {
				break
			}
			start := at + n
			matches = append(matches, match{start, start + pointerEnd, start + end, r})
			at = start + len(a.B)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].start < matches[j].start })
	if site < 0 || site >= len(matches) {
		t.Fatalf("poll site %d missing: %v", site, matches)
	}
	m := matches[site]
	if m.end-m.start < 5 {
		t.Fatal("insufficient complete MOV instructions for rel32 jump")
	}
	var tmp []Reg
	for _, r := range []Reg{R10, R11, RAX} {
		if r != m.scratch {
			tmp = append(tmp, r)
		}
	}
	addr, value := tmp[0], tmp[1]
	a := &encoderamd64.Asm{}
	a.B = append(a.B, original[m.start:m.pointerEnd]...) // original trap pointer load
	a.B = append(a.B, 0x9c)                              // PUSHFQ; instrumentation must not leak flags
	a.Push(addr)
	a.Push(value)
	a.MovImm64(addr, uint64(uintptr(unsafe.Pointer(&counter[0]))))
	a.Load32(value, addr, 0)
	a.AluRI(0, value, 1, false)
	a.Store32(addr, 0, value)
	a.CmpImm32Mem(addr, 0, int32(visit))
	noTrap := a.JccPlaceholder(condNE)
	a.StoreImm32Mem(m.scratch, 0, int32(runtime.TrapInterrupted))
	a.PatchRel32(noTrap, a.Len())
	a.Pop(value)
	a.Pop(addr)
	a.B = append(a.B, 0x9d)                            // POPFQ; RSP fully restored
	a.B = append(a.B, original[m.pointerEnd:m.end]...) // original cell read, even on activation
	code := append([]byte(nil), original...)
	trampoline := len(code)
	code = append(code, a.B...)
	jump := func(from, to int) {
		delta := int64(to) - int64(from+5)
		if delta < math.MinInt32 || delta > math.MaxInt32 {
			t.Fatal("test trampoline rel32 overflow")
		}
		code[from] = 0xe9
		binary.LittleEndian.PutUint32(code[from+1:], uint32(int32(delta)))
	}
	back := len(code)
	code = append(code, 0, 0, 0, 0, 0)
	jump(back, m.end)
	for i := m.start; i < m.end; i++ {
		code[i] = 0x90
	}
	jump(m.start, trampoline)
	if !bytes.Equal(code[m.end:len(original)], original[m.end:]) || !bytes.Equal(code[:m.start], original[:m.start]) {
		t.Fatal("original code outside complete MOV prefix changed")
	}
	return code
}

func cooperativePollSites(code []byte) []int {
	var sites []int
	for reg := Reg(0); reg < 16; reg++ {
		a := &encoderamd64.Asm{}
		a.Load64(reg, RBX, -offTrapCellPtr)
		load := a.Len()
		a.Load32(reg, reg, 0)
		a.TestSelf(reg, false)
		for at := 0; at < len(code); {
			found := bytes.Index(code[at:], a.B)
			if found < 0 {
				break
			}
			start := at + found
			opcode := start + load
			if code[opcode] >= 0x40 && code[opcode] <= 0x4f {
				opcode++
			}
			if code[opcode] != 0x8b {
				panic("unexpected poll encoding")
			}
			sites = append(sites, opcode)
			at = start + len(a.B)
		}
	}
	sort.Ints(sites)
	return sites
}
