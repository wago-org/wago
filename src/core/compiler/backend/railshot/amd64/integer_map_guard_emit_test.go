// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math/rand"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
)

func TestIntegerMapSharedStreamChain(t *testing.T) {
	var m integerMapMemoryPlan
	m.streamN = 3
	for i := 0; i < int(m.streamN); i++ {
		m.streams[i].stride = 4
		m.streams[i].address.constant = uint32(i * 100)
	}
	if base, delta := shareStream(&m, 2); base != 0 || delta != 200 {
		t.Fatalf("chain resolved to stream %d offset %d, want stream 0 offset 200", base, delta)
	}
}

func TestIntegerMapNativeGuardsMatchReference(t *testing.T) {
	p, base := integerMapGuardFixture(t)
	step8 := base
	step8.step = 8
	for i := range step8.memory.streams[:step8.memory.streamN] {
		step8.memory.streams[i].stride = 8
	}
	constantLimit := base
	constantLimit.limit = integerMapAffine{valid: true, constant: 64}
	constantLimit.memory.streams[0].offset = 12
	invariantLoad := base
	invariantLoad.memory.streams[2] = integerMapStream{address: integerMapAffine{valid: true, constant: 2048}, stride: 0, read: true}
	invariantLoad.memory.streamN = 3
	invariantLoad.memory.pairs[1] = integerMapAliasPair{a: 1, b: 2}
	invariantLoad.memory.pairN = 2
	largeStride := base
	largeStride.step = 1
	for i := range largeStride.memory.streams[:largeStride.memory.streamN] {
		largeStride.memory.streams[i].stride = 0x7ffffffc
	}
	for _, tc := range []struct {
		name   string
		guards integerMapGuards
	}{
		{"baseline", base}, {"step8", step8}, {"constant-limit", constantLimit}, {"invariant-load", invariantLoad}, {"large-stride", largeStride},
	} {
		t.Run(tc.name, func(t *testing.T) { checkIntegerMapNativeGuards(t, p, tc.guards) })
	}
}

func checkIntegerMapNativeGuards(t *testing.T, p integerMapPlan, g integerMapGuards) {
	t.Helper()
	a := &encoder.Asm{}
	frame := int32((int(g.localN) + 1 + 2*int(g.memory.streamN)) * 8)
	a.SubRsp(frame)
	for i := 0; i < int(g.localN); i++ {
		a.Load32(RAX, RDI, int32(i*8))
		a.Store64(RSP, int32(i*8), RAX)
	}
	a.Load64(R15, RDI, integerMapMaxLocals*8)
	e := integerMapGuardEmitter{a: a, plan: g, gp: [4]Reg{R8, R9, R10, R11}, memorySize: R15}
	e.emit()
	for i := 0; i < int(g.memory.streamN); i++ {
		a.Load64(RAX, RSP, e.slot(e.startSlot(i)))
		a.Store64(RCX, int32(8+16*i), RAX)
		a.Load64(RAX, RSP, e.slot(e.endSlot(i)))
		a.Store64(RCX, int32(16+16*i), RAX)
	}
	a.Load32(RAX, RSP, e.slot(e.countSlot()))
	done := a.JmpPlaceholder()
	fallback := a.Len()
	e.patchFallback(fallback)
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
	args, result, trap := ar.Alloc((integerMapMaxLocals+1)*8), ar.Alloc(8+16*int(g.memory.streamN)), ar.Alloc(runtime.TrapBufferBytes)
	// Exact memory32 end, one-byte overflow, zero memory, and overlapping
	// ranges exercise guard boundaries without touching the modeled memory.
	for _, tc := range []struct {
		dst, counter, src, limit uint32
		memory                   uint64
	}{
		{0, 0, 0xfffffff0, 16, 1 << 32},
		{0, 0, 0xfffffff1, 16, 1 << 32},
		{0, 0, 128, 16, 0},
		{0, 0, 8, 16, 65536},
		{0, 0xfffffffc, 128, 4, 1 << 32},
		{0, 0, 128, 0xfffffffc, 1 << 32},
	} {
		var locals [integerMapMaxLocals]uint32
		for i, l := range p.locals[:p.localN] {
			switch l.index {
			case 0:
				locals[i] = tc.dst
			case 1:
				locals[i] = tc.counter
			case 2:
				locals[i] = tc.src
			case 3:
				locals[i] = tc.limit
			}
		}
		for i, v := range locals {
			binary.LittleEndian.PutUint64(args[i*8:], uint64(v))
		}
		binary.LittleEndian.PutUint64(args[integerMapMaxLocals*8:], tc.memory)
		want, _ := evaluateIntegerMapGuards(g, locals, tc.memory)
		if err := eng.Call(entry, args, jm.LinearMemory(), trap, result); err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(result); got != want {
			t.Fatalf("boundary=%+v got=%d want=%d", tc, got, want)
		}
	}
	rng := rand.New(rand.NewSource(293))
	for test := 0; test < 20000; test++ {
		var locals [integerMapMaxLocals]uint32
		for i, l := range p.locals[:p.localN] {
			switch l.index {
			case 0, 2:
				locals[i] = rng.Uint32()
			case 1:
				locals[i] = rng.Uint32() & 255
			case 3:
				locals[i] = rng.Uint32() & 511
			}
		}
		if test%2 == 0 {
			for i, l := range p.locals[:p.localN] {
				switch l.index {
				case 0:
					locals[i] &= 511
				case 2:
					locals[i] = (locals[i] & 511) + 512
				case 1:
					locals[i] = 0
				case 3:
					locals[i] = uint32(16 * (1 + rng.Intn(8)))
				}
			}
		}
		memory := uint64(4096)
		if test%5 == 0 {
			memory = 1 << 32
		}
		for i, v := range locals {
			binary.LittleEndian.PutUint64(args[i*8:], uint64(v))
		}
		binary.LittleEndian.PutUint64(args[integerMapMaxLocals*8:], memory)
		wantCount, want := evaluateIntegerMapGuards(g, locals, memory)
		if err := eng.Call(entry, args, jm.LinearMemory(), trap, result); err != nil {
			t.Fatal(err)
		}
		gotCount := binary.LittleEndian.Uint32(result)
		if (gotCount != 0) != want || gotCount != wantCount {
			t.Fatalf("test=%d locals=%v memory=%d got=%d want=%d", test, locals, memory, gotCount, wantCount)
		}
		if want {
			for i, stream := range g.memory.streams[:g.memory.streamN] {
				start := stream.address.constant
				for j, coeff := range stream.address.coeff {
					start += coeff * locals[j]
				}
				wantStart := uint64(start) + stream.offset
				wantEnd := wantStart + uint64(wantCount-1)*uint64(stream.stride) + 4
				gotStart := binary.LittleEndian.Uint64(result[8+16*i:])
				gotEnd := binary.LittleEndian.Uint64(result[16+16*i:])
				if gotStart != wantStart || gotEnd != wantEnd {
					t.Fatalf("test=%d stream=%d range=[%d,%d) want=[%d,%d)", test, i, gotStart, gotEnd, wantStart, wantEnd)
				}
			}
		}
	}
}

func TestIntegerMapGuardFailureCapacity(t *testing.T) {
	var g integerMapGuards
	g.localN = 1
	g.lanes = 4
	g.step = 2
	g.limit = integerMapAffine{valid: true, constant: 32}
	g.memory.streamN = integerMapMaxEffects
	for i := range g.memory.streams {
		g.memory.streams[i] = integerMapStream{address: integerMapAffine{valid: true, constant: uint32(i * 128)}, stride: 4, read: true}
	}
	g.memory.pairN = integerMapMaxAliasPairs
	for i := range g.memory.pairs {
		g.memory.pairs[i] = integerMapAliasPair{a: uint8(i), b: uint8(i + 1)}
	}
	e := integerMapGuardEmitter{a: &encoder.Asm{}, plan: g, gp: [4]Reg{R8, R9, R10, R11}, memorySize: R15}
	e.emit()
	if e.failureN != 2+2*integerMapMaxEffects+integerMapMaxAliasPairs {
		t.Fatalf("failures=%d", e.failureN)
	}
	e.patchFallback(e.a.Len())
}
