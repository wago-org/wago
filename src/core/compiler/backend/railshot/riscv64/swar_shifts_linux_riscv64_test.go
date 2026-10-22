//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func TestSWARRV64VectorShiftsWidthsAndRawCounts(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 3)
	rng := rand.New(rand.NewSource(64083163264))
	for _, kind := range []uint32{107, 108, 109, 139, 140, 141, 171, 172, 173, 203, 204, 205} {
		width, direction, _ := swarShiftOpcode(kind)
		laneBytes := int(width / 8)
		fixture := swarShiftFixture(kind)
		fixture.locals = []byte{1, 126, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large vector shift frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		for _, count := range []uint32{0, 1, uint32(width) - 1, uint32(width), uint32(width) + 1, 2*uint32(width) - 1, 31, 32, 63, 64, 0x80000000, 0xffffffff} {
			for sample := 0; sample < 16; sample++ {
				var input, want [16]byte
				_, _ = rng.Read(input[:])
				if sample < 5 {
					clear(input[:])
					for start := 0; start < 16; start += laneBytes {
						switch sample {
						case 1:
							for i := start; i < start+laneBytes; i++ {
								input[i] = 255
							}
						case 2:
							input[start+laneBytes-1] = 0x80
						case 3:
							input[start] = 1
						case 4:
							for i := start; i < start+laneBytes; i++ {
								input[i] = 255
							}
							input[start+laneBytes-1] = 0x7f
						}
					}
				} else if sample == 5 {
					binary.LittleEndian.PutUint64(input[:8], 0x7ff0000000000001)
					binary.LittleEndian.PutUint64(input[8:], 0x8000000000000000)
				}
				// Decode bytes to a scalar lane, apply Wasm modulo shift
				// semantics, then write only this lane's bytes back.
				amount := count % uint32(width)
				for start := 0; start < 16; start += laneBytes {
					var value uint64
					for i := 0; i < laneBytes; i++ {
						value |= uint64(input[start+i]) << (8 * i)
					}
					switch direction {
					case 0:
						value <<= amount
					case 1:
						value = uint64((int64(value<<(64-width)) >> (64 - width)) >> amount)
					case 2:
						value >>= amount
					}
					for i := 0; i < laneBytes; i++ {
						want[start+i] = byte(value >> (8 * i))
					}
				}
				copy(args, input[:])
				rawCount := uint64(count) | 0xdeadbeef00000000
				binary.LittleEndian.PutUint64(args[16:], rawCount)
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil ||
					!bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:16], input[:]) || binary.LittleEndian.Uint64(args[16:]) != rawCount {
					t.Fatal("lane shift modulo/sign/isolation", kind, count, sample, input, results[:16], want, err)
				}
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("vector shift native allocations", kind, allocs)
		}
	}
}
