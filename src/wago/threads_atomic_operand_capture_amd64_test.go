//go:build linux && amd64 && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func atomicOperandCaptureModule(sub, align byte, typ wasm.ValType, prefix, shifts int, dropped, shared, addr64 bool, index byte) []byte {
	address, addressConst, addressShift, limits := wasm.I32, byte(0x41), byte(0x74), byte(1)
	if addr64 {
		address, addressConst, addressShift, limits = wasm.I64, 0x42, 0x86, 5
	}
	if shared {
		limits |= 2
	}
	memories := [][]byte{{limits, 1, 1}}
	if index != 0 {
		memories = [][]byte{{1, 1, 1}, {limits, 1, 1}}
	}
	var body []byte
	results := make([]wasm.ValType, prefix)
	for i := range results {
		results[i] = wasm.I32
		constant := byte(0x41)
		if index == 0 {
			constant = addressConst
		}
		body = append(body, constant)
		body = append(body, wasmtest.SLEB32(int32(128+4*i))...)
		body = append(body, 0x28, 2, 0) // live loads occupy registers before the atomic
	}
	body = append(body, 0x20, 0)
	if shifts&1 != 0 {
		body = append(body, 0x20, 1, addressShift)
	}
	body = append(body, 0x20, 2)
	if shifts&2 != 0 {
		body = append(body, 0x20, 1)
		if address != typ {
			if typ == wasm.I32 {
				body = append(body, 0xa7) // i32.wrap_i64
			} else {
				body = append(body, 0xad) // i64.extend_i32_u
			}
		}
		shift := byte(0x74)
		if typ == wasm.I64 {
			shift = 0x86
		}
		body = append(body, shift)
	}
	if sub >= 0x48 {
		body = append(body, 0x20, 3) // replacement remains live during earlier expressions
	}
	body = append(body, 0xfe, sub, align|64, index, 0)
	if sub >= 0x1e {
		if dropped {
			body = append(body, 0x1a)
		} else {
			results = append(results, typ)
		}
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{address, address, typ, typ}, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memories...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("target", 2, uint32(index)))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestThreadsAtomicDeferredOperandCapture(t *testing.T) {
	widths := []struct {
		name  string
		typ   wasm.ValType
		align byte
	}{
		{"i32", wasm.I32, 2}, {"i64", wasm.I64, 3},
		{"i32_8", wasm.I32, 0}, {"i32_16", wasm.I32, 1},
		{"i64_8", wasm.I64, 0}, {"i64_16", wasm.I64, 1}, {"i64_32", wasm.I64, 2},
	}
	for _, memory := range []struct {
		name           string
		shared, addr64 bool
		index          byte
	}{
		{"private32", false, false, 0}, {"shared32", true, false, 0},
		{"private64", false, true, 0}, {"indexed_shared64", true, true, 1},
	} {
		for width, w := range widths {
			for op, name := range []string{"store", "add", "sub", "and", "or", "xor", "xchg", "cmpxchg"} {
				for _, prefix := range []int{5, 7} {
					for _, shifts := range []int{1, 2, 3} {
						t.Run(fmt.Sprintf("%s/%s/%s/prefix=%d/shifts=%d", memory.name, w.name, name, prefix, shifts), func(t *testing.T) {
							// Exercise the direct locked-ALU path as well as observed RMW results.
							for _, dropped := range []bool{false, true} {
								if dropped && (op == 0 || op == 7) {
									continue
								}
								sub := byte(0x17 + 7*op + width)
								testAtomicOperandCapture(t, sub, w.align, w.typ, op, prefix, shifts, dropped, memory.shared, memory.addr64, memory.index)
							}
						})
					}
				}
			}
		}
	}
}

func testAtomicOperandCapture(t *testing.T, sub, align byte, typ wasm.ValType, op, prefix, shifts int, dropped, shared, addr64 bool, index byte) {
	t.Helper()
	module := atomicOperandCaptureModule(sub, align, typ, prefix, shifts, dropped, shared, addr64, index)
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	target, err := in.ExportedMemory("target")
	if err != nil {
		t.Fatal(err)
	}
	mem := target.UnsafeBytes()
	size := 1 << align
	mask := ^uint64(0) >> (64 - 8*size)
	for _, tc := range []struct {
		name                  string
		address, count, value uint64
		mismatch              bool
	}{
		{"zero_shift", 8, 0, 7, false},
		{"shift", 8, 1, 0x8765432187654321, false},
		{"mismatch", 8, 0, 7, true},
		{"unaligned", 1, 0, 7, false},
		{"bounds", 65536, 0, 7, false},
		{"high_address", ^uint64(0), 0, 7, false},
	} {
		for i := range mem {
			mem[i] = 0xa5
		}
		for i := 0; i < prefix; i++ {
			binary.LittleEndian.PutUint32(in.Memory().UnsafeBytes()[128+4*i:], uint32(100+i))
		}
		address, value := tc.address, tc.value
		if !addr64 {
			address = uint64(uint32(address))
		}
		if shifts&1 != 0 {
			address <<= tc.count
		}
		if shifts&2 != 0 {
			value <<= tc.count
		}
		if typ == wasm.I32 {
			value = uint64(uint32(value))
		}
		old, replacement := value&mask, uint64(13)
		if tc.mismatch {
			old ^= 1
		}
		valid := address <= uint64(len(mem)-size) && address%uint64(size) == 0
		if valid {
			for i := 0; i < size; i++ {
				mem[int(address)+i] = byte(old >> (8 * i))
			}
		}
		want := bytes.Clone(mem)
		updated := value
		switch op {
		case 1:
			updated = old + value
		case 2:
			updated = old - value
		case 3:
			updated = old & value
		case 4:
			updated = old | value
		case 5:
			updated = old ^ value
		case 7:
			updated = old
			if old == value&mask {
				updated = replacement
			}
		}
		if valid {
			for i := 0; i < size; i++ {
				want[int(address)+i] = byte(updated >> (8 * i))
			}
		}
		got, err := in.Invoke("run", tc.address, tc.count, tc.value, replacement)
		if valid {
			resultCount := prefix
			if op != 0 && !dropped {
				resultCount++
			}
			if err != nil || len(got) != resultCount {
				t.Fatalf("%s dropped=%t: results=%v, error=%v", tc.name, dropped, got, err)
			}
			for i := 0; i < prefix; i++ {
				if uint32(got[i]) != uint32(100+i) {
					t.Fatalf("%s: live result[%d]=%d", tc.name, i, got[i])
				}
			}
			if resultCount > prefix && got[prefix] != old {
				t.Fatalf("%s: old=%#x, want %#x", tc.name, got[prefix], old)
			}
		} else {
			code := TrapLinMemOutOfBounds
			if address < uint64(len(mem)) {
				code = TrapAtomicUnaligned
			}
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != code {
				t.Fatalf("%s: error=%v, want trap %v", tc.name, err, code)
			}
		}
		if !bytes.Equal(mem, want) {
			t.Fatalf("%s dropped=%t: memory[%d:%d]=%x, want %x (full memory differs)", tc.name, dropped, 8, 24, mem[8:24], want[8:24])
		}
	}
}
