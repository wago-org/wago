// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"math/bits"
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var modularLoadGroupEnabled = os.Getenv("WAGO_AMD64_MODULAR_LOAD_GROUP") != "0"

// modularLoadAddress recognizes wrapping i32 source arithmetic, not the
// widened memory immediate. The bounded group currently covers word loads.
func modularLoadAddress(e *elem) (local uint32, displacement uint8, ok bool) {
	if e != nil && e.isValue() && (e.st.kind == stLocalReg || e.st.kind == stLocalRef) && e.st.typ == mtI32 {
		return e.st.idx, 0, true
	}
	if e == nil || !e.isDeferred() || e.deferredOp() != opAdd || e.valueType() != mtI32 {
		return
	}
	a, b := e.arg0, e.arg1
	if a.isValue() && a.st.kind == stConst {
		a, b = b, a
	}
	if !a.isValue() || (a.st.kind != stLocalReg && a.st.kind != stLocalRef) ||
		!b.isValue() || b.st.kind != stConst || b.st.typ != mtI32 {
		return
	}
	v := uint32(b.st.cval)
	if v >= 256 || v&3 != 0 {
		return
	}
	return a.st.idx, uint8(v), true
}

type modularLoadGroup struct {
	local            uint32
	first, low, high uint8
	valid, covered   bool
}

// scanModularLoadGroup looks only through effects that cannot mutate memory or
// trap except for plain loads. It proves a dense set of adjacent word accesses
// using the same unchanged local, with a strict per-function scan budget.
func (f *fn) scanModularLoadGroup(r *wasm.Reader, local uint32, first uint8) modularLoadGroup {
	const limit = 192
	scan := *r
	mask := uint64(1) << (first / 4)
	stage := uint8(0)
	var candidate uint32
	for n := 0; n < limit && f.boundsScanWork < maxFunctionRangeOps && scan.HasNext(); n++ {
		// Sparse probes rarely amortize their setup; stop before scanning a long
		// unrelated expression unless another matching load appears nearby.
		if n == 24 && bits.OnesCount64(mask) < 2 {
			return modularLoadGroup{}
		}
		f.boundsScanWork++
		op, err := scan.Byte()
		if err != nil {
			break
		}
		switch {
		case op == 0x20:
			x, err := scan.U32()
			if err != nil {
				return modularLoadGroup{}
			}
			if uint64(x)+uint64(f.localBase) == uint64(local) {
				if stage == 4 {
					stage = 2 // constant, local, add
				} else {
					stage = 1
				}
			} else {
				stage = 0
			}
		case op == 0x21 || op == 0x22:
			x, err := scan.U32()
			if err != nil || uint64(x)+uint64(f.localBase) == uint64(local) {
				return finishModularLoadGroup(local, first, mask)
			}
			stage = 0
		case op == 0x41:
			v, err := scan.I32()
			if err != nil {
				return modularLoadGroup{}
			}
			if uint32(v) < 256 && uint32(v)&3 == 0 {
				candidate = uint32(v)
				if stage == 1 {
					stage = 2 // local, constant, add
				} else {
					stage = 4
				}
			} else {
				stage = 0
			}
		case op == 0x6a:
			if stage == 2 {
				stage = 3
			} else {
				stage = 0
			}
		case op >= 0x28 && op <= 0x35:
			align, err := scan.U32()
			if err != nil {
				return modularLoadGroup{}
			}
			mem := uint32(0)
			if align >= 64 && align < 128 {
				mem, err = scan.U32()
				if err != nil {
					return modularLoadGroup{}
				}
			}
			off, err := scan.U32()
			if err != nil || mem != 0 {
				return finishModularLoadGroup(local, first, mask)
			}
			if op == 0x28 && off == 0 && (stage == 1 || stage == 3) {
				if stage == 1 {
					candidate = 0
				}
				mask |= uint64(1) << (candidate / 4)
			}
			stage = 0
		case op == 0x01 || op == 0x1a || op == 0x1b,
			op >= 0x45 && op <= 0x6c, op >= 0x71 && op <= 0x7e,
			op >= 0x83 && op <= 0x8a, op == 0xa7 || op == 0xac || op == 0xad:
			stage = 0
		case op == 0x42:
			if skipImmediates(&scan, op) != nil {
				return modularLoadGroup{}
			}
			stage = 0
		default:
			return finishModularLoadGroup(local, first, mask)
		}
	}
	return finishModularLoadGroup(local, first, mask)
}

func finishModularLoadGroup(local uint32, first uint8, mask uint64) modularLoadGroup {
	// Setup needs several checks to amortize; short groups keep the scalar checks.
	if bits.OnesCount64(mask) < 5 {
		return modularLoadGroup{}
	}
	lo, hi := bits.TrailingZeros64(mask), bits.Len64(mask)-1
	// Dense means that if the sequence crosses 2^32, one access ends in the
	// final four bytes of memory; this makes the wrap case exactly testable.
	count := hi - lo + 1
	if count == 64 && mask != ^uint64(0) || count < 64 && mask>>lo != (uint64(1)<<count)-1 {
		return modularLoadGroup{}
	}
	return modularLoadGroup{local: local, first: first, low: uint8(lo * 4), high: uint8(hi * 4), valid: true}
}

func (f *fn) modularLoadGroupCovers(local uint32, displacement uint8) bool {
	if f.boundsCertSources&boundsCertSourceBit(3, local) == 0 {
		return false
	}
	for i := range f.boundsCerts {
		c := &f.boundsCerts[i]
		if c.kind == 3 && c.idx == local {
			lo, hi := uint8(c.extent), uint8(uint32(c.extent)>>8)
			return displacement >= lo && displacement <= hi && (displacement-lo)&3 == 0
		}
	}
	return false
}

func (f *fn) addModularLoadGroup(g modularLoadGroup) {
	f.boundsCertSources |= boundsCertSourceBit(3, g.local)
	slot := -1
	for i := range f.boundsCerts {
		c := &f.boundsCerts[i]
		if c.kind == 3 && c.idx == g.local {
			slot = i
			break
		}
		if slot < 0 && c.kind == 0 {
			slot = i
		}
	}
	if slot < 0 {
		slot = int(f.nextBoundsCert) % len(f.boundsCerts)
		f.nextBoundsCert++
	}
	f.boundsCerts[slot] = boundsCert{kind: 3, idx: g.local, extent: int32(g.low) | int32(g.high)<<8}
}

// checkModularLoadGroup tests the exact set of adjacent word accesses. With no
// dynamic wrap, the highest word suffices. If the sequence wraps, a valid set
// requires full 4-GiB memory and word-aligned addresses; otherwise a member of
// the dense set traps. No source state or memory is changed by this check.
func (f *fn) checkModularLoadGroup(ea Reg, g modularLoadGroup) {
	f.pinned = f.pinned.add(ea)
	low := f.allocReg(0)
	f.a.MovRegReg32(low, ea)
	if delta := int32(g.low) - int32(g.first); delta != 0 {
		f.a.AluRI(0, low, delta, false)
	}
	high := f.allocReg(maskOf(low))
	limit := f.allocReg(maskOf(low, high))
	f.a.LeaDisp(high, low, int32(g.high-g.low)+4)
	// The common case needs no explicit 4-GiB test: if the widened end is
	// within memory, none of the grouped dynamic addresses wrapped.
	f.a.Cmp64(high, f.memSizeReg)
	done := f.a.JccPlaceholder(condBE)
	f.a.MovImm64(limit, 1<<32)
	f.a.Cmp64(f.memSizeReg, limit)
	f.trapIf(condNE, trapMemOOB)
	f.a.AluRI(5, high, 4, true)
	f.a.Cmp64(high, limit)
	f.trapIf(condB, trapMemOOB)
	f.a.TestImm(low, 3, false)
	f.trapIf(condNE, trapMemOOB)
	f.a.PatchRel32(done, f.a.Len())
	f.release(limit)
	f.release(high)
	f.release(low)
	f.pinned = f.pinned.remove(ea)
	f.stats.peep("modular-load-group")
}
