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

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestInlineTailSpareR8PreservesMergeHome(t *testing.T) {
	f := fn{m: &wasm.Module{}, nParams: 1, nLocals: 3, nLocalSlots: 3, regMerge: true, policy: currentCodegenPolicy(), sc: &scratch{}, localType: []machineType{mtI32, mtI32, mtI32}, localSlot: []uint32{0, 8, 16}, locals: []localDef{{reg: R12, typ: mtI32, state: lsReg}, {reg: R13, typ: mtI32, state: lsReg}, {reg: R14, typ: mtI32, state: lsReg}}, pinnedLocals: []int{0, 1, 2}, pinnedLocalMask: maskOf(R12, R13, R14)}
	data := &inlineTargetData{targets: []inlineTarget{{globalIdx: 1, params: inlineTailLoopFlag | 3, localTypeEnd: 5, resultTypeEnd: 6, res0: mtI32}}, types: []machineType{mtI32, mtI32, mtI32, mtI32, mtI32, mtI32}}
	f.reserveInlineLocals([]*inlineTarget{&data.targets[0]}, inlineTargetTable{data: data})
	if profileEnabled {
		if f.pinnedLocalMask.has(R8) {
			t.Fatal("profiling borrowed R8")
		}
		return
	}
	if !f.pinnedLocalMask.has(R8) || f.pinnedLocalMask.has(mergeReg) || !f.regMerge {
		t.Fatalf("pins=%x regmerge=%t", f.pinnedLocalMask, f.regMerge)
	}
}

func TestInlineTailSpareR8RejectsFixedRoles(t *testing.T) {
	for _, name := range []string{"params", "calls", "poll", "eh", "simd", "gc", "custom", "interval", "vector", "reserved", "pinned", "mergehome", "owned", "memory", "table", "global", "disabled"} {
		f := fn{m: &wasm.Module{}, nParams: 1, regMerge: true, policy: currentCodegenPolicy()}
		switch name {
		case "params":
			f.nParams = 4
		case "calls":
			f.makesCalls = true
		case "poll":
			f.interruptible = true
		case "eh":
			f.moduleEH = true
		case "simd":
			f.moduleHasSIMD = true
		case "gc":
			f.gcFrameRoots = &shared.GCFrameRootPlan{}
		case "custom":
			f.customInstructions = map[uint32]CustomInstruction{0: {}}
		case "interval":
			f.intervalReg = []Reg{R9}
		case "vector":
			f.vectorRegion.enabled = true
		case "reserved":
			f.reserved = maskOf(R8)
		case "pinned":
			f.pinnedLocalMask = maskOf(R8)
		case "mergehome":
			f.pinnedLocalMask = maskOf(mergeReg)
		case "owned":
			f.regUser[R8] = &elem{}
		case "memory":
			f.m.Memories = append(f.m.Memories, wasm.MemType{})
		case "table":
			f.m.Tables = append(f.m.Tables, wasm.Table{})
		case "global":
			f.m.Globals = append(f.m.Globals, wasm.Global{})
		case "disabled":
			f.regMerge = false
		}
		if f.inlineTailSpareR8() {
			t.Fatalf("%s borrowed fixed scratch", name)
		}
	}
}

func TestInlineTailSpareR8RepeatedCallsPreserveLiveResults(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	params, results := []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}
	caller := []byte{1, 2, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x41, 3, 0x4f, 0x0d, 1, 0x20, 2, 0x20, 0, 0x41, 7, 0x41, 13, 0x10, 1, 0x20, 0, 0x41, 19, 0x41, 23, 0x10, 1, 0x6a, 0x6a, 0x21, 2, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b}
	callee := []byte{1, 2, 0x7f, 0x20, 0, 0x45, 0x04, 0x7f, 0x20, 1, 0x20, 2, 0x6a, 0x05, 0x20, 1, 0x41, 2, 0x6a, 0x21, 3, 0x20, 2, 0x41, 3, 0x6a, 0x21, 4, 0x20, 0, 0x41, 1, 0x6b, 0x20, 3, 0x20, 4, 0x12, 1, 0x0b, 0x0b}
	m := modFuncs(t, funcDef{params, results, caller}, funcDef{[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, results, callee})
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"reg-merge": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 2, 31, 511, 10000} {
			want := uint64(uint32(3 * (62 + 10*n)))
			if got := runCompiledAmd64u(t, c, n|0xdeadbeef00000000); got != want {
				t.Fatalf("enabled=%t n=%d got=%d want=%d", enabled, n, got, want)
			}
		}
		if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tail-spare-pin"] != 0) != enabled {
			t.Fatalf("enabled=%t stats=%v", enabled, stats.Funcs[0].Peephole)
		}
		if c.CodeImage != nil {
			c.CodeImage.Close()
		}
	}
}

func TestInlineTailSpareR8FullWidthResults(t *testing.T) {
	if profileEnabled {
		t.Skip("tail inlining excludes profiling")
	}
	results := []wasm.ValType{wasm.I64}
	// Keep a value above 32 bits live across repeated private tail calls.
	caller := []byte{1, 2, 0x7e, 0x42, 0x80, 0x80, 0x80, 0x80, 0x80, 0x20, 0x21, 2, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x42, 3, 0x5a, 0x0d, 1, 0x20, 2, 0x20, 0, 0x42, 7, 0x42, 13, 0x10, 1, 0x20, 0, 0x42, 19, 0x42, 23, 0x10, 1, 0x7c, 0x7c, 0x21, 2, 0x20, 1, 0x42, 1, 0x7c, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b}
	callee := []byte{1, 2, 0x7e, 0x20, 0, 0x50, 0x04, 0x7e, 0x20, 1, 0x20, 2, 0x7c, 0x05, 0x20, 1, 0x42, 2, 0x7c, 0x21, 3, 0x20, 2, 0x42, 3, 0x7c, 0x21, 4, 0x20, 0, 0x42, 1, 0x7d, 0x20, 3, 0x20, 4, 0x12, 1, 0x0b, 0x0b}
	m := modFuncs(t, funcDef{results, results, caller}, funcDef{[]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, results, callee})
	for _, enabled := range []bool{false, true} {
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"reg-merge": enabled}})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range []uint64{0, 1, 31, 10000} {
			if got, want := runCompiledAmd64u(t, c, n), uint64(1<<40)+3*(62+10*n); got != want {
				t.Fatalf("enabled=%t n=%d got=%d want=%d", enabled, n, got, want)
			}
		}
		if diagnosticsEnabled && (stats.Funcs[0].Peephole["inline-tail-spare-pin"] != 0) != enabled {
			t.Fatalf("enabled=%t stats=%v", enabled, stats.Funcs[0].Peephole)
		}
		if c.CodeImage != nil {
			c.CodeImage.Close()
		}
	}
}
