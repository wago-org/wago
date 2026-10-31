package shared

import (
	"fmt"
	"testing"
)

var regMoveBenchSink uint8

func BenchmarkResolveRegMoves(b *testing.B) {
	for _, n := range []int{8, 32} {
		for _, shape := range []string{"forward-chain", "reverse-chain", "cycle"} {
			b.Run(fmt.Sprintf("%s/n%d", shape, n), func(b *testing.B) {
				moveAt := func(i int) (uint8, uint8) {
					switch shape {
					case "cycle":
						return uint8(i), uint8((i + 1) % n)
					case "reverse-chain":
						if i == 0 {
							return 0, 63
						}
						return uint8(i), uint8(i - 1)
					default:
						return uint8(i), uint8(i + 1)
					}
				}
				emit := func(d, s uint8) { regMoveBenchSink ^= d ^ s }
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ResolveRegMoves(n, moveAt, emit, emit)
				}
			})
		}
	}
}

func TestResolveRegMovesSimultaneousAssignments(t *testing.T) {
	for n := 1; n <= 64; n++ {
		for seed := uint64(1); seed <= 1000; seed++ {
			var pairs [64][2]uint8
			var destinations [64]uint8
			var got, want [64]int
			x := seed
			next := func() uint64 { x = x*6364136223846793005 + 1; return x >> 32 }
			for i := range got {
				got[i] = i
				want[i] = i
				destinations[i] = uint8(i)
			}
			for i := 63; i > 0; i-- {
				j := int(next() % uint64(i+1))
				destinations[i], destinations[j] = destinations[j], destinations[i]
			}
			for i := 0; i < n; i++ {
				// Alternate between closed graphs (many cycles) and external sources.
				source := uint8(next() % 64)
				if seed%2 == 0 {
					source = destinations[int(source)%n]
				}
				pairs[i] = [2]uint8{destinations[i], source}
				want[pairs[i][0]] = got[source]
			}
			ResolveRegMoves(n, func(i int) (uint8, uint8) { return pairs[i][0], pairs[i][1] },
				func(d, s uint8) { got[d] = got[s] }, func(d, s uint8) { got[d], got[s] = got[s], got[d] })
			if got != want {
				t.Fatalf("n=%d seed=%d got=%v want=%v", n, seed, got, want)
			}
		}
	}
}
