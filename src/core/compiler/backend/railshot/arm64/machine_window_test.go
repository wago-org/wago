//go:build arm64

package arm64

import (
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"testing"
)

var benchmarkMachineRuleMatch machineRuleMatch

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

func TestGeneratedMachineRulesCurrentArm64(t *testing.T) {
	source, err := os.ReadFile("machine_rules.rules")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != machineRuleSourceSHA256 {
		t.Fatalf("machine_rules_gen.go is stale: source hash %s, generated hash %s; run go generate", got, machineRuleSourceSHA256)
	}
}

func TestMachineSwapChainRuleExhaustiveArm64(t *testing.T) {
	const scratch Reg = 4
	for a := Reg(0); a < scratch; a++ {
		for b := Reg(0); b < scratch; b++ {
			for c := Reg(0); c < scratch; c++ {
				first := machineOp{kind: machineSwap, dst: a, src: b}
				second := machineOp{kind: machineSwap, dst: b, src: c}
				match := matchMachinePair(first, second)
				wantMatch := a != c
				if got := match.id == machineRuleSwapChain3; got != wantMatch {
					t.Fatalf("match swap(%d,%d), swap(%d,%d) = %v, want %v", a, b, b, c, got, wantMatch)
				}
				if !wantMatch {
					continue
				}
				before := [5]int{0, 1, 2, 3, -1}
				original, rewritten := before, before
				original[a], original[b] = original[b], original[a]
				original[b], original[c] = original[c], original[b]
				rewritten[scratch] = rewritten[match.a]
				rewritten[match.a] = rewritten[match.b]
				rewritten[match.b] = rewritten[match.c]
				rewritten[match.c] = rewritten[scratch]
				for r := Reg(0); r < scratch; r++ {
					if rewritten[r] != original[r] {
						t.Fatalf("swap(%d,%d), swap(%d,%d): rewritten %v, original %v", a, b, b, c, rewritten, original)
					}
				}
			}
		}
	}

	nearMisses := [][2]machineOp{
		{{kind: machineMove, dst: 0, src: 1}, {kind: machineSwap, dst: 1, src: 2}},
		{{kind: machineSwap, dst: 0, src: 1}, {kind: machineMove, dst: 1, src: 2}},
		{{kind: machineSwap, dst: 0, src: 1}, {kind: machineSwap, dst: 2, src: 3}},
	}
	for _, pair := range nearMisses {
		if got := matchMachinePair(pair[0], pair[1]).id; got != machineRuleNone {
			t.Fatalf("near miss matched %s", got)
		}
	}
}

func BenchmarkMachineRuleMatchArm64(b *testing.B) {
	pairs := [...]struct{ first, second machineOp }{
		{machineOp{kind: machineSwap, dst: 0, src: 1}, machineOp{kind: machineSwap, dst: 1, src: 2}},
		{machineOp{kind: machineMove, dst: 0, src: 1}, machineOp{kind: machineSwap, dst: 1, src: 2}},
		{machineOp{kind: machineSwap, dst: 2, src: 3}, machineOp{kind: machineMove, dst: 3, src: 4}},
		{machineOp{kind: machineSwap, dst: 4, src: 5}, machineOp{kind: machineSwap, dst: 6, src: 7}},
	}
	b.Run("generated", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pair := pairs[i&(len(pairs)-1)]
			benchmarkMachineRuleMatch = matchMachinePair(pair.first, pair.second)
		}
	})
	b.Run("direct-condition", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pair := pairs[i&(len(pairs)-1)]
			first, second := pair.first, pair.second
			match := machineRuleMatch{}
			if first.kind == machineSwap && second.kind == machineSwap && first.src == second.dst && first.dst != second.src {
				match = machineRuleMatch{id: machineRuleSwapChain3, a: first.dst, b: first.src, c: second.src}
			}
			benchmarkMachineRuleMatch = match
		}
	})
}
