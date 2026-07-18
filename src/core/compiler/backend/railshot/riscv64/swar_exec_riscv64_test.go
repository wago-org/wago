//go:build linux && riscv64

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func swarFD(sub uint32, immediate ...byte) []byte {
	out := []byte{0xfd}
	out = append(out, wasmtest.ULEB(sub)...)
	return append(out, immediate...)
}

func swarV128Const(lo, hi uint64) []byte {
	out := swarFD(12)
	var bits [16]byte
	binary.LittleEndian.PutUint64(bits[:8], lo)
	binary.LittleEndian.PutUint64(bits[8:], hi)
	return append(out, bits[:]...)
}

func runProductionSWARWrapper(t *testing.T, m *wasm.Module) uint64 {
	t.Helper()
	cm, err := CompileModuleWith(m, CompileOptions{allowIncompleteSWAR: true})
	if err != nil {
		t.Fatalf("compile SWAR: %v", err)
	}
	eng, err := coreruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := coreruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	code, entry, err := coreruntime.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer coreruntime.Unmap(code)
	args, results, trap := arena.Alloc(32), arena.Alloc(32), arena.Alloc(8)
	if err := eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results); err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint64(results)
}

func swarScalarModule(t *testing.T, result wasm.ValType, instructions ...[]byte) *wasm.Module {
	t.Helper()
	body := []byte{0} // no declared locals
	for _, in := range instructions {
		body = append(body, in...)
	}
	body = append(body, 0x0b)
	return productionModule1(t, nil, []wasm.ValType{result}, body)
}

func TestSWARV128BitwiseAndI64x2Exec(t *testing.T) {
	const (
		aLo = uint64(0xfedcba9876543210)
		aHi = uint64(0x0123456789abcdef)
		bLo = uint64(0x0f0ff0f05555aaaa)
		bHi = uint64(0x3333cccc5a5aa5a5)
	)
	tests := []struct {
		name string
		ins  [][]byte
		want uint64
	}{
		{"not-high", [][]byte{swarV128Const(aLo, aHi), swarFD(77), swarFD(29, 1)}, ^aHi},
		{"and-low", [][]byte{swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), swarFD(78), swarFD(29, 0)}, aLo & bLo},
		{"andnot-high", [][]byte{swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), swarFD(79), swarFD(29, 1)}, aHi &^ bHi},
		{"or-high", [][]byte{swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), swarFD(80), swarFD(29, 1)}, aHi | bHi},
		{"xor-low", [][]byte{swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), swarFD(81), swarFD(29, 0)}, aLo ^ bLo},
		{"bitselect-high", [][]byte{swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), swarV128Const(0, 0xff00ff00ff00ff00), swarFD(82), swarFD(29, 1)}, (aHi & 0xff00ff00ff00ff00) | (bHi &^ 0xff00ff00ff00ff00)},
		{"add", [][]byte{swarV128Const(40, 100), swarV128Const(2, 58), swarFD(206), swarFD(29, 1)}, 158},
		{"sub", [][]byte{swarV128Const(100, 100), swarV128Const(58, 42), swarFD(209), swarFD(29, 0)}, 42},
		{"mul", [][]byte{swarV128Const(6, 7), swarV128Const(7, 6), swarFD(213), swarFD(29, 1)}, 42},
		{"splat", [][]byte{{0x42, 0x2a}, swarFD(18), swarFD(29, 1)}, 42},
		{"replace", [][]byte{swarV128Const(1, 2), {0x42, 0x2a}, swarFD(30, 1), swarFD(29, 1)}, 42},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runProductionSWARWrapper(t, swarScalarModule(t, wasm.I64, tc.ins...))
			if got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}
}

func TestSWARV128AnyTrueExec(t *testing.T) {
	for _, tc := range []struct {
		name   string
		lo, hi uint64
		want   uint64
	}{{"zero", 0, 0, 0}, {"low", 1, 0, 1}, {"high", 0, 1 << 63, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, wasm.I32, swarV128Const(tc.lo, tc.hi), swarFD(83))
			if got := runProductionSWARWrapper(t, m); uint32(got) != uint32(tc.want) {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func swarI32Const(v int32) []byte {
	return append([]byte{0x41}, wasmtest.SLEB32(v)...)
}

func swarIntegerExtract(width, lane int, signed bool) []byte {
	var sub uint32
	switch width {
	case 8:
		sub = 22
		if signed {
			sub = 21
		}
	case 16:
		sub = 25
		if signed {
			sub = 24
		}
	case 32:
		sub = 27
	case 64:
		sub = 29
	default:
		panic("invalid lane width")
	}
	return swarFD(sub, byte(lane))
}

func TestSWARIntegerLanePlumbingExec(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, lane   int
		value         int32
		signedExtract bool
		want          uint64
	}{
		{"i8-splat-signed", 8, 15, -128, true, uint64(uint32(0xffffff80))},
		{"i8-splat-unsigned", 8, 9, 0xab, false, 0xab},
		{"i16-splat-signed", 16, 7, -32767, true, uint64(uint32(0xffff8001))},
		{"i16-splat-unsigned", 16, 5, 0xabcd, false, 0xabcd},
		{"i32-splat", 32, 3, 0x76543210, false, 0x76543210},
	} {
		t.Run(tc.name, func(t *testing.T) {
			splatSub := map[int]uint32{8: 15, 16: 16, 32: 17}[tc.width]
			m := swarScalarModule(t, wasm.I32, swarI32Const(tc.value), swarFD(splatSub), swarIntegerExtract(tc.width, tc.lane, tc.signedExtract))
			if got := runProductionSWARWrapper(t, m); uint32(got) != uint32(tc.want) {
				t.Fatalf("got %#x, want %#x", uint32(got), uint32(tc.want))
			}
		})
	}

	for _, tc := range []struct {
		name        string
		width, lane int
		value       int32
		want        uint64
	}{
		{"i8", 8, 10, 0xab, 0xab},
		{"i16", 16, 6, 0x7abc, 0x7abc},
		{"i32", 32, 3, 0x76543210, 0x76543210},
	} {
		t.Run("replace-"+tc.name, func(t *testing.T) {
			replaceSub := map[int]uint32{8: 23, 16: 26, 32: 28}[tc.width]
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(0, 0), swarI32Const(tc.value), swarFD(replaceSub, byte(tc.lane)),
				swarIntegerExtract(tc.width, tc.lane, false))
			if got := runProductionSWARWrapper(t, m); uint32(got) != uint32(tc.want) {
				t.Fatalf("got %#x, want %#x", uint32(got), uint32(tc.want))
			}
		})
	}
}

func TestSWARPackedAddSubNoCrossLaneCarryExec(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, lane   int
		sub           uint32
		aLo, aHi      uint64
		bLo, bHi      uint64
		signedExtract bool
		want          uint64
	}{
		{"i8-add-carry", 8, 1, 110, 0x00000000000000ff, 0, 1, 0, false, 0},
		{"i8-sub-borrow", 8, 1, 113, 0, 0, 1, 0, false, 0},
		{"i16-add-carry", 16, 1, 142, 0x000000000000ffff, 0, 1, 0, false, 0},
		{"i16-sub-borrow", 16, 1, 145, 0, 0, 1, 0, false, 0},
		{"i32-add-carry", 32, 1, 174, 0x00000000ffffffff, 0, 1, 0, false, 0},
		{"i32-sub-borrow", 32, 1, 177, 0, 0, 1, 0, false, 0},
		{"i8-high-half", 8, 15, 110, 0, 0xfe00000000000000, 0, 0x0300000000000000, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(tc.aLo, tc.aHi), swarV128Const(tc.bLo, tc.bHi), swarFD(tc.sub),
				swarIntegerExtract(tc.width, tc.lane, tc.signedExtract))
			if got := runProductionSWARWrapper(t, m); uint32(got) != uint32(tc.want) {
				t.Fatalf("got %#x, want %#x", uint32(got), uint32(tc.want))
			}
		})
	}
}

func TestSWARIntegerShiftUnaryAllTrueAndBitmaskExec(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, lane   int
		valueLo       uint64
		shiftSub      uint32
		count         int32
		signedExtract bool
		want          uint32
	}{
		{"i8-shl-masked-count", 8, 0, 0x81, 107, 9, false, 0x02},
		{"i8-shr-s", 8, 0, 0x80, 108, 1, true, 0xffffffc0},
		{"i8-shr-u", 8, 0, 0x80, 109, 1, false, 0x40},
		{"i16-shl", 16, 0, 0x8001, 139, 1, false, 0x0002},
		{"i16-shr-s", 16, 0, 0x8000, 140, 4, true, 0xfffff800},
		{"i32-shr-u", 32, 0, 0x80000000, 173, 4, false, 0x08000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(tc.valueLo, 0), swarI32Const(tc.count), swarFD(tc.shiftSub),
				swarIntegerExtract(tc.width, tc.lane, tc.signedExtract))
			if got := runProductionSWARWrapper(t, m); uint32(got) != tc.want {
				t.Fatalf("got %#x, want %#x", uint32(got), tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name          string
		width, lane   int
		bits          uint64
		sub           uint32
		signedExtract bool
		want          uint32
	}{
		{"i8-abs", 8, 0, 0xfb, 96, true, 5},
		{"i8-abs-min", 8, 0, 0x80, 96, false, 0x80},
		{"i16-neg", 16, 0, 5, 129, true, 0xfffffffb},
		{"i32-abs", 32, 0, 0xffffffd6, 160, false, 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(tc.bits, 0), swarFD(tc.sub),
				swarIntegerExtract(tc.width, tc.lane, tc.signedExtract))
			if got := runProductionSWARWrapper(t, m); uint32(got) != tc.want {
				t.Fatalf("got %#x, want %#x", uint32(got), tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name   string
		width  int
		lo, hi uint64
		want   uint32
	}{
		{"i8-all", 8, 0x0101010101010101, 0x0101010101010101, 1},
		{"i8-zero", 8, 0x0101010101000101, 0x0101010101010101, 0},
		{"i16-all", 16, 0x0001000100010001, 0x0001000100010001, 1},
		{"i32-zero", 32, 0x0000000100000001, 0x0000000000000001, 0},
		{"i64-all", 64, 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := map[int]uint32{8: 99, 16: 131, 32: 163, 64: 195}[tc.width]
			m := swarScalarModule(t, wasm.I32, swarV128Const(tc.lo, tc.hi), swarFD(sub))
			if got := runProductionSWARWrapper(t, m); uint32(got) != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name   string
		width  int
		lo, hi uint64
		want   uint32
	}{
		{"i8", 8, 0x80007f0080007f80, 0x0080000000000080, 0x4189},
		{"i16", 16, 0x800000007fff8000, 0x000080000000ffff, 0x59},
		{"i32", 32, 0x8000000000000000, 0x800000007fffffff, 0x0a},
		{"i64", 64, 1 << 63, 0, 0x01},
	} {
		t.Run("bitmask-"+tc.name, func(t *testing.T) {
			sub := map[int]uint32{8: 100, 16: 132, 32: 164, 64: 196}[tc.width]
			m := swarScalarModule(t, wasm.I32, swarV128Const(tc.lo, tc.hi), swarFD(sub))
			if got := runProductionSWARWrapper(t, m); uint32(got) != tc.want {
				t.Fatalf("got %#x, want %#x", uint32(got), tc.want)
			}
		})
	}
}

func TestSWARIntegerCompareMinMaxMulAverageAndPopcntExec(t *testing.T) {
	for _, tc := range []struct {
		name        string
		width, lane int
		sub         uint32
		a, b        uint64
		want        uint64
	}{
		{"i8-eq", 8, 0, 35, 7, 7, 0xff},
		{"i8-ne", 8, 0, 36, 7, 8, 0xff},
		{"i8-lt-s", 8, 0, 37, 0xff, 1, 0xff},
		{"i8-lt-u", 8, 0, 38, 0xff, 1, 0},
		{"i8-gt-s", 8, 0, 39, 1, 0xff, 0xff},
		{"i8-ge-u-false", 8, 0, 44, 1, 2, 0},
		{"i16-le-s", 16, 0, 51, 0x8000, 0x7fff, 0xffff},
		{"i16-gt-u", 16, 0, 50, 0xffff, 1, 0xffff},
		{"i32-lt-s", 32, 0, 57, 0xffffffff, 1, 0xffffffff},
		{"i32-ge-u", 32, 0, 64, 0xffffffff, 1, 0xffffffff},
		{"i64-lt-s", 64, 0, 216, ^uint64(0), 1, ^uint64(0)},
		{"i64-gt-s-false", 64, 0, 217, 1, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resultType := wasm.I32
			if tc.width == 64 {
				resultType = wasm.I64
			}
			m := swarScalarModule(t, resultType,
				swarV128Const(tc.a, 0), swarV128Const(tc.b, 0), swarFD(tc.sub),
				swarIntegerExtract(tc.width, tc.lane, false))
			got := runProductionSWARWrapper(t, m)
			if tc.width < 64 {
				got = uint64(uint32(got))
			}
			if got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name        string
		width, lane int
		sub         uint32
		a, b, want  uint64
	}{
		{"i8-min-s", 8, 0, 118, 0xff, 1, 0xff},
		{"i8-min-u", 8, 0, 119, 0xff, 1, 1},
		{"i8-max-s", 8, 0, 120, 0xff, 1, 1},
		{"i8-max-u", 8, 0, 121, 0xff, 1, 0xff},
		{"i16-min-s", 16, 0, 150, 0x8000, 1, 0x8000},
		{"i16-max-u", 16, 0, 153, 0xffff, 1, 0xffff},
		{"i32-min-s", 32, 0, 182, 0xffffffff, 1, 0xffffffff},
		{"i32-max-u", 32, 0, 185, 0xffffffff, 1, 0xffffffff},
		{"i8-avgr-u", 8, 0, 123, 0xff, 0xff, 0xff},
		{"i16-avgr-u", 16, 0, 155, 0xffff, 0xfffe, 0xffff},
		{"i16-mul", 16, 0, 149, 300, 300, 0x5f90}, // 90000 modulo 2^16
		{"i32-mul", 32, 0, 181, 0xffffffff, 2, 0xfffffffe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(tc.a, 0), swarV128Const(tc.b, 0), swarFD(tc.sub),
				swarIntegerExtract(tc.width, tc.lane, false))
			if got := runProductionSWARWrapper(t, m); uint32(got) != uint32(tc.want) {
				t.Fatalf("got %#x, want %#x", uint32(got), uint32(tc.want))
			}
		})
	}

	t.Run("i8x16-popcnt", func(t *testing.T) {
		const lo = uint64(0xff0180550f00aa7f)
		for lane, want := range []uint32{7, 4, 0, 4, 4, 1, 1, 8} {
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(lo, 0), swarFD(98), swarIntegerExtract(8, lane, false))
			if got := runProductionSWARWrapper(t, m); uint32(got) != want {
				t.Fatalf("lane %d: got %d, want %d", lane, got, want)
			}
		}
	})
}

func swarPackLanes(width int, lanes ...uint64) (lo, hi uint64) {
	for lane, value := range lanes {
		shift := (lane * width) & 63
		if lane*width < 64 {
			lo |= (value & swarLaneMask(width)) << shift
		} else {
			hi |= (value & swarLaneMask(width)) << shift
		}
	}
	return
}

func TestSWARSaturatingNarrowExtendMultiplyAndDotExec(t *testing.T) {
	for _, tc := range []struct {
		name        string
		width, lane int
		sub         uint32
		a, b        uint64
		want        uint32
	}{
		{"i8-add-s-max", 8, 0, 111, 0x7f, 1, 0x7f},
		{"i8-add-s-min", 8, 0, 111, 0x80, 0xff, 0x80},
		{"i8-add-u", 8, 0, 112, 0xff, 1, 0xff},
		{"i8-sub-s-min", 8, 0, 114, 0x80, 1, 0x80},
		{"i8-sub-s-max", 8, 0, 114, 0x7f, 0xff, 0x7f},
		{"i8-sub-u", 8, 0, 115, 0, 1, 0},
		{"i16-add-s-max", 16, 0, 143, 0x7fff, 1, 0x7fff},
		{"i16-add-u", 16, 0, 144, 0xffff, 1, 0xffff},
		{"i16-sub-s-min", 16, 0, 146, 0x8000, 1, 0x8000},
		{"i16-sub-u", 16, 0, 147, 0, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(tc.a, 0), swarV128Const(tc.b, 0), swarFD(tc.sub),
				swarIntegerExtract(tc.width, tc.lane, false))
			if got := runProductionSWARWrapper(t, m); uint32(got) != tc.want {
				t.Fatalf("got %#x, want %#x", uint32(got), tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name               string
		srcWidth, dstWidth int
		sub                uint32
		aLo, aHi, bLo, bHi uint64
		lane               int
		want               uint32
	}{
		{"i8-narrow-s-a", 16, 8, 101, 0x0000000000007fff, 0, 0, 0, 0, 0x7f},
		{"i8-narrow-s-b", 16, 8, 101, 0, 0, 0x0000000000008000, 0, 8, 0x80},
		{"i8-narrow-u-negative", 16, 8, 102, 0xffff, 0, 0, 0, 0, 0},
		{"i8-narrow-u-high", 16, 8, 102, 300, 0, 0, 0, 0, 0xff},
		{"i16-narrow-s", 32, 16, 133, 0x000000007fffffff, 0, 0, 0, 0, 0x7fff},
		{"i16-narrow-u", 32, 16, 134, 0xffffffff, 0, 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(tc.aLo, tc.aHi), swarV128Const(tc.bLo, tc.bHi), swarFD(tc.sub),
				swarIntegerExtract(tc.dstWidth, tc.lane, false))
			if got := runProductionSWARWrapper(t, m); uint32(got) != tc.want {
				t.Fatalf("got %#x, want %#x", uint32(got), tc.want)
			}
		})
	}

	i8Lo, i8Hi := swarPackLanes(8, 0x80, 2, 3, 4, 5, 6, 7, 8, 0xff, 10, 11, 12, 13, 14, 15, 0x7f)
	i16Lo, i16Hi := swarPackLanes(16, 0x8000, 2, 3, 4, 0xffff, 6, 7, 0x7fff)
	for _, tc := range []struct {
		name       string
		sub        uint32
		lo, hi     uint64
		dstWidth   int
		lane       int
		want       uint64
		resultType wasm.ValType
	}{
		{"i16-extend-low-i8-s", 135, i8Lo, i8Hi, 16, 0, 0xff80, wasm.I32},
		{"i16-extend-high-i8-u", 138, i8Lo, i8Hi, 16, 0, 0xff, wasm.I32},
		{"i32-extend-low-i16-s", 167, i16Lo, i16Hi, 32, 0, 0xffff8000, wasm.I32},
		{"i32-extend-high-i16-u", 170, i16Lo, i16Hi, 32, 0, 0xffff, wasm.I32},
		{"i64-extend-low-i32-s", 199, 0x00000000ffffffff, 0, 64, 0, ^uint64(0), wasm.I64},
		{"i64-extend-high-i32-u", 202, 0, 0x00000000ffffffff, 64, 0, 0xffffffff, wasm.I64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, tc.resultType,
				swarV128Const(tc.lo, tc.hi), swarFD(tc.sub), swarIntegerExtract(tc.dstWidth, tc.lane, false))
			got := runProductionSWARWrapper(t, m)
			if tc.dstWidth < 64 {
				got = uint64(uint32(got))
			}
			if got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name               string
		sub                uint32
		aLo, aHi, bLo, bHi uint64
		dstWidth, lane     int
		want               uint64
		resultType         wasm.ValType
	}{
		{"i16-extmul-low-i8-s", 156, 0x80, 0, 2, 0, 16, 0, 0xff00, wasm.I32},
		{"i16-extmul-high-i8-u", 159, 0, 0xff, 0, 2, 16, 0, 510, wasm.I32},
		{"i32-extmul-low-i16-s", 188, 0x8000, 0, 2, 0, 32, 0, 0xffff0000, wasm.I32},
		{"i64-extmul-low-i32-u", 222, 0xffffffff, 0, 2, 0, 64, 0, 0x1fffffffe, wasm.I64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, tc.resultType,
				swarV128Const(tc.aLo, tc.aHi), swarV128Const(tc.bLo, tc.bHi), swarFD(tc.sub),
				swarIntegerExtract(tc.dstWidth, tc.lane, false))
			got := runProductionSWARWrapper(t, m)
			if tc.dstWidth < 64 {
				got = uint64(uint32(got))
			}
			if got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}

	t.Run("pairwise-and-dot", func(t *testing.T) {
		lo, hi := swarPackLanes(8, 0xff, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16)
		m := swarScalarModule(t, wasm.I32, swarV128Const(lo, hi), swarFD(124), swarIntegerExtract(16, 0, false))
		if got := runProductionSWARWrapper(t, m); uint32(got) != 1 { // -1 + 2
			t.Fatalf("pairwise got %#x", uint32(got))
		}

		aLo, aHi := swarPackLanes(16, 2, 3, 4, 5, 6, 7, 8, 9)
		bLo, bHi := swarPackLanes(16, 10, 20, 30, 40, 50, 60, 70, 80)
		m = swarScalarModule(t, wasm.I32,
			swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), swarFD(186), swarIntegerExtract(32, 0, false))
		if got := runProductionSWARWrapper(t, m); uint32(got) != 80 { // 2*10 + 3*20
			t.Fatalf("dot got %d", got)
		}
	})

	for _, sub := range []uint32{130, 273} {
		t.Run("q15", func(t *testing.T) {
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(0x8000, 0), swarV128Const(0x8000, 0), swarFD(sub),
				swarIntegerExtract(16, 0, false))
			if got := runProductionSWARWrapper(t, m); uint32(got) != 0x7fff {
				t.Fatalf("got %#x", uint32(got))
			}
		})
	}
}

func TestSWARV128LocalControlAndSelectExec(t *testing.T) {
	t.Run("local", func(t *testing.T) {
		body := []byte{1, 1, 0x7b} // one v128 local
		body = append(body, swarV128Const(11, 42)...)
		body = append(body, 0x21, 0x00, 0x20, 0x00) // local.set/get 0
		body = append(body, swarFD(29, 1)...)
		body = append(body, 0x0b)
		m := productionModule1(t, nil, []wasm.ValType{wasm.I64}, body)
		if got := runProductionSWARWrapper(t, m); got != 42 {
			t.Fatalf("got %d", got)
		}
	})

	t.Run("block-result", func(t *testing.T) {
		body := []byte{0, 0x02, 0x7b} // block (result v128)
		body = append(body, swarV128Const(0, 42)...)
		body = append(body, 0x0b)
		body = append(body, swarFD(29, 1)...)
		body = append(body, 0x0b)
		m := productionModule1(t, nil, []wasm.ValType{wasm.I64}, body)
		if got := runProductionSWARWrapper(t, m); got != 42 {
			t.Fatalf("got %d", got)
		}
	})

	for _, cond := range []byte{0, 1} {
		t.Run("select", func(t *testing.T) {
			body := []byte{0}
			body = append(body, swarV128Const(11, 12)...)
			body = append(body, swarV128Const(41, 42)...)
			body = append(body, 0x41, cond, 0x1b) // i32.const cond; select
			body = append(body, swarFD(29, 1)...)
			body = append(body, 0x0b)
			m := productionModule1(t, nil, []wasm.ValType{wasm.I64}, body)
			want := uint64(42)
			if cond != 0 {
				want = 12
			}
			if got := runProductionSWARWrapper(t, m); got != want {
				t.Fatalf("got %d, want %d", got, want)
			}
		})
	}
}
