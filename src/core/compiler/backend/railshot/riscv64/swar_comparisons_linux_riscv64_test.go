//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"fmt"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64IntegerComparisonLaneMasks(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	rng := rand.New(rand.NewSource(640835219))
	// These predicates independently express the Wasm instruction table.
	predicates := []struct {
		name string
		fn   func(uint64, uint64, int) bool
	}{
		{"eq", func(a, b uint64, _ int) bool { return a == b }},
		{"ne", func(a, b uint64, _ int) bool { return a != b }},
		{"lt_s", func(a, b uint64, bits int) bool { return swarSignedLane(a, bits) < swarSignedLane(b, bits) }},
		{"lt_u", func(a, b uint64, _ int) bool { return a < b }},
		{"gt_s", func(a, b uint64, bits int) bool { return swarSignedLane(a, bits) > swarSignedLane(b, bits) }},
		{"gt_u", func(a, b uint64, _ int) bool { return a > b }},
		{"le_s", func(a, b uint64, bits int) bool { return swarSignedLane(a, bits) <= swarSignedLane(b, bits) }},
		{"le_u", func(a, b uint64, _ int) bool { return a <= b }},
		{"ge_s", func(a, b uint64, bits int) bool { return swarSignedLane(a, bits) >= swarSignedLane(b, bits) }},
		{"ge_u", func(a, b uint64, _ int) bool { return a >= b }},
	}
	for _, group := range []struct {
		base, width int
	}{{35, 1}, {45, 2}, {55, 4}, {214, 8}} {
		for op, predicate := range predicates {
			kind := group.base + op
			if group.width == 8 {
				// I64x2 supplies eq/ne and only signed order relations.
				switch op {
				case 0, 1, 2:
				case 4:
					kind = 217
				case 6:
					kind = 218
				case 8:
					kind = 219
				default:
					continue
				}
			}
			t.Run(fmt.Sprintf("bytes%d/%s", group.width, predicate.name), func(t *testing.T) {
				fixture := swarArithmeticFixture(uint32(kind))
				fixture.locals = []byte{1, 126, 0x7b}
				fn, ft := swarValidatedFunction(t, fixture)
				code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
				if err != nil || info.FrameBytes <= 2048 {
					t.Fatal("large vector comparison frame", kind, info, err)
				}
				entry := thunkImageRV64(t, code)
				for sample := 0; sample < 64; sample++ {
					var input [32]byte
					_, _ = rng.Read(input[:])
					if sample < 6 {
						clear(input[:])
						for start := 0; start < 16; start += group.width {
							switch sample {
							case 1:
								for i := start; i < start+group.width; i++ {
									input[i], input[16+i] = 255, 255
								}
							case 2:
								input[start+group.width-1] = 0x80
							case 3:
								input[16+start+group.width-1] = 0x80
							case 4:
								for i := start; i < start+group.width; i++ {
									input[i], input[16+i] = 255, 255
								}
								input[start+group.width-1] = 0x7f
							case 5:
								for i := start; i < start+group.width; i++ {
									input[i], input[16+i] = byte(start), byte(15-start)
								}
							}
						}
					} else if sample == 6 {
						copy(input[16:], input[:16])
					} else if sample == 7 {
						copy(input[16:], input[:16])
						for start := group.width; start < 16; start += 2 * group.width {
							input[16+start] ^= 1
						}
					}
					var want [16]byte
					for start := 0; start < 16; start += group.width {
						var a, b uint64
						for i := 0; i < group.width; i++ {
							a |= uint64(input[start+i]) << (8 * i)
							b |= uint64(input[16+start+i]) << (8 * i)
						}
						if predicate.fn(a, b, 8*group.width) {
							for i := start; i < start+group.width; i++ {
								want[i] = 255
							}
						}
					}
					copy(args, input[:])
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
						t.Fatal("comparison signedness/full lane mask", kind, sample, input, results[:16], want, err)
					}
				}
				call := func() {
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
						panic(err)
					}
				}
				if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
					t.Fatal("vector comparison native allocations", kind, allocs)
				}
			})
		}
	}
}

func swarSignedLane(value uint64, bits int) int64 {
	return int64(value<<(64-bits)) >> (64 - bits)
}
