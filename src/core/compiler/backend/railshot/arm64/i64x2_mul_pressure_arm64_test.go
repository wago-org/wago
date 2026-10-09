//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// Match the transient capacity of 26 vector-local pins plus one cached constant.
// The lowering must leave both inputs live until their last reads.
func TestI64x2MulFiveTransientRegisters(t *testing.T) {
	f := &fn{a: &a64.Asm{}, s: newStack()}
	for r := Reg(5); r < 31; r++ {
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(r)
	}
	f.vconsts = []v128ConstReg{{lo: 1, reg: 31}}
	for _, r := range []Reg{0, 1} {
		v := f.pushValue(storage{kind: stReg, typ: mtV128, reg: r})
		f.fregUser[r] = v
	}
	if err := f.i64x2Mul(wasm.NewReader(nil)); err != nil {
		t.Fatal(err)
	}
	if f.depth() != 1 || f.s.back().st.typ != mtV128 || len(f.a.B) == 0 {
		t.Fatal("missing vector result")
	}
}
