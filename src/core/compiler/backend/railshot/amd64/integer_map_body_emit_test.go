// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
	"golang.org/x/sys/cpu"
)

func TestIntegerMapNativeBodyMatchesScalar(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
	}
	for _, step := range []byte{4, 8} {
		for _, lanes := range []uint8{2, 4} {
			t.Run(fmt.Sprintf("step-%d-lanes-%d", step, lanes), func(t *testing.T) { testIntegerMapNativeBody(t, step, lanes) })
		}
	}
}

func testIntegerMapNativeBody(t *testing.T, step byte, lanes uint8) {
	t.Helper()
	// The last loaded value is live at the loop exit; select uses a signed test.
	body := []byte{0x20, 0, 0x20, 1, 0x6a, 0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x22, 4, 0x41, 3, 0x6a, 0x20, 4, 0x20, 4, 0x41, 9, 0x4a, 0x1b, 0x36, 2, 0, 0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b}
	update := bytes.LastIndex(body, []byte{0x20, 1, 0x41, 4, 0x6a})
	if update < 0 {
		t.Fatal("missing induction update")
	}
	body[update+3] = step
	m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}}
	var p integerMapPlan
	if !inspectIntegerMapLoop(wasm.ReaderFrom(body), []machineType{mtI32, mtI32, mtI32, mtI32, mtI32}, wasm.NewModuleInstructionClassifier(m, true), &p) {
		t.Fatal("capture rejected")
	}
	facts := p.affineFacts()
	ind, ok := p.induction(&facts)
	if !ok {
		t.Fatal("induction rejected")
	}
	used := p.requiredNodes()
	g, ok := p.guardPlan(&facts, ind, &used)
	if !ok {
		t.Fatal("guard rejected")
	}
	g.lanes = lanes
	r, ok := p.buildIntegerMapRecipe(ind, &g.memory, &facts)
	if !ok || !integerMapBodySupported(&p, &g, &r) {
		t.Fatal("recipe rejected")
	}
	a := &encoder.Asm{}
	guardSlots := int(g.localN) + 1 + 2*int(g.memory.streamN)
	outBase := int32(guardSlots * 8)
	frame := outBase + int32(g.localN)*8
	a.SubRsp(frame)
	for i := 0; i < int(g.localN); i++ {
		a.Load32(RAX, RDI, int32(i*8))
		a.Store64(RSP, int32(i*8), RAX)
	}
	a.Load64(R15, RDI, integerMapMaxLocals*8)
	ge := integerMapGuardEmitter{a: a, plan: g, gp: [4]Reg{R8, R9, R10, R11}, memorySize: R15}
	ge.emit()
	var fp [16]Reg
	for i := range fp {
		fp[i] = Reg(i)
	}
	be := integerMapBodyEmitter{a: a, plan: &p, guard: &ge, recipe: &r, facts: &facts, induction: ind, gp: [4]Reg{R8, R9, R10, R11}, fp: fp, memoryBase: RSI, outputBase: outBase}
	be.emit()
	for i, l := range p.locals[:p.localN] {
		if l.written {
			a.Load64(RAX, RSP, outBase+int32(i*8))
			a.Store64(RCX, int32(8+8*i), RAX)
		}
	}
	a.MovImm32(RAX, 1)
	done := a.JmpPlaceholder()
	fallback := a.Len()
	ge.patchFallback(fallback)
	a.Xor32(RAX, RAX)
	a.PatchRel32(done, a.Len())
	a.Store32(RCX, 0, RAX)
	a.AluRI(0, RSP, frame, true)
	a.Ret()
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
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	code, entry, err := runtime.MapCode(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Unmap(code)
	args, result, trap := ar.Alloc((integerMapMaxLocals+1)*8), ar.Alloc(8+integerMapMaxLocals*8), ar.Alloc(runtime.TrapBufferBytes)
	mem := jm.LinearMemory()
	cases := []struct {
		dst, src, counter, limit uint32
		accept                   bool
	}{{1024, 128, 0, uint32(step) * 4, true}, {1025, 129, 0, uint32(step) * 8, true}, {65000, 128, 0, uint32(step) * 16, true}, {128, 128, 0, uint32(step) * 4, true}, {132, 128, 0, uint32(step) * 4, false}, {1024, 65528, 0, uint32(step) * 4, false}, {1024, 128, 0, uint32(step) * uint32(lanes-1), true}, {1024, 128, 0xfffffffc, 4, false}}
	for _, tc := range cases {
		for i := range mem {
			mem[i] = byte(i*17 + 3)
		}
		for i := uint32(0); i < 32 && uint64(tc.src)+uint64(i)*4+4 <= uint64(len(mem)); i++ {
			v := uint32(i * 13)
			if i%3 == 0 {
				v = 0xfffffff0 + i
			}
			binary.LittleEndian.PutUint32(mem[tc.src+i*4:], v)
		}
		expected := append([]byte(nil), mem...)
		var slots [integerMapMaxLocals]uint32
		for i, l := range p.locals[:p.localN] {
			switch l.index {
			case 0:
				slots[i] = tc.dst
			case 1:
				slots[i] = tc.counter
			case 2:
				slots[i] = tc.src
			case 3:
				slots[i] = tc.limit
			case 4:
				slots[i] = 0
			}
		}
		for i, v := range slots {
			binary.LittleEndian.PutUint64(args[i*8:], uint64(v))
		}
		binary.LittleEndian.PutUint64(args[integerMapMaxLocals*8:], uint64(len(mem)))
		var last uint32
		if tc.accept {
			for counter := tc.counter; counter != tc.limit; counter += uint32(step) {
				last = binary.LittleEndian.Uint32(expected[tc.src+counter:])
				v := last
				if int32(last) > 9 {
					v += 3
				}
				binary.LittleEndian.PutUint32(expected[tc.dst+counter:], v)
			}
		}
		if err := eng.Call(entry, args, mem, trap, result); err != nil {
			t.Fatal(tc, err)
		}
		got := binary.LittleEndian.Uint32(result) != 0
		if got != tc.accept || !bytes.Equal(mem, expected) {
			t.Fatalf("case=%+v accepted=%v memory=%v", tc, got, bytes.Equal(mem, expected))
		}
		if tc.accept {
			for i, l := range p.locals[:p.localN] {
				if !l.written {
					continue
				}
				v := uint32(binary.LittleEndian.Uint64(result[8+8*i:]))
				want := last
				if uint8(i) == ind.local {
					want = tc.limit
				}
				if v != want {
					t.Fatalf("case=%+v local=%d got=%x want=%x", tc, l.index, v, want)
				}
			}
		}
	}
}

func TestIntegerMapNativeComparisons(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
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
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	args, result, trap := ar.Alloc(16), ar.Alloc(8), ar.Alloc(runtime.TrapBufferBytes)
	values := []uint32{0, 1, 9, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff}
	for op := byte(0x45); op <= 0x4f; op++ {
		for _, out := range []Reg{0, 1, 2} {
			a := &encoder.Asm{}
			a.Load32(RAX, RDI, 0)
			a.Load32(RDX, RDI, 8)
			a.MovGprToXmm(1, RAX, false)
			a.MovGprToXmm(2, RDX, false)
			a.Pshufd(1, 1, 0)
			a.Pshufd(2, 2, 0)
			recipe := integerMapRecipe{registers: 5}
			e := integerMapBodyEmitter{a: a, recipe: &recipe, gp: [4]Reg{R8, R9, R10, R11}}
			for i := range e.fp {
				e.fp[i] = Reg(i)
			}
			e.compare(out, 1, 2, op)
			a.Pextrd(RAX, out, 0)
			a.Store32(RCX, 0, RAX)
			a.Ret()
			code, entry, err := runtime.MapCode(a.B)
			if err != nil {
				t.Fatal(err)
			}
			for _, x := range values {
				for _, y := range values {
					binary.LittleEndian.PutUint64(args, uint64(x))
					binary.LittleEndian.PutUint64(args[8:], uint64(y))
					var yes bool
					switch op {
					case 0x45:
						yes = x == 0
					case 0x46:
						yes = x == y
					case 0x47:
						yes = x != y
					case 0x48:
						yes = int32(x) < int32(y)
					case 0x49:
						yes = x < y
					case 0x4a:
						yes = int32(x) > int32(y)
					case 0x4b:
						yes = x > y
					case 0x4c:
						yes = int32(x) <= int32(y)
					case 0x4d:
						yes = x <= y
					case 0x4e:
						yes = int32(x) >= int32(y)
					case 0x4f:
						yes = x >= y
					}
					if err := eng.Call(entry, args, jm.LinearMemory(), trap, result); err != nil {
						t.Fatal(err)
					}
					got := binary.LittleEndian.Uint32(result)
					want := uint32(0)
					if yes {
						want = 1
					}
					if got != want {
						t.Fatalf("op=%x out=%d x=%x y=%x got=%x want=%x", op, out, x, y, got, want)
					}
				}
			}
			runtime.Unmap(code)
		}
	}
}

func TestIntegerMapNativeConstantShifts(t *testing.T) {
	if !cpu.X86.HasSSE41 {
		t.Skip("SSE4.1 required")
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
	ar, err := runtime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Close()
	args, result, trap := ar.Alloc(8), ar.Alloc(8), ar.Alloc(runtime.TrapBufferBytes)
	for _, op := range []byte{0x74, 0x75, 0x76} {
		for _, count := range []byte{0, 1, 7, 31} {
			a := &encoder.Asm{}
			a.Load32(RAX, RDI, 0)
			a.MovGprToXmm(1, RAX, false)
			a.Pshufd(1, 1, 0)
			e := integerMapBodyEmitter{a: a}
			digit := byte(6)
			if op == 0x75 {
				digit = 4
			} else if op == 0x76 {
				digit = 2
			}
			e.shift(1, digit, count)
			a.Pextrd(RAX, 1, 0)
			a.Store32(RCX, 0, RAX)
			a.Ret()
			code, entry, err := runtime.MapCode(a.B)
			if err != nil {
				t.Fatal(err)
			}
			for _, x := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
				binary.LittleEndian.PutUint64(args, uint64(x))
				want := x << count
				if op == 0x75 {
					want = uint32(int32(x) >> count)
				} else if op == 0x76 {
					want = x >> count
				}
				if err := eng.Call(entry, args, jm.LinearMemory(), trap, result); err != nil {
					t.Fatal(err)
				}
				if got := binary.LittleEndian.Uint32(result); got != want {
					t.Fatalf("op=%x count=%d x=%x got=%x want=%x", op, count, x, got, want)
				}
			}
			runtime.Unmap(code)
		}
	}
}
