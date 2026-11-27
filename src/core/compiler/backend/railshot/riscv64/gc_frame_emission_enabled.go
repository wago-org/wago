// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo && (!wago_no_gc || !wago_no_exception_handling)

package riscv64

import (
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type integerGCFrameState struct {
	gcOperandRoots                 *wasm.CollectorOperandRoots
	gcOperandStride                int
	gcDeadAllocations, gcDeadCalls uint32
}

// Both layouts record logical local indexes. SWAR uses a pair of physical
// local slots and supplies an allocation count from its admission pass.
func (f *integerEmitter) initializeGCFrameRoots(locals, slotsPerLocal, allocations int) error {
	plan := f.mode.plan
	swar := slotsPerLocal == 2
	if plan == nil {
		if allocations != 0 {
			return unsupported("SWAR allocating GC requires exact frame roots")
		}
		return nil
	}
	plan.ResetSafepoints()
	plan.ResetCallsites()
	plan.FrameBytes = uint32(f.frameBytes)
	if !plan.Candidate || !plan.Exact || !plan.ValidLiveMasks() || swar && plan.AllocationMaskCount() != allocations {
		if swar {
			return unsupported("invalid SWAR GC frame-root plan")
		}
		return unsupported("invalid GC frame-root plan")
	}
	var previous uint32
	for i, root := range plan.Locals {
		offset := shared.GCFrameRootOffset(root.Offset)
		if uint64(root.Index) >= uint64(locals) || i != 0 && root.Index <= previous ||
			offset != uint32(f.LocalOffset(int(root.Index)*slotsPerLocal)) || uint64(offset) > uint64(f.frameBytes-8) ||
			root.Offset&shared.GCFrameFuncRefRootFlag != 0 && !f.mode.functionEnvironmentRoots() ||
			root.Offset&shared.GCFrameExternRefRootFlag != 0 && !f.mode.opaqueExternRoots() {
			plan.Exact = false
			if swar {
				return unsupported("invalid SWAR GC frame-root local home")
			}
			return unsupported("invalid GC frame-root local home")
		}
		previous = root.Index
	}
	return nil
}

func (f *integerEmitter) finishScalarGCFrameRoots() error {
	plan := f.mode.plan
	if plan == nil {
		return nil
	}
	if !plan.Exact || plan.SafepointCount()+int(f.gcDeadAllocations) != plan.AllocationMaskCount() ||
		plan.CallsiteCount()+int(f.gcDeadCalls) != plan.CallMaskCount() {
		return unsupported("incomplete GC frame-root safepoints")
	}
	if !finalizeGCFrameRootMasks(plan, f.gcDeadAllocations, f.gcDeadCalls) {
		return unsupported("invalid dead GC frame-root masks")
	}
	return nil
}

func (f *integerEmitter) finishSWARGCFrameRoots(allocations int) error {
	plan := f.mode.plan
	if plan != nil && (!plan.Exact || plan.SafepointCount() != allocations || plan.CallsiteCount() != plan.CallMaskCount()) {
		return unsupported("incomplete SWAR GC frame-root callsites")
	}
	return nil
}

func (f *integerEmitter) recordDeadGCFrameSites(classifier *wasm.ModuleInstructionClassifier, r wasm.Reader, op byte) error {
	// Source masks are conservative and include syntactic dead sites.
	// Account for those sites explicitly; every remaining site must
	// publish a native map. Operand roots still use the exact live PC.
	var dead wasm.InstructionImmediate
	if err := classifier.ClassifyInto(&r, op, &dead); err != nil {
		return err
	}
	if !f.mode.plan.Conservative {
		return unsupported("dead GC sites require conservative local masks")
	}
	switch dead.Kind {
	case wasm.InstrCall:
		inline := false
		if f.mode.customInstructions != nil {
			_, inline = f.mode.customInstructions.instructions[dead.Index]
		}
		if !inline {
			f.gcDeadCalls++
		}
	case wasm.InstrCallRef, wasm.InstrCallIndirect:
		f.gcDeadCalls++
	}
	if op == 0xfb {
		switch dead.Subopcode {
		case 0, 1, 6, 7, 8, 9, 10, 26, 27:
			f.gcDeadAllocations++
		}
	}
	if wasm.OpaqueStorageMutation(f.mode.tableModuleSource, dead.Kind, dead.Index, dead.Index2) {
		f.gcDeadAllocations++
	}
	return nil
}

// Source plans include dead sites. Once every source site has been accounted
// for, retain only masks for emitted sites. The independently validated source
// plan is conservative: every mask contains every tracked local, so removing
// a dead site cannot change the local roots of a later live site.
func finalizeGCFrameRootMasks(plan *shared.GCFrameRootPlan, deadAllocations, deadCalls uint32) bool {
	if plan == nil || deadAllocations == 0 && deadCalls == 0 {
		return true
	}
	if !plan.Exact || !plan.Conservative || plan.SafepointCount()+int(deadAllocations) != plan.AllocationMaskCount() || plan.CallsiteCount()+int(deadCalls) != plan.CallMaskCount() {
		return false
	}
	wordsPerSite := (len(plan.Locals) + 63) / 64
	if wordsPerSite == 0 {
		wordsPerSite = 1
	}
	words := make([]uint64, wordsPerSite*(plan.SafepointCount()+plan.CallsiteCount()))
	for site := 0; site < plan.SafepointCount()+plan.CallsiteCount(); site++ {
		for local := range plan.Locals {
			words[site*wordsPerSite+local/64] |= uint64(1) << (local % 64)
		}
	}
	return plan.SetLiveMasks(words, plan.SafepointCount(), plan.CallsiteCount())
}

// recordGCFrameCallsite publishes the caller-frame roots visible while a
// direct callee owns execution. Locals use their canonical homes and retained
// operands below the consumed argument vector use canonical spill homes.
func (f *integerEmitter) recordGCFrameCallsite(returnOffset int, hiddenRefs []bool) {
	f.recordGCFrameCallsiteAdjusted(returnOffset, 0, hiddenRefs)
}

// recordGCFrameCallsiteAdjusted describes a call whose callee returns through
// a bounded bridge allocation before reaching the Wasm caller frame. The
// return PC still belongs to this function image; stackAdjust skips the bridge
// so the runtime walker resumes at the caller's canonical frame homes.
func (f *integerEmitter) recordGCFrameCallsiteAdjusted(returnOffset int, stackAdjust uint32, hiddenRefs []bool) {
	plan := f.mode.plan
	if plan == nil || !plan.Candidate {
		return
	}
	site := plan.CallsiteCount()
	if site >= plan.CallMaskCount() || returnOffset <= 0 {
		plan.Exact = false
		return
	}
	offsets := make([]uint32, 0, len(plan.Locals)+len(hiddenRefs)+len(plan.FixedOffsets()))
	offsets = append(offsets, plan.FixedOffsets()...)
	if !plan.VisitLiveLocals(site, true, func(root int) {
		offsets = append(offsets, plan.Locals[root].Offset)
	}) {
		plan.Exact = false
		return
	}
	if !f.referenceOperandOffsets(hiddenRefs, f.sourcePC, func(off uint32) { offsets = append(offsets, off) }) {
		plan.Exact = false
		return
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	for i, off := range offsets {
		if off%8 != 0 || f.frameBytes < 8 || uint64(shared.GCFrameRootOffset(off)) > uint64(f.frameBytes-8) || i != 0 && off == offsets[i-1] {
			plan.Exact = false
			return
		}
	}
	plan.RecordCallsite(uint32(returnOffset), stackAdjust, offsets)
}

// recordGCFrameSafepoint snapshots the exact local and hidden operand roots
// visible while a parked allocating helper owns execution. Integer lowering
// already keeps every local and operand in its canonical frame home, so unlike
// the register backends it does not need a materialization pass here.
func (f *integerEmitter) recordGCFrameSafepoint(hiddenRefs []bool) uint32 {
	return f.recordGCFrameSafepointAt(hiddenRefs, f.sourcePC)
}

func (f *integerEmitter) recordGCFrameSafepointAt(hiddenRefs []bool, pc uint32) uint32 {
	plan := f.mode.plan
	if plan == nil || !plan.Candidate {
		return 0
	}
	id := plan.SafepointBase + uint32(plan.SafepointCount()+1)
	if id == 0 || id > shared.GCSafepointIDMax {
		plan.Exact = false
		return 0
	}
	site := plan.SafepointCount()
	if site >= plan.AllocationMaskCount() {
		plan.Exact = false
		return id
	}
	builder := plan.BeginSafepoint()
	if !plan.VisitLiveLocals(site, false, func(root int) {
		builder.AppendOffset(plan.Locals[root].Offset)
	}) {
		builder.Abort()
		plan.Exact = false
		return id
	}
	if !f.referenceOperandOffsets(hiddenRefs, pc, func(off uint32) { builder.AppendOffset(off) }) {
		builder.Abort()
		plan.Exact = false
		return id
	}
	for _, off := range plan.FixedOffsets() {
		builder.AppendOffset(off)
	}
	offsets, ok := builder.Offsets()
	if !ok {
		builder.Abort()
		plan.Exact = false
		return id
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	for _, off := range offsets {
		if off%8 != 0 || f.frameBytes < 8 || uint64(shared.GCFrameRootOffset(off)) > uint64(f.frameBytes-8) {
			builder.Abort()
			plan.Exact = false
			return id
		}
	}
	// Locals and hidden operands can intentionally name the same canonical home
	// only in malformed producer input. Reject duplicates instead of publishing
	// an ambiguous root vector.
	for i := 1; i < len(offsets); i++ {
		if offsets[i] == offsets[i-1] {
			builder.Abort()
			plan.Exact = false
			return id
		}
	}
	if !builder.Commit() {
		builder.Abort()
		plan.Exact = false
	}
	return id
}
