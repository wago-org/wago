//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"
)

// simulate runs resolveRegMoves against a register file where each register
// initially holds a tag equal to its own id, then returns the final contents.
// For a correct parallel move, every destination must end holding the tag of
// its requested source.
func simulate(t *testing.T, moves []regMove) map[Reg]Reg {
	t.Helper()
	reg := map[Reg]Reg{}
	touch := func(r Reg) {
		if _, ok := reg[r]; !ok {
			reg[r] = r // tag = initial register id
		}
	}
	for _, m := range moves {
		touch(m.dst)
		touch(m.src)
	}
	swaps := 0
	resolveRegMoves(moves,
		func(dst, src Reg) { reg[dst] = reg[src] },
		func(a, b Reg) { reg[a], reg[b] = reg[b], reg[a]; swaps++ },
	)
	return reg
}

func check(t *testing.T, name string, moves []regMove) {
	t.Helper()
	reg := simulate(t, moves)
	for _, m := range moves {
		if m.dst == m.src {
			continue
		}
		if reg[m.dst] != m.src {
			t.Errorf("%s: reg[%d] = %d, want tag %d (move %d<-%d)", name, m.dst, reg[m.dst], m.src, m.dst, m.src)
		}
	}
}

func TestResolveRegMoves(t *testing.T) {
	cases := []struct {
		name  string
		moves []regMove
	}{
		{"empty", nil},
		{"identity", []regMove{{RAX, RAX}}},
		{"independent", []regMove{{R8, RAX}, {R9, RCX}}},
		{"chain", []regMove{{RAX, RCX}, {RCX, RDX}}},
		{"chain_reversed_order", []regMove{{RCX, RDX}, {RAX, RCX}}},
		{"two_cycle", []regMove{{RAX, RCX}, {RCX, RAX}}},
		{"three_cycle", []regMove{{RAX, RCX}, {RCX, RDX}, {RDX, RAX}}},
		{"cycle_plus_chain", []regMove{{RAX, RCX}, {RCX, RAX}, {R8, RAX}}},
		{"fan_in_to_chain", []regMove{{R8, RAX}, {R9, RAX}, {RAX, RCX}}},
		{"two_independent_cycles", []regMove{{RAX, RCX}, {RCX, RAX}, {R8, R9}, {R9, R8}}},
	}
	for _, c := range cases {
		check(t, c.name, c.moves)
	}
}

// Property-style: a batch of random-ish permutations must all resolve correctly.
func TestResolveRegMovesPermutations(t *testing.T) {
	regs := []Reg{RAX, RCX, RDX, R8, R9, R10, R11}
	// Rotate the first k registers by one — a single k-cycle — for each k.
	for k := 2; k <= len(regs); k++ {
		var moves []regMove
		for i := 0; i < k; i++ {
			moves = append(moves, regMove{dst: regs[i], src: regs[(i+1)%k]})
		}
		check(t, fmt.Sprintf("rotate_%d", k), moves)
	}
}
