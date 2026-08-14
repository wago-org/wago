//go:build amd64

package amd64

import (
	"reflect"
	"testing"
)

func TestMachineWindowIdentityAndCapacityAMD64(t *testing.T) {
	moves := make([]regMove, 30)
	for i := range moves {
		moves[i] = regMove{dst: Reg(i), src: Reg(63)}
	}
	collect := func(window bool) []machineOp {
		var got []machineOp
		move := func(dst, src Reg) { got = append(got, machineOp{kind: machineMove, dst: dst, src: src}) }
		swap := func(a, b Reg) { got = append(got, machineOp{kind: machineSwap, dst: a, src: b}) }
		if window {
			resolveRegMovesWindow(moves, move, swap)
		} else {
			resolveRegMoves(moves, move, swap)
		}
		return got
	}
	before, after := collect(false), collect(true)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("window changed resolved operations:\nbefore=%v\nafter=%v", before, after)
	}
	if len(after) != 30 {
		t.Fatalf("resolved operations = %d, want 30 across capacity flush", len(after))
	}
}
