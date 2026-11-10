//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	goruntime "runtime"
	"testing"
)

func swarSpecialMultiplyOracle(a, b []byte, kind uint32) [16]byte {
	var out [16]byte
	for start := 0; start < 16; start += 2 {
		av := int64(int16(binary.LittleEndian.Uint16(a[start:])))
		bv := int64(int16(binary.LittleEndian.Uint16(b[start:])))
		value := av * bv
		if kind == 130 {
			value += 16384
			// Go division truncates toward zero; implement floor explicitly.
			if value < 0 {
				value = -((-value + 32767) / 32768)
			} else {
				value /= 32768
			}
			if value > 32767 {
				value = 32767
			}
			binary.LittleEndian.PutUint16(out[start:], uint16(value))
		} else {
			av = int64(int16(binary.LittleEndian.Uint16(a[start+2:])))
			bv = int64(int16(binary.LittleEndian.Uint16(b[start+2:])))
			value += av * bv
			binary.LittleEndian.PutUint32(out[start:], uint32(value))
			start += 2
		}
	}
	return out
}

func TestSWARRV64Q15AndDotSignedArithmetic(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	rng := rand.New(rand.NewSource(640130186))
	edges := []uint16{0, 1, 0xffff, 0x7fff, 0x8000, 0x4000, 0xc000, 0x7ffe}
	for _, kind := range []uint32{130, 186} {
		fixture := swarArithmeticFixture(kind)
		fixture.locals = []byte{1, 126, 0x7b}
		fn, ft := swarValidatedFunction(t, fixture)
		code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
		if err != nil || info.FrameBytes <= 2048 {
			t.Fatal("large special arithmetic frame", kind, info, err)
		}
		entry := thunkImageRV64(t, code)
		for sample := 0; sample < 64; sample++ {
			var input [32]byte
			_, _ = rng.Read(input[:])
			if sample < 16 {
				for lane := 0; lane < 8; lane++ {
					binary.LittleEndian.PutUint16(input[lane*2:], edges[sample%8])
					binary.LittleEndian.PutUint16(input[16+lane*2:], edges[(sample/8+lane)%8])
				}
			} else if sample == 16 || sample == 17 {
				for lane := 0; lane < 8; lane++ {
					binary.LittleEndian.PutUint16(input[lane*2:], 0x8000)
					bv := uint16(0x8000)
					if sample == 17 && lane%2 == 0 {
						bv = 0x7fff
					}
					binary.LittleEndian.PutUint16(input[16+lane*2:], bv)
				}
			} else if sample == 18 {
				binary.LittleEndian.PutUint64(input[:8], 0x7ff0000000000001)
				binary.LittleEndian.PutUint64(input[8:16], 0x8000000000000000)
				binary.LittleEndian.PutUint64(input[16:24], 0xffffffffffffffff)
				binary.LittleEndian.PutUint64(input[24:], 1)
			}
			copy(args, input[:])
			want := swarSpecialMultiplyOracle(input[:16], input[16:], kind)
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:32], input[:]) {
				t.Fatal("signed rounding/dot wrap", kind, sample, input, results[:16], want, err)
			}
		}
		call := func() {
			if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
				panic(err)
			}
		}
		if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
			t.Fatal("special arithmetic allocations", kind, allocs)
		}
	}
}

func TestSWARRV64Q15EveryInputWithBoundaryPartners(t *testing.T) {
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	fn, ft := swarValidatedFunction(t, swarArithmeticFixture(130))
	code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true})
	if err != nil {
		t.Fatal(err)
	}
	entry := thunkImageRV64(t, code)
	partners := []uint16{0, 1, 0xffff, 0x7fff, 0x8000, 0x4000, 0xc000, 0x7ffe}
	// Every16-bit input appears once, paired with a rotating boundary value.
	for base := 0; base < 65536; base += 8 {
		for lane := 0; lane < 8; lane++ {
			binary.LittleEndian.PutUint16(args[lane*2:], uint16(base+lane))
			binary.LittleEndian.PutUint16(args[16+lane*2:], partners[(base/8+lane)%8])
		}
		want := swarSpecialMultiplyOracle(args[:16], args[16:32], 130)
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) {
			t.Fatal("Q15 boundary partner", base, results[:16], want, err)
		}
	}
}

func TestSWARRV64BytePopcountEveryValueAtEveryLane(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 2)
	fixture := swarUnaryFixture(98, false)
	fixture.locals = []byte{1, 127, 0x7b}
	fn, ft := swarValidatedFunction(t, fixture)
	code, info, err := compileSWARPairs(fn, ft, emissionMode{production: true})
	if err != nil || info.FrameBytes <= 2048 {
		t.Fatal("large popcount frame", info, err)
	}
	entry := thunkImageRV64(t, code)
	for base := 0; base < 256; base++ {
		var input, want [16]byte
		for lane := 0; lane < 16; lane++ {
			input[lane] = byte(base + 17*lane)
			for bit := uint(0); bit < 8; bit++ {
				want[lane] += (input[lane] >> bit) & 1
			}
		}
		copy(args, input[:])
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil || !bytes.Equal(results[:16], want[:]) || !bytes.Equal(args[:16], input[:]) {
			t.Fatal("byte bit count", base, input, results[:16], want, err)
		}
	}
	call := func() {
		if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
			panic(err)
		}
	}
	if allocs := testing.AllocsPerRun(50, call); allocs != 0 {
		t.Fatal("popcount allocations", allocs)
	}
}
