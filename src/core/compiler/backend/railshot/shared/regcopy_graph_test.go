package shared

import "testing"

func TestRegMoveGraphSimultaneousAssignments(t *testing.T) {
	type request struct{ dst, src uint8 }
	var graph RegMoveGraph[uint8]
	check := func(moves []request) {
		t.Helper()
		var initial [64]uint64
		for i := range initial {
			initial[i] = uint64(i)*17 + 101
		}
		want := initial
		for _, m := range moves {
			want[m.dst] = initial[m.src]
		}
		actual := initial
		graph.Reset()
		for _, m := range moves {
			graph.Add(m.dst, m.src)
		}
		steps := 0
		for {
			dst, src, swap, ok := graph.Next()
			if !ok {
				break
			}
			steps++
			if steps > 128 {
				t.Fatal("move graph did not terminate")
			}
			if swap {
				actual[dst], actual[src] = actual[src], actual[dst]
			} else {
				actual[dst] = actual[src]
			}
		}
		if actual != want {
			t.Fatalf("graph assignment mismatch for %v", moves)
		}
		actual = initial
		ResolveRegMoves(len(moves), func(i int) (uint8, uint8) { return moves[i].dst, moves[i].src }, func(dst, src uint8) { actual[dst] = actual[src] }, func(a, b uint8) { actual[a], actual[b] = actual[b], actual[a] })
		if actual != want {
			t.Fatalf("callback adapter assignment mismatch for %v", moves)
		}
	}
	check(nil)
	// Exhaust graphs with written destinations 0..n-1 and sources 0..n,
	// including fan-out, chains, cycles, self moves and an unwritten source.
	for n := 1; n <= 5; n++ {
		combinations := 1
		for i := 0; i < n; i++ {
			combinations *= n + 1
		}
		moves := make([]request, n)
		for code := 0; code < combinations; code++ {
			value := code
			for i := range moves {
				moves[i] = request{uint8(i), uint8(value % (n + 1))}
				value /= n + 1
			}
			check(moves)
		}
	}
	check([]request{{63, 62}, {62, 63}, {1, 63}, {0, 1}})
	// Reuse after abandoning a partially consumed cycle must drop pending work.
	graph.Reset()
	graph.Add(0, 1)
	graph.Add(1, 0)
	graph.Next()
	graph.Reset()
	graph.Add(63, 62)
	dst, src, swap, ok := graph.Next()
	if !ok || swap || dst != 63 || src != 62 {
		t.Fatal("reset retained an abandoned graph")
	}
	if _, _, _, ok = graph.Next(); ok {
		t.Fatal("reset retained pending operations")
	}
}
