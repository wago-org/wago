//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var arithmeticTeeBranchEnabled = os.Getenv("WAGO_ARM64_NO_ARITHMETIC_TEE_BRANCH") != "1"

// Keep the ordinary assignment and branch implementations, including regional
// local convergence. Only omit the branch's zero comparison when the native
// arithmetic result and its flags survive every intervening instruction.
func (f *fn) tryArithmeticTeeBranch(r *wasm.Reader, x int) (bool, error) {
	if !f.opt(optArithmeticTeeBranch) || f.unreachable || x < 0 || x >= len(f.localType) || f.localType[x] != mtI32 || f.depth() != 1 || len(f.ctrl) == 0 {
		return false, nil
	}
	loop := &f.ctrl[len(f.ctrl)-1]
	if loop.kind != cfLoop || loop.paramN != 0 || loop.resultN != 0 {
		return false, nil
	}
	top := f.s.back()
	if top == nil || !top.isDeferred() || (top.deferredOp() != opAdd && top.deferredOp() != opSub) {
		return false, nil
	}
	if _, isFloat, ok := f.pinReg(x); !ok || isFloat {
		return false, nil
	}
	scan := *r
	op, err := scan.Byte()
	if err != nil || op != 0x0d {
		return false, nil
	}
	label, err := scan.U32()
	if err != nil || label != 0 {
		return false, nil
	}
	f.setLocal(r, x, true)
	flagsAt := f.a.Len() - 4
	if _, err := r.Byte(); err != nil {
		return false, err
	}
	return true, f.opBrWithArithmeticFlags(r, true, flagsAt)
}

func arithmeticZeroWord(w uint32, dst Reg) bool {
	if w&31 != uint32(dst) || dst == SP {
		return false
	}
	switch w & 0xff000000 {
	case 0x11000000, 0x51000000:
		return true
	}
	switch w & 0xff200000 {
	case 0x0b000000, 0x4b000000:
		return true
	}
	return false
}

func arithmeticFlagGapWord(w uint32) (writes uint32, ok bool) {
	// Scalar/vector unsigned-offset memory operations have no writeback or NZCV
	// effect. Integer loads write Rt; FP/vector loads write only the vector bank.
	if w&0x3b000000 == 0x39000000 && w>>22&3 <= 1 {
		if w&(1<<26) == 0 && w&(1<<22) != 0 {
			return uint32(1) << (w & 31) & 0x7fffffff, true
		}
		return 0, true
	}
	// A scalar FPR copy changes neither the integer result nor flags.
	if w&0xffbffc00 == 0x1e204000 {
		return 0, true
	}
	x, ok := guardedIntegerWord(w)
	return x.writes, ok && !x.flags && !x.readsFlags
}

func (f *fn) reuseArithmeticZeroFlags(at int, dst Reg) bool {
	if at < 0 || at+4 > f.a.Len() || f.a.Len()-at > 128 {
		return false
	}
	w := rdWord(f.a.B, at)
	if !arithmeticZeroWord(w, dst) {
		return false
	}
	for pc := at + 4; pc < f.a.Len(); pc += 4 {
		writes, ok := arithmeticFlagGapWord(rdWord(f.a.B, pc))
		if !ok || writes&(uint32(1)<<uint32(dst)) != 0 {
			return false
		}
	}
	// Change NZCV only after the complete no-reader/no-writer proof succeeds.
	wrWord(f.a.B, at, w|1<<29)
	f.stats.peep("arithmetic-tee-branch")
	return true
}
