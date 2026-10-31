//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/rand"
	"testing"
)

func TestStraightLineRangeDecodeParity(t *testing.T) {
	check := func(data []byte) {
		t.Helper()
		for _, kind := range []uint8{0, 1, 2} {
			for _, index := range []uint32{0, 3, ^uint32(0)} {
				for _, extent := range []int32{1, 4, 0x7fffffff} {
					r := wasm.ReaderFrom(data)
					before := r.Offset()
					f := fn{}
					want := f.referenceStraightLineLoadExtent(&r, kind, index, extent)
					got := f.straightLineLoadExtent(&r, kind, index, extent)
					if got != want || r.Offset() != before {
						t.Fatalf("kind=%d idx=%d extent=%d data=%x: got=%d want=%d cursor=%d", kind, index, extent, data, got, want, r.Offset())
					}
				}
			}
		}
	}
	payloads := [][]byte{nil, {0}, {1}, {0x80}, {0xff, 0xff, 0xff, 0xff, 0x0f}, {0x80, 0x80, 0x80, 0x80, 0x80}, {0x40, 0, 32}, {0, 0x80}}
	for op := 0; op < 256; op++ {
		for _, payload := range payloads {
			data := append([]byte{byte(op)}, payload...)
			data = append(data, 0x20, 3, 0x28, 2, 64, 0x23, 3, 0x2d, 0, 127, 0x0b)
			check(data)
		}
	}
	random := rand.New(rand.NewSource(89))
	atoms := [][]byte{{0x01}, {0x20, 3}, {0x23, 3}, {0x21, 2}, {0x21, 3}, {0x22, 2}, {0x41, 0}, {0x41, 0xff, 0x7f}, {0x42, 0x80, 1}, {0x28, 2, 8}, {0x28, 0x42, 0, 32}, {0x2d, 0, 127}, {0x2d, 0, 0xff, 0xff, 0xff, 0xff, 0x0f}, {0x1a}, {0x1b}, {0x45}, {0x6a}, {0x6d}, {0x71}, {0x7e}, {0x83}, {0x8a}, {0xa7}, {0xac}, {0xad}, {0x04, 0x40}}
	for sample := 0; sample < 256; sample++ {
		var data []byte
		for n := 0; n < 64; n++ {
			data = append(data, atoms[random.Intn(len(atoms))]...)
		}
		check(append(data, 0x0b))
	}
	for _, n := range []int{4093, 4094, 4095, 4096, 4097} {
		data := make([]byte, n)
		for i := range data {
			data[i] = 1
		}
		check(append(data, 0x20, 3, 0x28, 2, 64, 0x0b))
	}
}

// Frozen pre-change scan: independent control flow and the original generic
// immediate skipper preserve truncation, barrier, and budget behavior.
func (f *fn) referenceStraightLineLoadExtent(r *wasm.Reader, kind uint8, idx uint32, extent int32) int32 {
	scan := *r
	prevKind, prevIdx := uint8(0), uint32(0)
	for n := 0; n < maxStraightLineRangeOps && scan.HasNext(); n++ {
		op, err := scan.Byte()
		if err != nil {
			break
		}
		nextKind, nextIdx := uint8(0), uint32(0)
		switch {
		case op == 0x20: // local.get
			x, err := scan.U32()
			if err != nil {
				return extent
			}
			nextKind, nextIdx = 1, x
		case op == 0x23: // global.get
			x, err := scan.U32()
			if err != nil {
				return extent
			}
			nextKind, nextIdx = 2, x
		case op == 0x21 || op == 0x22: // local.set / local.tee
			x, err := scan.U32()
			if err != nil || (kind == 1 && x == idx) {
				return extent
			}
		case op >= 0x28 && op <= 0x35: // scalar loads
			size := memAccessSize(op)
			align, err := scan.U32()
			if err != nil {
				return extent
			}
			memoryIndex := uint32(0)
			if align >= 64 && align < 128 {
				memoryIndex, err = scan.U32()
				if err != nil {
					return extent
				}
			}
			off, err := scan.U32() // this helper is only used for memory32
			if err != nil || memoryIndex != 0 {
				return extent
			}
			if prevKind == kind && prevIdx == idx {
				if candidate := int64(off) + int64(size); candidate > 0x7fffffff {
					return extent
				} else if int32(candidate) > extent {
					extent = int32(candidate)
				}
			}
		case op == 0x01 || op == 0x1a || op == 0x1b, // nop, drop, select
			op == 0x41 || op == 0x42,               // integer constants
			op >= 0x45 && op <= 0x6c,               // integer tests/arithmetic before div/rem
			op >= 0x71 && op <= 0x7e,               // i32 bit ops + i64 clz/ctz/popcnt/add/sub/mul
			op >= 0x83 && op <= 0x8a,               // i64 bit ops
			op == 0xa7 || op == 0xac || op == 0xad: // non-trapping integer conversions
			if err := skipImmediates(&scan, op); err != nil {
				return extent
			}
		default:
			return extent
		}
		prevKind, prevIdx = nextKind, nextIdx
	}
	return extent
}
