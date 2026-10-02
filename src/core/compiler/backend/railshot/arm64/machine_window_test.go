//go:build arm64

package arm64

import (
	"reflect"
	"testing"
)

func TestMachineWindowIdentityAndCapacityArm64(t *testing.T) {
	moves := make([]regMove, 30)
	for i := range moves {
		moves[i] = regMove{dst: Reg(i), src: Reg(63)}
	}
	collect := func(window bool) []machineOp {
		var got []machineOp
		move := func(dst, src Reg) { got = append(got, machineOp{kind: machineMove, dst: dst, src: src}) }
		swap := func(a, b Reg) { got = append(got, machineOp{kind: machineSwap, dst: a, src: b}) }
		if window {
			resolveRegMovesWindow(moves, move, swap, nil)
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

func TestMachineWindowFusesThreeRegisterCycleArm64(t *testing.T) {
	moves := []regMove{{dst: 0, src: 1}, {dst: 1, src: 2}, {dst: 2, src: 0}}
	regs := [4]int{10, 20, 30, -1}
	move := func(dst, src Reg) { regs[dst] = regs[src] }
	swap := func(a, b Reg) { regs[a], regs[b] = regs[b], regs[a] }
	chain := func(a, b, c Reg) {
		const scratch Reg = 3
		move(scratch, a)
		move(a, b)
		move(b, c)
		move(c, scratch)
	}
	if got := resolveRegMovesWindow(moves, move, swap, chain); got != 1 {
		t.Fatalf("swap-chain rewrites = %d, want 1", got)
	}
	if want := [4]int{20, 30, 10, 10}; regs != want {
		t.Fatalf("registers = %v, want %v", regs, want)
	}
}
