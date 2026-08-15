//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// deadGCImmediateDrop recognizes only the one-instruction consumer that needs
// no retained constructor result. The copied-reader tree recognizer used by
// AMD64 remains a separate parity step; this first ARM64 slice has no fuel or
// recursive state because it peeks exactly one byte.
func (f *fn) deadGCImmediateDrop(r *wasm.Reader) bool {
	if !f.policy.EnabledOption(optGCDeadNew) {
		return false
	}
	op, ok := r.Peek()
	return ok && op == 0x1a
}

func (f *fn) reserveDeadGCStructConstructor(typeIndex uint32, operandN int) error {
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	if err := f.callGCStructHelper(gcStructReserveDead, []wasm.ValType{wasm.I32}, nil); err != nil {
		return err
	}
	f.discardGCConstructorOperands(operandN)
	return nil
}

func (f *fn) reserveDeadGCFixedArrayConstructor(typeIndex, count uint32) error {
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(typeIndex)})
	f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(count)})
	if err := f.callGCStructHelper(gcArrayCheckFixed, []wasm.ValType{wasm.I32, wasm.I32}, nil); err != nil {
		return err
	}
	f.discardGCConstructorOperands(int(count))
	return nil
}

func (f *fn) finishDeadGCImmediateDrop(r *wasm.Reader) {
	op, err := r.Byte()
	if err != nil || op != 0x1a {
		panic("arm64: dead GC constructor lost following drop")
	}
	f.stats.peep("gc-dead-new")
	f.stats.peep("gc-dead-new-checked")
}

func (f *fn) discardGCConstructorOperands(n int) {
	if n == 0 {
		return
	}
	roots := f.rootsBottomToTop()
	if n < 0 || n > len(roots) {
		panic("arm64: invalid dead GC constructor operand count")
	}
	dead := roots[len(roots)-n:]
	for _, root := range dead {
		if !f.treeDiscardable(root) {
			f.flush()
			for _, operand := range dead {
				f.erase(operand)
			}
			return
		}
	}
	for i := len(dead) - 1; i >= 0; i-- {
		f.discardTree(dead[i])
	}
}
