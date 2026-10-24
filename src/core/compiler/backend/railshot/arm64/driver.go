//go:build arm64

package arm64

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func (f *fn) setRepresentationLimit(limit functionRepresentationLimit) {
	if f.representationLimit == functionRepresentationOK {
		f.representationLimit = limit
	}
}

var errNativeBranchRange = errors.New("arm64: native branch displacement exceeds instruction range")

func (f *fn) representationError() error {
	var field string
	switch f.representationLimit {
	case functionRepresentationReturnSite:
		field = "return-site branch chain"
	case functionRepresentationFrameEnd:
		field = "forward control-end offset"
	case functionRepresentationCallReloc:
		field = "call relocation"
	case functionRepresentationBranchRange:
		return errNativeBranchRange
	case functionRepresentationADRRange:
		return fmt.Errorf("arm64: native PC-relative address exceeds instruction range")
	default:
		field = "unknown field"
	}
	return fmt.Errorf("arm64: function %s exceeds compact representation limit", field)
}

func (f *fn) tryFsignLocalSet(r *wasm.Reader, f64, neg bool) (bool, error) {
	return f.tryFscalarLocalSet(r, f64, 1, func(dst Reg) {
		f.fsignInto(dst, f64, neg)
		f.stats.peep("float-sign-local-sink")
	})
}

func (f *fn) tryFscalarLocalSet(r *wasm.Reader, f64 bool, arity int, emit func(Reg)) (bool, error) {
	save := r.Offset()
	op, ok := r.Peek()
	if !ok || (op != 0x21 && op != 0x22) {
		return false, nil
	}
	if _, err := r.Byte(); err != nil {
		return false, err
	}
	x32, err := r.U32()
	if err != nil {
		return false, err
	}
	x := int(x32) + f.localBase
	pr, isFloat, pinned := f.pinReg(x)
	if !pinned || !isFloat || x < 0 || x >= len(f.localType) || f.localType[x] != mtOf2(f64) {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}
	first := f.s.back()
	if first == nil {
		if err := r.JumpTo(save); err != nil {
			return false, err
		}
		return false, nil
	}
	if arity == 2 {
		first = f.s.prev(f.s.baseOfValentBlock(first))
	}
	f.invalidateBoundsCertFor(1, uint32(x))
	f.realizeLocalRefs(x, first)
	emit(pr)
	f.markLocalDirty(x)
	result := f.s.back()
	if op == 0x22 {
		f.replaceStorage(result, storage{kind: stLocalReg, typ: f.localType[x], reg: pr, idx: uint32(x)})
	} else {
		f.erase(result)
	}
	return true, nil
}
