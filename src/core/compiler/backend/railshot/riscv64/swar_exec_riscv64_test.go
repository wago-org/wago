//go:build linux && riscv64

package riscv64

import (
	"encoding/binary"
	"math"
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

func runProductionSWARWrapper(t *testing.T, m *wasm.Module, serializedArgs ...uint64) uint64 {
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
	for i, value := range serializedArgs {
		binary.LittleEndian.PutUint64(args[i*8:], value)
	}
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

func swarF32Const(v float32) []byte {
	var bits [4]byte
	binary.LittleEndian.PutUint32(bits[:], math.Float32bits(v))
	return append([]byte{0x43}, bits[:]...)
}

func swarF64Const(v float64) []byte {
	var bits [8]byte
	binary.LittleEndian.PutUint64(bits[:], math.Float64bits(v))
	return append([]byte{0x44}, bits[:]...)
}

func TestSWARFloatLaneArithmeticCompareAndRoundingExec(t *testing.T) {
	t.Run("splat-extract-replace", func(t *testing.T) {
		m := swarScalarModule(t, wasm.F32, swarF32Const(3.5), swarFD(19), swarFD(31, 3))
		if got := uint32(runProductionSWARWrapper(t, m)); got != math.Float32bits(3.5) {
			t.Fatalf("f32 splat got %#x", got)
		}
		m = swarScalarModule(t, wasm.F64, swarV128Const(1, 2), swarF64Const(6.25), swarFD(34, 1), swarFD(33, 1))
		if got := runProductionSWARWrapper(t, m); got != math.Float64bits(6.25) {
			t.Fatalf("f64 replace got %#x", got)
		}
	})

	for _, tc := range []struct {
		name string
		sub  uint32
		a, b float32
		want float32
	}{
		{"add", 228, 1.5, 2.25, 3.75},
		{"sub", 229, 5.5, 1.25, 4.25},
		{"mul", 230, 1.5, -2, -3},
		{"div", 231, 7.5, 2.5, 3},
	} {
		t.Run("f32-"+tc.name, func(t *testing.T) {
			aLo, aHi := swarPackLanes(32, uint64(math.Float32bits(tc.a)), 0, 0, 0)
			bLo, bHi := swarPackLanes(32, uint64(math.Float32bits(tc.b)), 0, 0, 0)
			m := swarScalarModule(t, wasm.F32,
				swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), swarFD(tc.sub), swarFD(31, 0))
			if got := uint32(runProductionSWARWrapper(t, m)); got != math.Float32bits(tc.want) {
				t.Fatalf("got %#x, want %#x", got, math.Float32bits(tc.want))
			}
		})
	}

	for _, tc := range []struct {
		name string
		sub  uint32
		a, b float64
		want float64
	}{
		{"add", 240, 1.5, 2.25, 3.75},
		{"sub", 241, 5.5, 1.25, 4.25},
		{"mul", 242, 1.5, -2, -3},
		{"div", 243, 7.5, 2.5, 3},
	} {
		t.Run("f64-"+tc.name, func(t *testing.T) {
			m := swarScalarModule(t, wasm.F64,
				swarV128Const(math.Float64bits(tc.a), 0), swarV128Const(math.Float64bits(tc.b), 0), swarFD(tc.sub), swarFD(33, 0))
			if got := runProductionSWARWrapper(t, m); got != math.Float64bits(tc.want) {
				t.Fatalf("got %#x, want %#x", got, math.Float64bits(tc.want))
			}
		})
	}

	t.Run("bit-sign-and-sqrt", func(t *testing.T) {
		const nanPayload = uint64(0x7ff8123456789abc)
		m := swarScalarModule(t, wasm.F64, swarV128Const(nanPayload|1<<63, 0), swarFD(236), swarFD(33, 0))
		if got := runProductionSWARWrapper(t, m); got != nanPayload {
			t.Fatalf("abs payload got %#x", got)
		}
		m = swarScalarModule(t, wasm.F64, swarV128Const(nanPayload, 0), swarFD(237), swarFD(33, 0))
		if got := runProductionSWARWrapper(t, m); got != nanPayload|1<<63 {
			t.Fatalf("neg payload got %#x", got)
		}
		m = swarScalarModule(t, wasm.F64, swarV128Const(math.Float64bits(81), 0), swarFD(239), swarFD(33, 0))
		if got := runProductionSWARWrapper(t, m); got != math.Float64bits(9) {
			t.Fatalf("sqrt got %#x", got)
		}
	})

	for _, tc := range []struct {
		name string
		sub  uint32
		in   float64
		want float64
	}{
		{"ceil", 116, -1.75, -1},
		{"floor", 117, -1.25, -2},
		{"trunc", 122, -1.75, -1},
		{"nearest-even", 148, 2.5, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := swarScalarModule(t, wasm.F64, swarV128Const(math.Float64bits(tc.in), 0), swarFD(tc.sub), swarFD(33, 0))
			if got := runProductionSWARWrapper(t, m); got != math.Float64bits(tc.want) {
				t.Fatalf("got %#x, want %#x", got, math.Float64bits(tc.want))
			}
		})
	}

	nan32 := uint64(0x7fc01234)
	for _, tc := range []struct {
		name string
		sub  uint32
		a, b uint64
		want uint32
	}{
		{"eq", 65, uint64(math.Float32bits(2)), uint64(math.Float32bits(2)), 0xffffffff},
		{"lt", 67, uint64(math.Float32bits(-1)), uint64(math.Float32bits(1)), 0xffffffff},
		{"ne-nan", 66, nan32, uint64(math.Float32bits(1)), 0xffffffff},
		{"lt-nan", 67, nan32, uint64(math.Float32bits(1)), 0},
	} {
		t.Run("cmp-"+tc.name, func(t *testing.T) {
			aLo, aHi := swarPackLanes(32, tc.a, 0, 0, 0)
			bLo, bHi := swarPackLanes(32, tc.b, 0, 0, 0)
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), swarFD(tc.sub), swarIntegerExtract(32, 0, false))
			if got := uint32(runProductionSWARWrapper(t, m)); got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}

	t.Run("min-max-pseudo", func(t *testing.T) {
		plusZero, minusZero := math.Float64bits(0), math.Float64bits(math.Copysign(0, -1))
		m := swarScalarModule(t, wasm.F64,
			swarV128Const(plusZero, 0), swarV128Const(minusZero, 0), swarFD(244), swarFD(33, 0))
		if got := runProductionSWARWrapper(t, m); got != minusZero {
			t.Fatalf("min zero got %#x", got)
		}
		m = swarScalarModule(t, wasm.F64,
			swarV128Const(minusZero, 0), swarV128Const(plusZero, 0), swarFD(245), swarFD(33, 0))
		if got := runProductionSWARWrapper(t, m); got != plusZero {
			t.Fatalf("max zero got %#x", got)
		}
		first := math.Float64bits(1.25)
		m = swarScalarModule(t, wasm.F64,
			swarV128Const(first, 0), swarV128Const(0x7ff8123456789abc, 0), swarFD(246), swarFD(33, 0))
		if got := runProductionSWARWrapper(t, m); got != first {
			t.Fatalf("pmin first-wins got %#x", got)
		}
	})
}

func TestSWARFloatConversionsAndRelaxedMaddExec(t *testing.T) {
	t.Run("demote-promote", func(t *testing.T) {
		m := swarScalarModule(t, wasm.F32,
			swarV128Const(math.Float64bits(1.5), math.Float64bits(-2.25)), swarFD(94), swarFD(31, 1))
		if got := uint32(runProductionSWARWrapper(t, m)); got != math.Float32bits(-2.25) {
			t.Fatalf("demote got %#x", got)
		}
		fLo, fHi := swarPackLanes(32, uint64(math.Float32bits(1.5)), uint64(math.Float32bits(-2.25)), 0, 0)
		m = swarScalarModule(t, wasm.F64, swarV128Const(fLo, fHi), swarFD(95), swarFD(33, 1))
		if got := runProductionSWARWrapper(t, m); got != math.Float64bits(-2.25) {
			t.Fatalf("promote got %#x", got)
		}
	})

	for _, tc := range []struct {
		name string
		sub  uint32
		bits uint64
		want uint32
	}{
		{"f32-signed", 248, uint64(math.Float32bits(-42.75)), 0xffffffd6},
		{"f32-signed-nan", 248, 0x7fc01234, 0},
		{"f32-signed-high", 257, uint64(math.Float32bits(float32(math.Inf(1)))), 0x7fffffff},
		{"f32-unsigned-negative", 249, uint64(math.Float32bits(-1)), 0},
		{"f32-unsigned-high", 258, uint64(math.Float32bits(float32(math.Inf(1)))), 0xffffffff},
		{"f64-signed", 252, math.Float64bits(-42.75), 0xffffffd6},
		{"f64-unsigned-nan", 260, 0x7ff8123456789abc, 0},
		{"f64-unsigned-high", 253, math.Float64bits(math.Inf(1)), 0xffffffff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f64src := tc.sub == 252 || tc.sub == 253 || tc.sub == 259 || tc.sub == 260
			lo, hi := tc.bits, uint64(0)
			if !f64src {
				lo, hi = swarPackLanes(32, tc.bits, 0, 0, 0)
			}
			m := swarScalarModule(t, wasm.I32,
				swarV128Const(lo, hi), swarFD(tc.sub), swarIntegerExtract(32, 0, false))
			if got := uint32(runProductionSWARWrapper(t, m)); got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name string
		sub  uint32
		bits uint32
		want uint64
		f64  bool
	}{
		{"i32-to-f32-s", 250, 0xffffffd6, uint64(math.Float32bits(-42)), false},
		{"i32-to-f32-u", 251, 0xffffffff, uint64(math.Float32bits(float32(uint64(0xffffffff)))), false},
		{"i32-to-f64-s", 254, 0xffffffd6, math.Float64bits(-42), true},
		{"i32-to-f64-u", 255, 0xffffffff, math.Float64bits(float64(uint64(0xffffffff))), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lo, hi := swarPackLanes(32, uint64(tc.bits), 0, 0, 0)
			result, extract := wasm.F32, swarFD(31, 0)
			if tc.f64 {
				result, extract = wasm.F64, swarFD(33, 0)
			}
			m := swarScalarModule(t, result, swarV128Const(lo, hi), swarFD(tc.sub), extract)
			got := runProductionSWARWrapper(t, m)
			if !tc.f64 {
				got = uint64(uint32(got))
			}
			if got != tc.want {
				t.Fatalf("got %#x, want %#x", got, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name string
		sub  uint32
		f64  bool
		want float64
	}{
		{"f32-madd", 261, false, 10},
		{"f32-nmadd", 262, false, 4},
		{"f64-madd", 263, true, 10},
		{"f64-nmadd", 264, true, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			width, result, extract := 32, wasm.F32, swarFD(31, 0)
			bits := func(v float64) uint64 { return uint64(math.Float32bits(float32(v))) }
			if tc.f64 {
				width, result, extract = 64, wasm.F64, swarFD(33, 0)
				bits = func(v float64) uint64 { return math.Float64bits(v) }
			}
			aLo, aHi := swarPackLanes(width, bits(2), 0, 0, 0)
			bLo, bHi := swarPackLanes(width, bits(1.5), 0, 0, 0)
			cLo, cHi := swarPackLanes(width, bits(7), 0, 0, 0)
			m := swarScalarModule(t, result,
				swarV128Const(aLo, aHi), swarV128Const(bLo, bHi), swarV128Const(cLo, cHi), swarFD(tc.sub), extract)
			got := runProductionSWARWrapper(t, m)
			want := bits(tc.want)
			if !tc.f64 {
				got = uint64(uint32(got))
			}
			if got != want {
				t.Fatalf("got %#x, want %#x", got, want)
			}
		})
	}
}

func TestSWARV128SerializedCallABIExec(t *testing.T) {
	const lo, hi = uint64(0x0123456789abcdef), uint64(0xfedcba9876543210)
	t.Run("exported-wrapper", func(t *testing.T) {
		m := productionModule1(t, []wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}, []byte{0, 0x20, 0x00, 0x0b})
		if got := runProductionSWARWrapper(t, m, lo, hi); got != lo {
			t.Fatalf("got %#x, want %#x", got, lo)
		}
	})

	t.Run("param-result", func(t *testing.T) {
		caller := []byte{0}
		caller = append(caller, swarV128Const(lo, hi)...)
		caller = append(caller, 0x10, 0x01) // call function 1
		caller = append(caller, swarFD(29, 1)...)
		caller = append(caller, 0x0b)
		callee := []byte{0, 0x20, 0x00}
		callee = append(callee, swarFD(77)...)
		callee = append(callee, 0x0b)
		m := productionModuleFuncs(t,
			productionFuncDef{results: []wasm.ValType{wasm.I64}, body: caller},
			productionFuncDef{params: []wasm.ValType{wasm.V128}, results: []wasm.ValType{wasm.V128}, body: callee},
		)
		if got := runProductionSWARWrapper(t, m); got != ^hi {
			t.Fatalf("got %#x, want %#x", got, ^hi)
		}
	})

	t.Run("mixed-multivalue", func(t *testing.T) {
		caller := []byte{0}
		caller = append(caller, swarV128Const(lo, hi)...)
		caller = append(caller, 0x10, 0x01, 0x1a) // call; drop trailing i64
		caller = append(caller, swarFD(29, 0)...)
		caller = append(caller, 0x0b)
		callee := []byte{0, 0x20, 0x00, 0x42, 0x2a, 0x0b}
		m := productionModuleFuncs(t,
			productionFuncDef{results: []wasm.ValType{wasm.I64}, body: caller},
			productionFuncDef{params: []wasm.ValType{wasm.V128}, results: []wasm.ValType{wasm.V128, wasm.I64}, body: callee},
		)
		if got := runProductionSWARWrapper(t, m); got != lo {
			t.Fatalf("got %#x, want %#x", got, lo)
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
