// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// Recognize a constant-entry positive induction loop with exactly one counter
// write and one canonical conditional backedge. The latch either compares with
// a higher constant limit or branches on the updated value while progressing to
// zero through one final i32 wrap. Divisibility proves exact termination. The
// returned range covers values observed by the body before the latch update.
// No trapping check is hoisted.
func (f *fn) proveRootLoopRange(body []byte, known uint64, entries *[64]uint32) (local uint8, minimum, maximum uint32) {
	if len(body) > 16<<10 {
		return
	}
	type instruction struct {
		op       byte
		index    uint32
		constant int32
	}
	var recent [8]instruction
	var writes [64]uint16
	count, branches := 0, 0
	r := wasm.ReaderFrom(body)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return
		}
		if !(op == 0x01 || op == 0x1a || op == 0x1b || op == 0x0d || op == 0x0b || op >= 0x20 && op <= 0x22 || op >= 0x28 && op <= 0x3e || op >= 0x41 && op <= 0xc4) {
			return
		}
		in := instruction{op: op}
		copy := r
		switch op {
		case 0x41:
			in.constant, err = copy.I32()
		case 0x20, 0x21, 0x22, 0x0d:
			in.index, err = copy.U32()
		}
		if err != nil || f.classifier.ClassifyInto(&r, op, &imm) != nil {
			return
		}
		if op == 0x21 || op == 0x22 {
			if int(in.index) >= f.nLocals || in.index >= 64 {
				return
			}
			writes[in.index]++
		}
		if op == 0x0d {
			branches++
		}
		if op == 0x0b && r.HasNext() {
			return
		}
		recent[count%len(recent)] = in
		count++
	}
	if count < 6 || branches != 1 {
		return
	}
	var tail [8]instruction
	limitForm := count >= len(tail)
	if limitForm {
		for i := range tail {
			tail[i] = recent[(count-len(tail)+i)%len(recent)]
		}
	}
	if limitForm {
		for i, op := range []byte{0x20, 0x41, 0x6a, 0x22, 0x41, 0x47, 0x0d, 0x0b} {
			if tail[i].op != op {
				limitForm = false
				break
			}
		}
	}
	zeroForm := false
	if !limitForm {
		var short [6]instruction
		for i := range short {
			short[i] = recent[(count-6+i)%len(recent)]
		}
		zeroForm = true
		for i, op := range []byte{0x20, 0x41, 0x6a, 0x22, 0x0d, 0x0b} {
			if short[i].op != op {
				zeroForm = false
				break
			}
		}
		if zeroForm {
			tail[0], tail[1], tail[2], tail[3], tail[6], tail[7] = short[0], short[1], short[2], short[3], short[4], short[5]
		}
	}
	if !limitForm && !zeroForm {
		return
	}
	index := tail[0].index
	if index >= 64 || int(index) >= f.nLocals || int(index) < f.nParams || f.localType[index] != mtI32 || known&(uint64(1)<<index) == 0 || tail[3].index != index || writes[index] != 1 || tail[6].index != 0 {
		return
	}
	start := entries[index]
	stride := tail[1].constant
	if stride <= 0 {
		return
	}
	step := uint64(uint32(stride))
	if limitForm {
		limit := uint32(tail[4].constant)
		if start >= limit || uint64(limit-start)%step != 0 {
			return
		}
		return uint8(index + 1), start, limit
	}
	if start == 0 {
		return
	}
	distance := uint64(1<<32) - uint64(start)
	if step > distance || distance%step != 0 {
		return
	}
	return uint8(index + 1), start, uint32(uint64(start) + distance - step)
}

// Only the first outermost loop is considered, bounding lookahead to one body
// and one prefix scan per function. A declared local never written before this
// entry is still zero. The matched latch proves either a non-wrapping bounded
// progression or one exact final wrap to zero.
// Later source reuse is outside this frame's facts.
func (f *fn) prepareRootLoopRange(r *wasm.Reader) {
	if len(f.ctrl) != 1 || f.ctrl[0].kind != cfFunc || f.inlineDepth != 0 || f.localBase != 0 || f.rootAddressLoopScanned {
		return
	}
	f.rootAddressLoopScanned = true
	if !f.opt(optMemoryMinimumBounds) || f.nLocals <= f.nParams || f.nLocals > 64 || f.moduleEH || len(f.customInstructions) != 0 {
		return
	}
	memory, ok := f.memoryType(0)
	if !ok || memory.Limits.Addr64 || memory.Limits.Min == 0 {
		return
	}
	index := int(f.traceFuncIdx) - f.importedFunctionCount()
	if index < 0 || index >= len(f.m.Code) {
		return
	}
	body := f.m.Code[index].BodyBytes
	if len(body) > 16<<10 || f.wasmPC < f.tracePCBase {
		return
	}
	start := int(f.wasmPC - f.tracePCBase)
	begin := r.Offset()
	if start < 0 || begin < start || begin > len(body) {
		return
	}
	var known uint64
	var entries [64]uint32
	for i := f.nParams; i < f.nLocals; i++ {
		if f.localType[i] == mtI32 {
			known |= uint64(1) << i
		}
	}
	prefix := wasm.ReaderFrom(body)
	var imm wasm.InstructionImmediate
	depth := 0
	previousConst := false
	var previousValue uint32
	for prefix.Offset() < start {
		op, err := prefix.Byte()
		currentConst := false
		var currentValue uint32
		if op == 0x41 {
			copy := prefix
			value, e := copy.I32()
			if e != nil {
				return
			}
			currentConst, currentValue = true, uint32(value)
		}
		if err != nil || f.classifier.ClassifyInto(&prefix, op, &imm) != nil {
			return
		}
		if op == 0x21 || op == 0x22 {
			if int(imm.Index) >= f.nLocals {
				return
			}
			bit := uint64(1) << imm.Index
			if depth == 0 && previousConst && f.localType[imm.Index] == mtI32 {
				known |= bit
				entries[imm.Index] = previousValue
			} else {
				known &^= bit
			}
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x06, 0x1f:
			depth++
		case 0x0b:
			if depth > 0 {
				depth--
			}
		}
		previousConst, previousValue = currentConst, currentValue
	}
	if prefix.Offset() != start {
		return
	}
	scan := *r
	depth = 0
	for scan.HasNext() {
		op, err := scan.Byte()
		if err != nil || f.classifier.ClassifyInto(&scan, op, &imm) != nil {
			return
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f:
			depth++
		case 0x0b:
			if depth == 0 {
				loopBody := body[begin:scan.Offset()]
				f.rootLoopRangeLocal, f.rootLoopRangeMinimum, f.rootLoopRangeMaximum = f.proveRootLoopRange(loopBody, known, &entries)
				if f.rootLoopRangeLocal != 0 {
					f.stats.peep("root-loop-address-range")
				}
				return
			}
			depth--
		}
	}
	return
}
