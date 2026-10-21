//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
	rt "github.com/wago-org/wago/src/core/runtime"
	"testing"
)

func TestBitsetBrTable(t *testing.T) {
	for _, n := range []int{8, 31, 32, 63, 64} {
		labels := make([]uint32, n)
		for i := range labels {
			if i%3 == 1 || i == n-1 {
				labels[i] = 1
			}
		}
		m := brTableComputedLabelsArm64(t, labels, 0)
		var stats ModuleStats
		opts := CompileOptions{}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		cm, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if diagnosticsEnabled && stats.Funcs[0].Peephole["br-table-bitset"] != 1 {
			t.Fatalf("n=%d missing bitset: %v", n, stats.Funcs[0].Peephole)
		}
		assertBitsetLongMembershipBranch(t, cm.Code)
		for index := uint64(0); index < uint64(n+2); index++ {
			want := uint64(1000)
			if index < uint64(n) && labels[index] == 1 {
				want++
			}
			if got := func() uint64 {
				got, err := runArm64WrapperWithOptions(t, m, opts, index, 1)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}(); got != want {
				t.Fatalf("n=%d index=%d got=%d want=%d", n, index, got, want)
			}
		}
		for _, index := range []uint64{127, 0x80000000, 0xffffffff} {
			if got := func() uint64 {
				got, err := runArm64WrapperWithOptions(t, m, opts, index, 1)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}(); got != 1000 {
				t.Fatalf("index=%x wraps into mask: got=%d", index, got)
			}
		}
	}
}

// The membership edge uses the established imm19 branch path, including for
// void defaults. This is future range hardening, not a reproduced range fault.
func assertBitsetLongMembershipBranch(t *testing.T, code []byte) {
	t.Helper()
	var want encoder.Asm
	want.AndImm64(X16, X16, 1)
	mask := binary.LittleEndian.Uint32(want.B)
	for at := 0; at+8 <= len(code); at += 4 {
		word := binary.LittleEndian.Uint32(code[at:])
		next := binary.LittleEndian.Uint32(code[at+4:])
		if word == mask && next&0xff00001f == 0xb5000010 {
			return
		}
	}
	t.Fatal("bitset dispatch lacks AND x16, x16, #1 followed by CBNZ x16")
}

func TestBitsetVoidDefaultEdges(t *testing.T) {
	for _, edge := range []string{"loop", "block", "function"} {
		for _, interruptible := range []bool{false, true} {
			for _, compact := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/interruptible=%t/compact=%t", edge, interruptible, compact), func(t *testing.T) {
					// Memory records the taken edge of a void function. The loop
					// changes its index to a member after one default iteration.
					store := func(v byte) []byte { return []byte{0x41, 0, 0x41, v, 0x36, 2, 0} }
					body := []byte{0}
					var def, member byte
					switch edge {
					case "loop":
						body = append(body, 0x02, 0x40, 0x03, 0x40)
						body = append(body, 0x41, 0, 0x41, 0, 0x28, 2, 0, 0x41, 1, 0x6a, 0x36, 2, 0)
						body = append(body, 0x20, 0, 0x41, 1, 0x21, 0)
						def, member = 0, 1
					case "block":
						body = append(body, store(1)...)
						body = append(body, 0x02, 0x40, 0x02, 0x40, 0x20, 0)
						def, member = 0, 1
					case "function":
						body = append(body, store(1)...)
						body = append(body, 0x02, 0x40, 0x20, 0)
						def, member = 1, 0
					}
					body = append(body, 0x0e, 8, def, member, def, member, def, member, def, member, def, 0x0b)
					if edge != "loop" {
						body = append(body, store(2)...)
					}
					if edge != "function" {
						body = append(body, 0x0b)
					}
					body = append(body, 0x0b)
					m := modMem(t, 1, []wasm.ValType{wasm.I32}, nil, body)
					var stats ModuleStats
					opts := CompileOptions{Interruptible: interruptible, CompactNative: compact}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					assertBitsetLongMembershipBranch(t, cm.Code)
					if diagnosticsEnabled && stats.Funcs[0].Peephole["br-table-bitset"] != 1 {
						t.Fatalf("inactive dispatch counter: %+v", stats.Funcs[0].Peephole)
					}
					for _, index := range []uint64{0, 1, 7, 8, 63, 64, 0x80000000, 0xffffffff} {
						want := uint32(2)
						if index < 8 && index&1 != 0 {
							want = 1
						}
						if edge == "function" {
							want = 3 - want
						}
						if got := runBitsetVoidMemory(t, cm.Code, cm.Entry[0], index); got != want {
							t.Fatalf("index=%x got=%d want=%d", index, got, want)
						}
					}
				})
			}
		}
	}
}

func runBitsetVoidMemory(t *testing.T, code []byte, offset int, index uint64) uint32 {
	t.Helper()
	engine, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	jm, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	mem, entry, err := rt.MapCode(code)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unmap(mem)
	args := arena.Alloc(8)
	binary.LittleEndian.PutUint64(args, index)
	if err := engine.Call(entry+uintptr(offset), args, jm.LinearMemory(), arena.Alloc(rt.TrapBufferBytes), arena.Alloc(8)); err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(jm.CurrentBytes())
}

func TestBitsetBrTableTypedMerge(t *testing.T) {
	body := []byte{0, 0x02, 0x7e, 0x02, 0x7e, 0x20, 1, 0x20, 0, 0x0e, 8, 0, 1, 0, 1, 0, 1, 0, 1, 0, 0x0b, 0x42, 7, 0x7c, 0x0b, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, body)
	for _, compact := range []bool{false, true} {
		cm, e := CompileModuleWith(m, CompileOptions{CompactNative: compact})
		if e != nil {
			t.Fatal(e)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		for _, value := range []uint64{0, 1, 0x8000000000000000, ^uint64(0)} {
			for _, index := range []uint64{0, 1, 7, 8, 63, 64, 0xffffffff} {
				want := value + 7
				if index < 8 && index%2 == 1 {
					want = value
				}
				if got := func() uint64 {
					got, e := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: compact}, index, value)
					if e != nil {
						t.Fatal(e)
					}
					return got
				}(); got != want {
					t.Fatalf("compact=%v index=%x value=%x got=%x want=%x", compact, index, value, got, want)
				}
			}
		}
	}
}
