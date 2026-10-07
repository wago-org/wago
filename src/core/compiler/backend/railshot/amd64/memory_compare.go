//go:build amd64

package amd64

import (
	"os"
	"runtime"
)

// Default only on the platform qualified with native corpus measurements.
var memoryCompareEnabled = runtime.GOOS == "linux" && os.Getenv("WAGO_AMD64_MEMORY_COMPARE") != "0"

// memoryCompareImmediate admits constants in the image of the load's extension.
// This preserves equality and ordering when the comparison uses the access width.
func memoryCompareImmediate(st storage, constant int64, wide bool, cc Cond) (int32, Cond, bool) {
	size := st.memSize()
	if size != 1 && size != 2 && size != 4 && size != 8 {
		return 0, 0, false
	}
	targetSize := 4
	value := uint64(constant)
	if wide {
		targetSize = 8
	} else {
		value = uint64(uint32(value))
	}
	if size > targetSize {
		return 0, 0, false
	}
	if size == 8 {
		if !fitsImm32(int64(value)) {
			return 0, 0, false
		}
	} else {
		mask := uint64(1)<<(size*8) - 1
		extended := value & mask
		if st.memSigned() && extended&(uint64(1)<<(size*8-1)) != 0 {
			extended |= ^mask
		}
		if !wide {
			extended = uint64(uint32(extended))
		}
		if extended != value {
			return 0, 0, false
		}
	}
	if !st.memSigned() && size < targetSize {
		// Both extended values are nonnegative at the Wasm width. At the access
		// width, their high bit is data rather than a sign bit.
		switch cc {
		case condL:
			cc = condB
		case condLE:
			cc = condBE
		case condG:
			cc = condA
		case condGE:
			cc = condAE
		}
	}
	return int32(value), cc, true
}

func (f *fn) tryMemoryCompareToFlags(node *elem) (Cond, bool) {
	if !f.opt(optMemoryCompareImmediate) || node == nil || !node.isDeferred() {
		return 0, false
	}
	left := node.arg0
	if left == nil || !left.isValue() || left.st.kind != stMemRef {
		return 0, false
	}
	constant, cc := int64(0), condE
	if node.deferredOp() != opEqz {
		if !isCompare(node.deferredOp()) || node.arg1 == nil || !node.arg1.isValue() || node.arg1.st.kind != stConst {
			return 0, false
		}
		constant, cc = node.arg1.st.cval, condOf(node.deferredOp())
	}
	imm, cc, ok := memoryCompareImmediate(left.st, constant, node.valueType().is64(), cc)
	if !ok {
		return 0, false
	}
	// Address preparation already enforced the original access's bounds. Keep
	// that exact address and width, and consume the address owner only once.
	f.a.CmpImmIdx(RBX, left.st.reg, left.st.memDisp(), imm, left.st.memSize())
	f.releaseMemRef(left.st)
	f.consumeBlockBelow(node)
	f.stats.peep("memory-compare-immediate")
	return cc, true
}
