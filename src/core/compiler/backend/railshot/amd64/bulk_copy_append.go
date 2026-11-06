//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// A short left-shift copy followed by an adjacent byte append can write complete
// vectors. Loading the original destination vectors and aligning them in registers
// avoids unaligned reads across the previous iteration's separate stores. The
// enclosing span must be proved in bounds and unshared: the extra first-byte
// read must neither trap nor observe a concurrent writer.
func (f *fn) tryBulkCopyAppend(r *wasm.Reader, n int) (bool, error) {
	if !f.opt(optBulkCopyVector) || !f.opt(optMemoryMinimumBounds) || n < 15 || n > 63 || n%16 != 15 ||
		f.memoryAddr64(0) || f.memoryShared(0) ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.moduleEH || f.gcFrameRoots != nil ||
		profileEnabled && f.stats != nil && f.stats.RecordSources {
		return false, nil
	}
	count := f.s.back()
	if count == nil || !count.isValue() {
		return false, nil
	}
	src := count.prev
	dst := src.prev
	if !src.isValue() || !dst.isValue() || src.st.kind != stConst || dst.st.kind != stConst ||
		src.st.typ != mtI32 || dst.st.typ != mtI32 {
		return false, nil
	}
	base := uint64(uint32(dst.st.cval))
	if uint64(uint32(src.st.cval)) != base+1 || base+uint64(n)+1 > 0x7fffffff {
		return false, nil
	}
	mt, ok := f.memoryType(0)
	if !ok || !shared.ProvenMinimumAccess(mt, base, 0, n+1) {
		return false, nil
	}

	scan := *r
	op, err := scan.Byte()
	if err != nil || op != 0x41 {
		return false, nil
	}
	address, err := scan.I32()
	if err != nil {
		return false, nil
	}
	expression := scan
	depth, operations, expressionEnd := 0, 0, 0
	for {
		pc := scan.Offset()
		op, err = scan.Byte()
		if err != nil {
			return false, nil
		}
		if op == 0x3a {
			if depth != 1 {
				return false, nil
			}
			align, err := scan.U32()
			if err != nil || align != 0 {
				return false, nil
			}
			offset, err := scan.U32()
			if err != nil || uint64(uint32(address))+uint64(offset) != base+uint64(n) {
				return false, nil
			}
			expressionEnd = pc
			break
		}
		operations++
		if operations > 12 {
			return false, nil
		}
		switch op {
		case 0x41:
			if _, err := scan.I32(); err != nil {
				return false, nil
			}
			depth++
		case 0x20:
			x, err := scan.U32()
			local := int(x) + f.localBase
			if err != nil || local < 0 || local >= len(f.localType) || f.localType[local] != mtI32 {
				return false, nil
			}
			depth++
		case 0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73:
			if depth < 2 {
				return false, nil
			}
			depth--
		default:
			return false, nil
		}
		if depth > 4 {
			return false, nil
		}
	}
	f.materializePendingLoads()
	blocked := f.fpinned.union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
	free := 0
	for reg := Reg(0); reg < 16; reg++ {
		if !blocked.has(reg) && f.fregUser[reg] == nil {
			free++
		}
	}
	groups := (n + 1) / 16
	if free < groups || !f.cpuHas(shared.AMD64AVX|shared.AMD64SSSE3|shared.AMD64SSE41) {
		return false, nil
	}
	f.erase(f.s.back())
	f.erase(f.s.back())
	f.erase(f.s.back())
	// Use the proved arithmetic subset: the general dispatcher also includes
	// callbacks that make the expression reader escape to the heap. The lookahead admits
	// only non-trapping reads/arithmetic, so evaluating it before the copy cannot
	// change memory effects or trap ordering.
	for expression.Offset() < expressionEnd {
		op, _ := expression.Byte()
		switch op {
		case 0x41:
			v, _ := expression.I32()
			f.s.pushIntegerConstant(mtI32, int64(v))
		case 0x20:
			x, _ := expression.U32()
			x = uint32(int(x) + f.localBase)
			if regallocCheckEnabled {
				checkNativeSourceGet(f, x)
			}
			f.pushPlainLocalGet(x)
		default:
			f.pushBinOp(integerBinaryOps[op-0x6a], mtI32)
		}
	}
	value, owned := f.materializeRead(f.popValue())
	f.pinned = f.pinned.add(value)
	var vectors [4]Reg
	var avoid regMask
	for i := 0; i < groups; i++ {
		vectors[i] = f.allocFReg(avoid)
		avoid = avoid.add(vectors[i])
		f.mov128LoadIdx(vectors[i], RBX, RSP, int32(base)+int32(i*16))
	}
	for i := 0; i+1 < groups; i++ {
		f.a.VPAlignr(vectors[i], vectors[i+1], vectors[i], 1)
	}
	last := vectors[groups-1]
	f.a.VexShiftImm(0x73, 3, last, last, 1) // VPSRLDQ: discard the first byte
	f.a.Pinsrb(last, value, 15)
	for i := 0; i < groups; i++ {
		f.mov128StoreIdx(RBX, RSP, vectors[i], int32(base)+int32(i*16))
		f.releaseF(vectors[i])
	}
	f.pinned = f.pinned.remove(value)
	if owned {
		f.release(value)
	}
	f.invalidateStoreForward()
	// Advance only the position: assigning the whole reader would make its
	// input buffer escape even when the caller uses the dynamic-copy fallback.
	_, _ = r.Bytes(scan.Offset() - r.Offset())
	f.stats.peep("bulk-copy-append")
	return true, nil
}
