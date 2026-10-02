//go:build arm64 && wago_regalloccheck

package arm64

import (
	"fmt"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
	"testing"
)

func TestRegallocCheckSeedsSemanticResultWidths(t *testing.T) {
	cases := []struct {
		op         wOp
		input, typ machineType
		want       int
	}{
		{opEq, mtI64, mtI64, 4}, {opNe, mtI64, mtI64, 4},
		{opLtS, mtI64, mtI64, 4}, {opLtU, mtI64, mtI64, 4},
		{opGtS, mtI64, mtI64, 4}, {opGtU, mtI64, mtI64, 4},
		{opLeS, mtI64, mtI64, 4}, {opLeU, mtI64, mtI64, 4},
		{opGeS, mtI64, mtI64, 4}, {opGeU, mtI64, mtI64, 4},
		{opEqz, mtI64, mtI64, 4},
		{opEq, mtI32, mtI32, 4}, {opEqz, mtI32, mtI32, 4},
		{opWrap, mtI64, mtI32, 4},
		{opSExt32, mtI32, mtI64, 8}, {opZExt32, mtI32, mtI64, 8},
		{opSExt8, mtI32, mtI32, 4}, {opSExt16, mtI32, mtI32, 4},
		{opSExt8, mtI64, mtI64, 8}, {opSExt16, mtI64, mtI64, 8},
		{opSExt32, mtI64, mtI64, 8},
		{opClz, mtI64, mtI64, 8}, {opAdd, mtI64, mtI64, 8},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("op%d/input%d/type%d", tc.op, tc.input, tc.typ), func(t *testing.T) {
			f := fn{a: &encoder.Asm{}, s: newStack(), globalCellReg: regNone}
			input := f.pushValue(storage{kind: stSlot, typ: tc.input, slot: 3})
			if isCompare(tc.op) || isBinALU(tc.op) {
				f.pushValue(storage{kind: stSlot, typ: tc.input, slot: 4})
				f.pushBinOp(tc.op, tc.typ)
			} else {
				f.pushUnOp(tc.op, tc.typ)
			}
			root := f.s.back()
			f.checkBeginFlush([]*elem{root})
			defer f.a.ObserveRegalloc(nil)
			if got := len(f.allocationCheck.values[root]); got != tc.want {
				t.Errorf("result identity = %d bytes, want %d", got, tc.want)
			}
			if got, want := len(f.allocationCheck.values[input]), checkSize(tc.input); got != want {
				t.Errorf("input identity = %d bytes, want %d", got, want)
			}
		})
	}
}
