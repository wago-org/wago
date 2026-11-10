//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64IntegerUnaryLanesAndReductions(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 2)
	rng := rand.New(rand.NewSource(64096196))
	for _, group := range []struct {
		base, width int
	}{{96, 1}, {128, 2}, {160, 4}, {192, 8}} {
		for _, operation := range []struct {
			offset int
			name   string
		}{{0, "abs"}, {1, "neg"}, {3, "all_true"}, {4, "bitmask"}} {
			t.Run(fmt.Sprintf("bytes%d/%s", group.width, operation.name), func(t *testing.T) {
				scalar := operation.offset >= 3
				fixture := swarUnaryFixture(uint32(group.base+operation.offset), scalar)
				fixture.locals = []byte{1, 127, 0x7b}
				fn, ft := swarValidatedFunction(t, fixture)
				code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
				if err != nil || info.FrameBytes <= 2048 {
					t.Fatal("large unary vector frame", info, err)
				}
				entry := thunkImageRV64(t, code)
				lanes := 16 / group.width
				for sample := 0; sample < 64; sample++ {
					var input [16]byte
					_, _ = rng.Read(input[:])
					if sample < 4 {
						clear(input[:])
						for start := 0; start < 16; start += group.width {
							switch sample {
							case 1:
								for i := start; i < start+group.width; i++ {
									input[i] = 255
								}
							case 2:
								input[start+group.width-1] = 0x80
							case 3:
								for i := start; i < start+group.width; i++ {
									input[i] = 255
								}
								input[start+group.width-1] = 0x7f
							}
						}
					} else if sample < 4+lanes {
						// Exactly one zero lane, at every position in both halves.
						for i := range input {
							input[i] = 0x81
						}
						clear(input[(sample-4)*group.width : (sample-3)*group.width])
					} else if sample < 4+2*lanes {
						// Exactly one negative sign bit, at every lane index.
						clear(input[:])
						for start := 0; start < 16; start += group.width {
							input[start] = 1
						}
						input[(sample-4-lanes+1)*group.width-1] |= 0x80
					}
					var want [16]byte
					all, mask := uint32(1), uint32(0)
					for start := 0; start < 16; start += group.width {
						var value uint64
						for i := 0; i < group.width; i++ {
							value |= uint64(input[start+i]) << (8 * i)
						}
						if value == 0 {
							all = 0
						}
						negative := input[start+group.width-1]&0x80 != 0
						if negative {
							mask |= 1 << (start / group.width)
						}
						if operation.offset == 1 || (operation.offset == 0 && negative) {
							value = -value
						}
						for i := 0; i < group.width; i++ {
							want[start+i] = byte(value >> (8 * i))
						}
					}
					copy(args, input[:])
					for i := range results[:16] {
						results[i] = 0xa5
					}
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(args[:16], input[:]) {
						t.Fatal("unary entry/operand mutation", err)
					}
					if scalar {
						expect := mask
						if operation.offset == 3 {
							expect = all
						}
						if binary.LittleEndian.Uint64(results) != uint64(expect) || !bytes.Equal(results[8:16], bytes.Repeat([]byte{0xa5}, 8)) {
							t.Fatal("reduction normalization/compact result", input, results[:16], expect)
						}
					} else if !bytes.Equal(results[:16], want[:]) {
						t.Fatal("unary wrap/sign/lane isolation", input, results[:16], want)
					}
				}
				call := func() {
					if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
						panic(err)
					}
				}
				if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
					t.Fatal("unary native allocations", allocs)
				}
			})
		}
	}
}
