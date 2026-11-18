// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestIntegerMapRecipePreservesSourceOrderAndValues(t *testing.T) {
	// Two arithmetic choices and a signed comparison feed a select. The loaded
	// local is also an exit value, so its vector must remain live after the store.
	body := []byte{0x20, 0, 0x20, 1, 0x6a, 0x20, 2, 0x20, 1, 0x6a, 0x28, 2, 0, 0x22, 4, 0x41, 3, 0x6a, 0x20, 4, 0x20, 4, 0x41, 9, 0x4a, 0x1b, 0x36, 2, 0, 0x20, 1, 0x41, 4, 0x6a, 0x22, 1, 0x20, 3, 0x47, 0x0d, 0, 0x0b}
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
	guards, ok := p.guardPlan(&facts, ind, &used)
	if !ok {
		t.Fatal("guards rejected")
	}
	recipe, ok := p.buildIntegerMapRecipe(ind, &guards.memory, &facts)
	if !ok {
		t.Fatal("recipe rejected")
	}
	if recipe.strided || recipe.needsSSE41 || recipe.needsAVX2 || recipe.registers > 16 {
		t.Fatalf("unexpected recipe requirements: %+v", recipe)
	}
	// Track the owner of each local scratch register through the schedule. A
	// reused register may only be overwritten after its last scheduled read.
	var owner [16]uint8
	for i := 1; i <= int(p.nodeN); i++ {
		if recipe.used[i] && (p.nodes[i].op == 0x20 || p.nodes[i].op == 0x41) {
			r := recipe.reg[i]
			if owner[r] != 0 {
				t.Fatal("overlapping initial values")
			}
			owner[r] = uint8(i)
		}
	}
	read := func(id uint8) {
		t.Helper()
		if id == 0 || owner[recipe.reg[id]] != id {
			t.Fatalf("node %d read after scratch was reused", id)
		}
	}
	var previousPC uint32
	storeCount := 0
	for _, event := range recipe.events[:recipe.eventN] {
		if event&integerMapStoreEvent != 0 {
			e := p.effects[uint8(event)]
			if e.pc < previousPC {
				t.Fatal("store moved before source operation")
			}
			previousPC = e.pc
			read(e.value)
			storeCount++
			continue
		}
		id := uint8(event)
		n := p.nodes[id]
		if n.pc < previousPC {
			t.Fatal("operation order changed")
		}
		previousPC = n.pc
		switch {
		case n.op == 0x28:
		case n.op == 0x45:
			read(n.a)
		case n.op == 0x1b:
			read(n.a)
			read(n.b)
			read(n.c)
		default:
			read(n.a)
			read(n.b)
		}
		owner[recipe.reg[id]] = id
	}
	if storeCount != 1 {
		t.Fatalf("stores=%d", storeCount)
	}
	for i, l := range p.locals[:p.localN] {
		if l.written && i != int(ind.local) && !recipe.exitAffine[i] {
			read(l.value)
		}
	}
}

func TestIntegerMapRecipeRejectsExcessScratch(t *testing.T) {
	var p integerMapPlan
	for i := 0; i < 15; i++ {
		local, ok := p.local(uint32(i))
		if !ok {
			t.Fatal("local capacity")
		}
		p.effects[i] = integerMapEffect{op: 0x36, value: p.locals[local].entry, pc: uint32(i + 1)}
		p.effectN++
	}
	var memory integerMapMemoryPlan
	memory.streamN = 1
	memory.streams[0] = integerMapStream{stride: 4, write: true}
	facts := p.affineFacts()
	if _, ok := p.buildIntegerMapRecipe(integerMapInduction{}, &memory, &facts); ok {
		t.Fatal("accepted excessive vector scratch")
	}
}
