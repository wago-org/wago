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

func runProductionSWARMemory(t *testing.T, m *wasm.Module, init func([]byte)) (uint64, []byte, error) {
	t.Helper()
	cm, err := CompileModuleWith(m, CompileOptions{allowIncompleteSWAR: true})
	if err != nil {
		t.Fatalf("compile SWAR memory: %v", err)
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
	if init != nil {
		init(jm.CurrentBytes())
	}
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
	err = eng.Call(entry+uintptr(cm.Entry[0]), args, jm.LinearMemory(), trap, results)
	memory := append([]byte(nil), jm.CurrentBytes()...)
	return binary.LittleEndian.Uint64(results), memory, err
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

func TestSWARShuffleSwizzleAndRelaxedIntegerExec(t *testing.T) {
	aLo, aHi := swarPackLanes(8, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	bLo, bHi := swarPackLanes(8, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115)
	selectors := []byte{31, 0, 16, 15, 8, 23, 7, 24, 1, 30, 14, 17, 6, 25, 13, 18}
	shuffle := append(swarFD(13), selectors...)
	for lane, want := range []uint32{115, 0, 100, 15, 8, 107, 7, 108, 1, 114, 14, 101, 6, 109, 13, 102} {
		m := swarScalarModule(t, wasm.I32,
			swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), shuffle,
			swarIntegerExtract(8, lane, false))
		if got := runProductionSWARWrapper(t, m); uint32(got) != want {
			t.Fatalf("shuffle lane %d: got %d, want %d", lane, got, want)
		}
	}

	iLo, iHi := swarPackLanes(8, 0, 7, 8, 15, 16, 255, 3, 12, 14, 1, 9, 200, 6, 10, 2, 13)
	for _, sub := range []uint32{14, 256} {
		for lane, want := range []uint32{0, 7, 8, 15, 0, 0, 3, 12, 14, 1, 9, 0, 6, 10, 2, 13} {
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(aLo, aHi), swarV128Const(iLo, iHi), swarFD(sub),
				swarIntegerExtract(8, lane, false))
			if got := runProductionSWARWrapper(t, m); uint32(got) != want {
				t.Fatalf("swizzle sub %d lane %d: got %d, want %d", sub, lane, got, want)
			}
		}
	}

	for _, sub := range []uint32{265, 266, 267, 268} {
		m := swarScalarModule(t, wasm.I64,
			swarV128Const(0xffffffffffffffff, 0), swarV128Const(0, ^uint64(0)),
			swarV128Const(0x00ff00ff00ff00ff, 0xff00ff00ff00ff00), swarFD(sub), swarFD(29, 0))
		if got := runProductionSWARWrapper(t, m); got != 0x00ff00ff00ff00ff {
			t.Fatalf("relaxed laneselect sub %d: got %#x", sub, got)
		}
	}

	dotALo, dotAHi := swarPackLanes(8, 1, 2, 0xff, 0xfe, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14)
	dotBLo, dotBHi := swarPackLanes(8, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18)
	m := swarScalarModule(t, wasm.I32,
		swarV128Const(dotALo, dotAHi), swarV128Const(dotBLo, dotBHi), swarFD(274),
		swarIntegerExtract(16, 0, true))
	if got := runProductionSWARWrapper(t, m); uint32(got) != 11 { // 1*3 + 2*4
		t.Fatalf("relaxed dot got %d", got)
	}

	cLo, cHi := swarPackLanes(32, 100, 200, 300, 400)
	m = swarScalarModule(t, wasm.I32,
		swarV128Const(dotALo, dotAHi), swarV128Const(dotBLo, dotBHi), swarV128Const(cLo, cHi), swarFD(275),
		swarIntegerExtract(32, 0, false))
	if got := runProductionSWARWrapper(t, m); uint32(got) != 94 { // 100 + 1*3 + 2*4 - 1*5 - 2*6
		t.Fatalf("relaxed dot-add got %d", got)
	}
}

func swarMem(sub, align, offset uint32, lane ...byte) []byte {
	out := swarFD(sub)
	out = append(out, wasmtest.ULEB(align)...)
	out = append(out, wasmtest.ULEB(offset)...)
	return append(out, lane...)
}

func TestSWARV128MemoryExec(t *testing.T) {
	t.Run("load", func(t *testing.T) {
		body := []byte{0}
		body = append(body, swarI32Const(3)...)
		body = append(body, swarMem(0, 0, 0)...)
		body = append(body, swarIntegerExtract(64, 1, false)...)
		body = append(body, 0x0b)
		m := productionMemoryModule(t, nil, []wasm.ValType{wasm.I64}, body)
		got, _, err := runProductionSWARMemory(t, m, func(mem []byte) {
			for i := 0; i < 32; i++ {
				mem[i] = byte(i)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if want := binary.LittleEndian.Uint64([]byte{11, 12, 13, 14, 15, 16, 17, 18}); got != want {
			t.Fatalf("got %#x, want %#x", got, want)
		}
	})

	t.Run("store", func(t *testing.T) {
		const lo, hi = uint64(0x0706050403020100), uint64(0x0f0e0d0c0b0a0908)
		body := []byte{0}
		body = append(body, swarI32Const(5)...)
		body = append(body, swarV128Const(lo, hi)...)
		body = append(body, swarMem(11, 0, 0)...)
		body = append(body, swarI32Const(13)...)
		body = append(body, 0x29, 0x00, 0x00, 0x0b) // i64.load
		m := productionMemoryModule(t, nil, []wasm.ValType{wasm.I64}, body)
		got, memory, err := runProductionSWARMemory(t, m, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != hi || binary.LittleEndian.Uint64(memory[5:13]) != lo {
			t.Fatalf("result=%#x memory=%x", got, memory[5:21])
		}
	})

	t.Run("store-oob-no-partial-mutation", func(t *testing.T) {
		body := []byte{0}
		body = append(body, swarI32Const(65528)...)
		body = append(body, swarV128Const(0x1111111111111111, 0x2222222222222222)...)
		body = append(body, swarMem(11, 0, 0)...)
		body = append(body, 0x0b)
		m := productionMemoryModule(t, nil, nil, body)
		before := []byte{1, 2, 3, 4, 5, 6, 7, 8}
		_, memory, err := runProductionSWARMemory(t, m, func(mem []byte) { copy(mem[65528:], before) })
		if err == nil {
			t.Fatal("expected out-of-bounds trap")
		}
		if got := memory[65528:65536]; string(got) != string(before) {
			t.Fatalf("partial store mutated tail: got %x, want %x", got, before)
		}
	})

	for _, tc := range []struct {
		name   string
		sub    uint32
		init   []byte
		width  int
		lane   int
		signed bool
		want   uint64
		result wasm.ValType
	}{
		{"load8x8-s", 1, []byte{0x80, 2, 3, 4, 5, 6, 7, 8}, 16, 0, true, uint64(uint32(0xffffff80)), wasm.I32},
		{"load8x8-u", 2, []byte{0xff, 2, 3, 4, 5, 6, 7, 8}, 16, 0, false, 0xff, wasm.I32},
		{"load16x4-s", 3, []byte{0x00, 0x80, 3, 0, 4, 0, 5, 0}, 32, 0, false, 0xffff8000, wasm.I32},
		{"load32x2-u", 6, []byte{0xff, 0xff, 0xff, 0xff, 1, 0, 0, 0}, 64, 0, false, 0xffffffff, wasm.I64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0}
			body = append(body, swarI32Const(0)...)
			body = append(body, swarMem(tc.sub, 0, 0)...)
			body = append(body, swarIntegerExtract(tc.width, tc.lane, tc.signed)...)
			body = append(body, 0x0b)
			m := productionMemoryModule(t, nil, []wasm.ValType{tc.result}, body)
			got, _, err := runProductionSWARMemory(t, m, func(mem []byte) { copy(mem, tc.init) })
			if err != nil {
				t.Fatal(err)
			}
			if tc.width < 64 {
				got = uint64(uint32(got))
			}
			if got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}

	t.Run("splat-zero-and-lanes", func(t *testing.T) {
		body := []byte{0}
		body = append(body, swarI32Const(4)...)
		body = append(body, swarMem(9, 0, 0)...)
		body = append(body, swarIntegerExtract(32, 3, false)...)
		body = append(body, 0x0b)
		m := productionMemoryModule(t, nil, []wasm.ValType{wasm.I32}, body)
		got, _, err := runProductionSWARMemory(t, m, func(mem []byte) { binary.LittleEndian.PutUint32(mem[4:], 0x89abcdef) })
		if err != nil || uint32(got) != 0x89abcdef {
			t.Fatalf("splat got %#x, err=%v", uint32(got), err)
		}

		body = []byte{0}
		body = append(body, swarI32Const(0)...)
		body = append(body, swarMem(92, 0, 0)...)
		body = append(body, swarIntegerExtract(64, 1, false)...)
		body = append(body, 0x0b)
		m = productionMemoryModule(t, nil, []wasm.ValType{wasm.I64}, body)
		got, _, err = runProductionSWARMemory(t, m, func(mem []byte) { binary.LittleEndian.PutUint64(mem, ^uint64(0)) })
		if err != nil || got != 0 {
			t.Fatalf("load-zero high got %#x, err=%v", got, err)
		}

		body = []byte{0}
		body = append(body, swarI32Const(9)...)
		body = append(body, swarV128Const(0, 0)...)
		body = append(body, swarMem(84, 0, 0, 15)...)
		body = append(body, swarIntegerExtract(8, 15, false)...)
		body = append(body, 0x0b)
		m = productionMemoryModule(t, nil, []wasm.ValType{wasm.I32}, body)
		got, _, err = runProductionSWARMemory(t, m, func(mem []byte) { mem[9] = 0xab })
		if err != nil || uint32(got) != 0xab {
			t.Fatalf("load-lane got %#x, err=%v", got, err)
		}

		body = []byte{0}
		body = append(body, swarI32Const(10)...)
		body = append(body, swarV128Const(0x1122334455667788, 0x99aabbccddeeff00)...)
		body = append(body, swarMem(88, 0, 0, 8)...)
		body = append(body, swarI32Const(10)...)
		body = append(body, 0x2d, 0x00, 0x00, 0x0b) // i32.load8_u
		m = productionMemoryModule(t, nil, []wasm.ValType{wasm.I32}, body)
		got, _, err = runProductionSWARMemory(t, m, nil)
		if err != nil || uint32(got) != 0x00 {
			t.Fatalf("store-lane got %#x, err=%v", got, err)
		}
	})
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
