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
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Hoist only the object resolver, not array bounds or payload loads. The
// bounded straight-line loop has no calls, allocation, writes, or safepoints;
// its sole reference local is invariant. Thus a fully validated raw address
// remains valid on every backedge. Earlier admitted operations cannot trap or
// expose effects, so moving the first resolution to entry preserves trap order.
type gcLoopResolverPlan struct {
	local                     int
	typeIndex, endPC, firstPC uint32
}

func (f *fn) planGCLoopResolver(reader *wasm.Reader) (p gcLoopResolverPlan, ok bool) {
	if !gcResolveReuseEnabled || !f.gcArrayHelpers || f.interruptible || f.moduleEH || f.inlineDepth != 0 || f.localBase != 0 || f.intervalControl || f.vectorRegion.enabled || len(f.customInstructions) != 0 || f.gcPureIfSites >= 16 || f.gcResolvedRegister() == regNone {
		return p, false
	}
	if profileEnabled && f.stats != nil && f.stats.RecordSources {
		return p, false
	}
	f.gcPureIfSites++
	r := *reader
	window, err := r.Bytes(min(256, r.BytesLeft()))
	if err != nil {
		return p, false
	}
	r = wasm.ReaderFrom(window)
	// -1 is a scalar expression; nonnegative entries identify direct local reads.
	var stack [64]int
	n := 0
	var writes [64]int
	nw := 0
	object := -1
	var typ, firstPC uint32
	push := func(v int) bool {
		if n == len(stack) {
			return false
		}
		stack[n] = v
		n++
		return true
	}
	for ops := 0; ops < 128; ops++ {
		opPC := uint64(f.tracePCBase) + uint64(reader.Offset()) + uint64(r.Offset())
		op, e := r.Byte()
		if e != nil {
			return p, false
		}
		switch op {
		case 0x20:
			x, e := r.U32()
			if e != nil || int(x) >= len(f.localType) || !push(int(x)) {
				return p, false
			}
		case 0x21, 0x22:
			x, e := r.U32()
			if e != nil || int(x) >= len(f.localType) || n == 0 || nw == len(writes) {
				return p, false
			}
			// Candidate source writes are rejected below. Other local assignments
			// cannot publish an object or trigger collection.
			if f.localType[x] != mtI32 && f.localType[x] != mtI64 {
				return p, false
			}
			writes[nw] = int(x)
			nw++
			if op == 0x21 {
				n--
			} else {
				stack[n-1] = -1
			}
		case 0x41:
			if _, e := r.I32(); e != nil || !push(-1) {
				return p, false
			}
		case 0x42:
			if _, e := r.I64(); e != nil || !push(-1) {
				return p, false
			}
		case 0x1a:
			if n == 0 {
				return p, false
			}
			n--
		case 0x45, 0x50:
			if n == 0 {
				return p, false
			}
			stack[n-1] = -1
		case 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
			0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
			0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
			0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a:
			if n < 2 {
				return p, false
			}
			n--
			stack[n-1] = -1
		case 0xfb:
			sub, e := r.U32()
			if e != nil || sub < 11 || sub > 13 || n < 2 {
				return p, false
			}
			t, e := r.U32()
			if e != nil {
				return p, false
			}
			scalar, final, ok := f.directGCArrayLayout(t)
			if !ok || !final || (scalar.typ != mtI32 && scalar.typ != mtI64) {
				return p, false
			}
			source := stack[n-2]
			if source < 0 || (object >= 0 && (object != source || typ != t)) {
				return p, false
			}
			if object < 0 {
				if opPC > uint64(^uint32(0)) {
					return p, false
				}
				firstPC = uint32(opPC)
			}
			object, typ = source, t
			n--
			stack[n-1] = -1
		case 0x0d:
			depth, e := r.U32()
			if e != nil || depth != 0 || n != 1 || object < 0 {
				return p, false
			}
			endPC := uint64(f.tracePCBase) + uint64(reader.Offset()) + uint64(r.Offset())
			end, e := r.Byte()
			if e != nil || end != 0x0b || endPC > uint64(^uint32(0)) {
				return p, false
			}
			for _, x := range writes[:nw] {
				if x == object {
					return p, false
				}
			}
			return gcLoopResolverPlan{object, typ, uint32(endPC), firstPC}, true
		default:
			return p, false
		}
	}
	return p, false
}

func (f *fn) hoistGCLoopResolver(reader *wasm.Reader) {
	p, ok := f.planGCLoopResolver(reader)
	if !ok {
		return
	}
	f.pushValue(storage{kind: stLocalRef, typ: f.localType[p.local], idx: uint32(p.local)})
	value := f.popValue()
	oldPC := f.wasmPC
	f.wasmPC = p.firstPC
	_, done := f.emitDirectGCObject(value, p.typeIndex, gc.PayloadOffset, p.local, true)
	f.wasmPC = oldPC
	done()
	if f.gcResolved.valid {
		f.gcResolved.loopEndPC = p.endPC
		f.stats.peep("gc-loop-resolve-hoist")
	}
}
