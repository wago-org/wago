// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && (!wago_no_gc || !wago_no_exception_handling)

package riscv64

import (
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestExceptionTypedCollectorOperandHomes(t *testing.T) {
	m := exceptionTypedOperandModule(t)
	roots, err := wasm.BuildCollectorOperandRoots(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	pc := m.Code[0].LocalDeclBytes + 6
	if got, ok := roots.At(pc); !ok || !slices.Equal(got, []uint32{1}) {
		t.Fatalf("collector operands=%v present=%t", got, ok)
	}
	for _, stride := range []int{1, 2} {
		f := integerEmitter{integerGCFrameState: integerGCFrameState{gcOperandRoots: roots, gcOperandStride: stride}}
		hidden := make([]bool, stride+1)
		hidden[0], hidden[stride] = true, true
		var got []int
		if !f.collectorOperandSlots(hidden, pc, func(slot int) { got = append(got, slot) }) || !slices.Equal(got, []int{stride}) {
			t.Fatalf("stride %d roots=%v", stride, got)
		}
	}
	f := integerEmitter{integerGCFrameState: integerGCFrameState{gcOperandRoots: roots, gcOperandStride: 2}, customSpillOffsets: []int32{0, 512, 528}}
	hidden := make([]bool, 65)
	hidden[0], hidden[64] = true, true
	var got []int
	if !f.collectorOperandSlots(hidden, pc, func(slot int) { got = append(got, slot) }) || !slices.Equal(got, []int{64}) {
		t.Fatalf("compact custom roots=%v", got)
	}
	hidden[64] = false // The registered custom class has final authority over its physical home.
	got = nil
	if !f.collectorOperandSlots(hidden, pc, func(slot int) { got = append(got, slot) }) || len(got) != 0 {
		t.Fatalf("opaque custom became collector root=%v", got)
	}
	m.Types[0].SubTypes[0].Comp.Params[0] = wasm.FuncRef
	functionRoots, err := wasm.BuildCollectorOperandRoots(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := functionRoots.FunctionsAt(pc); !ok || !slices.Equal(got, []uint32{0}) {
		t.Fatalf("function operands=%v present=%t", got, ok)
	}
	f = integerEmitter{target: target{header: 48, mode: emissionMode{ownedExceptionReferences: true}}, integerGCFrameState: integerGCFrameState{gcOperandRoots: functionRoots, gcOperandStride: 1}}
	var offsets []uint32
	if !f.referenceOperandOffsets([]bool{true, true}, pc, func(off uint32) { offsets = append(offsets, off) }) || !slices.Equal(offsets, []uint32{56, shared.GCFrameFuncRefRootFlag | 48}) {
		t.Fatalf("typed reference offsets=%v", offsets)
	}
	f.customSpillOffsets = []int32{0, 512, 528}
	customClasses := make([]bool, 65)
	customClasses[64] = true // The real GC value lies above an opaque 512-byte value.
	offsets = nil
	if !f.referenceOperandOffsets(customClasses, pc, func(off uint32) { offsets = append(offsets, off) }) || !slices.Equal(offsets, []uint32{560}) {
		t.Fatalf("opaque custom became function root=%v", offsets)
	}
	f.customSpillOffsets = nil
	f.mode.ownedExceptionReferences = false
	offsets = nil
	if !f.referenceOperandOffsets([]bool{true, true}, pc, func(off uint32) { offsets = append(offsets, off) }) || !slices.Equal(offsets, []uint32{56}) {
		t.Fatalf("unqualified function roots=%v", offsets)
	}
}

func TestFunctionStorageMutationRoots(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m := &wasm.Module{Types: []wasm.RecType{
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}}}},
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{wasm.NewFieldType(wasm.StorageVal(wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))), wasm.Var)}}}}},
			{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false)), wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))}}}}},
		}, FuncTypes: []wasm.TypeIdx{{Index: 2}}}
		body := []byte{0x20, 0, 0x20, 1}
		if vector {
			body = append(body, 0xfd, 12)
			body = append(body, make([]byte, 16)...)
			body = append(body, 0x1a)
		}
		pc := uint32(len(body))
		body = append(body, 0xfb, 5, 1, 0, 0x0b)
		m.Code = []wasm.Func{{BodyBytes: body}}
		if !wasm.ModuleFunctionReferenceStorage(m) || !wasm.FunctionStorageMutation(m, wasm.InstrStructSet, 1, 0) {
			t.Fatal("typed function storage not classified")
		}
		directory, ok := newInternalReferenceTypeDirectory(m.Types)
		if !ok || !(internalReferenceTypes{gcRootedScalar: true, rootedDirectory: directory}).rootedStructFieldReference(1, 0) {
			t.Fatal("typed function storage has no reference carrier")
		}
		if wasm.FunctionStorageMutation(m, wasm.InstrStructGet, 1, 0) || wasm.FunctionStorageMutation(m, wasm.InstrStructSet, 1, 1) {
			t.Fatal("unrelated or out-of-range field classified as collecting mutation")
		}
		operandPlan, err := wasm.BuildCollectorOperandRoots(m, 0)
		if err != nil {
			t.Fatal(err)
		}
		stride := 1
		if vector {
			stride = 2
		}
		localEnd := uint32(48 + 2*stride*8)
		plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, Locals: []shared.GCFrameLocal{{Index: 0, Offset: 48}, {Index: 1, Offset: shared.GCFrameFuncRefRootFlag | uint32(48+stride*8)}}}
		if !plan.SetLiveMasks([]uint64{3}, 1, 0) {
			t.Fatal("local root masks")
		}
		f := &integerEmitter{target: target{header: 48, nlocals: 2 * stride, sourcePC: pc, mode: emissionMode{gcFunctionEnvironments: true, tableModuleSource: m, gcFrameEmissionMode: gcFrameEmissionMode{plan: plan}}}, frameBytes: 256, integerGCFrameState: integerGCFrameState{gcOperandRoots: operandPlan, gcOperandStride: stride}}
		refs := make([]bool, 2*stride)
		refs[0], refs[stride] = true, true
		f.gcStructSetScalar(0, 1, 0, true, refs)
		if f.err != nil || len(f.calls) != 1 || f.calls[0].safepoint != 1 || !plan.Exact {
			t.Fatalf("function setter has no collecting call: calls=%v error=%v", f.calls, f.err)
		}
		var got []uint32
		if !plan.VisitSafepoints(func(_ int, roots []uint32) bool { got = slices.Clone(roots); return true }) {
			t.Fatal("setter root map")
		}
		want := []uint32{48, localEnd, shared.GCFrameFuncRefRootFlag | uint32(48+stride*8), shared.GCFrameFuncRefRootFlag | (localEnd + uint32(stride*8))}
		if !slices.Equal(got, want) {
			t.Fatalf("vector=%v setter roots=%v, want %v", vector, got, want)
		}
	}
}

func TestRootedExternOperandHomes(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.ExternRef}, Results: []wasm.ValType{wasm.AnyRef}}}}}}, FuncTypes: []wasm.TypeIdx{{Index: 0}}, Code: []wasm.Func{{BodyBytes: []byte{0x20, 0, 0xfb, 26, 0x0b}}}}
	plan := &shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, Locals: []shared.GCFrameLocal{{Index: 0, Offset: shared.GCFrameExternRefRootFlag | 48}}}
	if !plan.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("local root masks")
	}
	f := &integerEmitter{target: target{header: 48, nlocals: 1, sourcePC: 2, mode: emissionMode{gcRootedScalar: true, tableModuleSource: m, gcFrameEmissionMode: gcFrameEmissionMode{plan: plan}}}, frameBytes: 128}
	if err := f.prepareCollectorOperands(&m.Code[0], false); err != nil {
		t.Fatal(err)
	}
	if id := f.recordGCFrameSafepoint([]bool{true}); id != 1 || !plan.Exact {
		t.Fatal("extern safepoint", id, f.err)
	}
	var got []uint32
	if !plan.VisitSafepoints(func(_ int, offsets []uint32) bool { got = slices.Clone(offsets); return true }) {
		t.Fatal("invalid extern safepoint stream")
	}
	want := []uint32{shared.GCFrameExternRefRootFlag | 48, shared.GCFrameExternRefRootFlag | 56}
	if !slices.Equal(got, want) {
		t.Fatalf("opaque extern root homes=%v want%v", got, want)
	}
}
