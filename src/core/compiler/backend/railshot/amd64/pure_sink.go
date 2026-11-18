// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// Experimental complete-transition specialization, reconstructed after loss of
// the original local workspace. This is not the original measured source.
var pureLocalSinkEnabled = os.Getenv("WAGO_EXPERIMENTAL_PURE_LOCAL_SINK") == "1"
var pureLocalSinkEntryGateEnabled = os.Getenv("WAGO_PURE_LOCAL_SINK_ENTRY_GATE") != "0"

// tryPureLocalSink consumes const/ALU/set-or-tee following a local.get index.
// All admission checks are read-only. Unsupported source or compiler state
// retains the ordinary lowering path, including its optimization priorities.
func (f *fn) tryPureLocalSink(r *wasm.Reader, x uint32) bool {
	if profileEnabled || regallocCheckEnabled {
		return false
	}
	next, ok := r.Peek()
	if !ok || next != 0x41 && next != 0x42 {
		return false
	}
	if f.inlineDepth != 0 || f.localBase != 0 || f.inlineTailConstants != nil || len(f.intervalReg) != 0 ||
		f.unreachable || f.s.logicalDepth != 0 || f.s.head.next != f.s.head || f.pinned != 0 ||
		f.storeFwd.valid || f.gcResolved.valid || f.gcSubtypeProofN&gcFreshStructFactMode != 0 || f.vectorRegion.enabled {
		return false
	}
	if int(x) >= len(f.localType) || f.gcFrameLocal(int(x)) {
		return false
	}
	typ := f.localType[x]
	source, floating, pinned := f.pinReg(int(x))
	if !pinned || floating || f.regUser[source] != nil || f.locals[x].state != lsReg && f.locals[x].state != lsStackReg {
		return false
	}
	q := *r
	q.Byte()
	var c int64
	if next == 0x41 && typ == mtI32 {
		v, err := q.I32()
		if err != nil {
			return false
		}
		c = int64(v)
	} else if next == 0x42 && typ == mtI64 {
		v, err := q.I64()
		if err != nil {
			return false
		}
		c = v
	} else {
		return false
	}
	if c == 0 || c == -1 || !fitsImm32(c) {
		return false
	}
	op, err := q.Byte()
	if err != nil {
		return false
	}
	var operation wOp
	if typ == mtI64 {
		if op < 0x7c {
			return false
		}
		op -= 0x12
	}
	switch op {
	case 0x6a:
		operation = opAdd
	case 0x6b:
		operation = opSub
	case 0x71:
		operation = opAnd
	case 0x72:
		operation = opOr
	case 0x73:
		operation = opXor
	default:
		return false
	}
	setPC := q.Offset()
	set, err := q.Byte()
	if err != nil || set != 0x21 && set != 0x22 {
		return false
	}
	dst, err := q.U32()
	if err != nil || int(dst) >= len(f.localType) || f.localType[dst] != typ || f.gcFrameLocal(int(dst)) {
		return false
	}
	dest, destFloat, destPinned := f.pinReg(int(dst))
	if !destPinned || destFloat || f.regUser[dest] != nil || f.locals[dst].state != lsReg && f.locals[dst].state != lsStackReg {
		return false
	}
	tee := set == 0x22
	if tee && !f.opt(optTeeSink) {
		return false
	}
	// Keep the original adjacent set/get-to-tee optimization in charge.
	if next, ok := q.Peek(); !tee && ok && next == 0x20 {
		look := q
		look.Byte()
		got, err := look.U32()
		if err == nil && got == dst {
			return false
		}
	}
	// Commit only after complete source and ownership admission. With no physical
	// operands, there are no old local reads to realize or owners to spill.
	*r = q
	f.wasmPC = f.tracePCBase + uint32(setPC)
	if dst < 64 {
		f.localWritten |= uint64(1) << dst
	}
	f.inlineTailConstantSet(int(dst))
	f.invalidateBoundsCertFor(1, dst)
	f.clearV128LocalAliases(int(dst))
	f.invalidateGCLoadFactsForLocal(int(dst))
	if dest != source {
		f.moveInt(dest, source, typ)
	}
	wide := typ.is64()
	if f.policy.CompactNative && (operation == opAdd || operation == opSub) && c == 1 {
		if operation == opAdd {
			f.a.Inc(dest, wide)
		} else {
			f.a.Dec(dest, wide)
		}
		f.stats.peep("inc-dec")
	} else {
		if wide && operation == opAnd && f.opt(optI64Mask32) && uint64(c) <= uint64(^uint32(0)) {
			wide = false
			f.stats.peep("i64-mask32")
		}
		f.a.AluRI(aluTable[operation].digit, dest, int32(c), wide)
	}
	f.release(dest)
	f.markLocalDirty(int(dst))
	f.s.canonicalSlots = false
	if tee {
		f.s.pushLocalRead(stLocalReg, typ, dest, dst)
		f.tryByteSwapAfterTee(r, int(dst))
	}
	f.stats.peep("pure-local-sink")
	return true
}
