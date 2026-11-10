// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// Exact, non-overlapping tails avoid making a later narrow store overlap an
// earlier copy store. Every source chunk is loaded before any destination store.
func (f *fn) emitBulkCopyChunks(n int, base, src, dst Reg, srcDisp, dstDisp int32) bool {
	if !f.opt(optBulkCopyVector) || n < 16 || n > 64 {
		return false
	}
	type chunk struct {
		off, size int
		reg       Reg
	}
	var chunks [7]chunk
	count := 0
	for off := 0; off+16 <= n; off += 16 {
		chunks[count] = chunk{off: off, size: 16}
		count++
	}
	off := n / 16 * 16
	gpN := 0
	for _, size := range [...]int{8, 4, 2, 1} {
		if n-off >= size {
			chunks[count] = chunk{off: off, size: size}
			count++
			gpN++
			off += size
		}
	}
	gpBlock := maskOf(base, src, dst).union(f.pinned).union(f.pinnedLocalMask).union(f.reserved)
	fpBlock := f.fpinned.union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
	freeGP, freeFP := 0, 0
	for _, r := range gpAlloc {
		if !gpBlock.has(r) && f.regUser[r] == nil {
			freeGP++
		}
	}
	for r := Reg(0); r < 16; r++ {
		if !fpBlock.has(r) && f.fregUser[r] == nil {
			freeFP++
		}
	}
	if freeGP < gpN || freeFP < n/16 {
		return false
	}
	gpAvoid, fpAvoid := maskOf(base, src, dst), regMask(0)
	for i := 0; i < count; i++ {
		c := &chunks[i]
		if c.size == 16 {
			c.reg = f.allocFReg(fpAvoid)
			fpAvoid = fpAvoid.add(c.reg)
			f.mov128LoadIdx(c.reg, base, src, srcDisp+int32(c.off))
		} else {
			c.reg = f.allocReg(gpAvoid)
			gpAvoid = gpAvoid.add(c.reg)
			f.a.LoadIdx(c.reg, base, src, srcDisp+int32(c.off), c.size, false, c.size == 8)
		}
	}
	for _, c := range chunks[:count] {
		if c.size == 16 {
			f.mov128StoreIdx(base, dst, c.reg, dstDisp+int32(c.off))
			f.releaseF(c.reg)
		} else {
			f.a.StoreIdx(base, dst, c.reg, dstDisp+int32(c.off), c.size)
			f.release(c.reg)
		}
	}
	f.stats.peep("memcopy-exact-chunks")
	return true
}

// Fold pure memory32 address constants into displacements only when both spans
// fit the declared minimum. The normal checked lowering handles every other
// case, including large unsigned displacements and register pressure.
func (f *fn) emitFixedBulkCopy(n int, dstMemory, srcMemory uint32) bool {
	if !f.opt(optBulkCopyVector) || !f.opt(optMemoryMinimumBounds) || n < 16 || n > 64 || dstMemory != 0 || srcMemory != 0 || f.memoryAddr64(0) {
		return false
	}
	count := f.s.back()
	if count == nil || !count.isValue() {
		return false
	}
	src := count.prev
	dst := src.prev
	if !src.isValue() || !dst.isValue() || src.st.kind != stConst || dst.st.kind != stConst {
		return false
	}
	source, destination := uint64(uint32(src.st.cval)), uint64(uint32(dst.st.cval))
	if source+uint64(n) > 0x7fffffff || destination+uint64(n) > 0x7fffffff {
		return false
	}
	mt, ok := f.memoryType(0)
	if !ok || !shared.ProvenMinimumAccess(mt, source, 0, n) || !shared.ProvenMinimumAccess(mt, destination, 0, n) {
		return false
	}
	base, owned := f.bulkMemory0Base(0)
	handled := f.emitBulkCopyChunks(n, base, RSP, RSP, int32(source), int32(destination))
	if owned {
		f.release(base)
	}
	if !handled {
		return false
	}
	f.erase(f.s.back())
	f.erase(f.s.back())
	f.erase(f.s.back())
	f.stats.peep("memory-min-bounds")
	f.stats.peep("memory-min-bounds")
	f.stats.peep("memcopy-fixed-address")
	return true
}
