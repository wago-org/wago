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
