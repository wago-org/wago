//go:build amd64

package amd64

import (
	"reflect"
	"testing"

	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
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

func TestMachineWindowKeepsCompactXchgChainAMD64(t *testing.T) {
	moves := []regMove{{dst: RAX, src: RCX}, {dst: RCX, src: RDX}, {dst: RDX, src: RAX}}
	var encoded encoder.Asm
	resolveRegMovesWindow(moves,
		func(dst, src Reg) { encoded.MovReg64(dst, src) },
		func(a, b Reg) { encoded.Xchg64(a, b) })
	if got, want := len(encoded.B), 6; got != want {
		t.Fatalf("three-register cycle = %d bytes, want two 3-byte XCHGs (%d)", got, want)
	}

	var expanded encoder.Asm
	expanded.MovReg64(R11, RAX)
	expanded.MovReg64(RAX, RCX)
	expanded.MovReg64(RCX, RDX)
	expanded.MovReg64(RDX, R11)
	if got := len(expanded.B); got <= len(encoded.B) {
		t.Fatalf("four-MOV expansion = %d bytes, want greater than XCHG chain %d", got, len(encoded.B))
	}
}
