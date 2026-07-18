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
