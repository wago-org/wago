//go:build (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestMemoryCopyPreservesCachedValues(t *testing.T) {
	copyBody := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0a, 0, 0}
	for _, tc := range []struct {
		name                  string
		typ                   wasm.ValType
		constOp, mulOp, addOp byte
		arg, want             uint64
		params                int
	}{
		{"f64", wasm.F64, 0x44, 0xa2, 0xa0, F64(2), F64(200), 1},
		{"f32", wasm.F32, 0x43, 0x94, 0x92, F32(2), F32(200), 1},
		{"f64-pressure", wasm.F64, 0x44, 0xa2, 0xa0, F64(2), F64(4800), 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}
			args := make([]uint64, tc.params)
			body := append([]byte(nil), copyBody...)
			for i := 0; i < tc.params; i++ {
				params = append(params, tc.typ)
				args[i] = tc.arg
				body = append(body, 0x20, byte(3+i))
				if tc.params > 1 {
					body = append(body, 0x20, byte(3+i), tc.addOp)
				}
				if i > 0 {
					body = append(body, tc.addOp)
				}
			}
			body = append(body, tc.constOp)
			if tc.typ == wasm.F64 {
				body = binary.LittleEndian.AppendUint64(body, math.Float64bits(100))
			} else {
				body = binary.LittleEndian.AppendUint32(body, math.Float32bits(100))
			}
			body = append(body, tc.mulOp, 0x0b)
			runBulkCopyOwnerCases(t, bulkCopyOwnerModule(params, tc.typ, body), args, tc.want)
		})
	}
	t.Run("live-and-cached-v128", func(t *testing.T) {
		constant := []byte{0xfd, 0x0c}
		constant = binary.LittleEndian.AppendUint64(constant, 0x123456789abcdef0)
		constant = binary.LittleEndian.AppendUint64(constant, 0xfedcba9876543210)
		body := append([]byte(nil), constant...)
		body = append(body, copyBody...)
		body = append(body, constant...)
		body = append(body, 0xfd, 0x51, 0xfd, 0x1d, 0, 0x0b) // xor; i64x2.extract_lane 0
		runBulkCopyOwnerCases(t, bulkCopyOwnerModule(
			[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, wasm.I64, body), nil, 0)
	})
}

func bulkCopyOwnerModule(params []wasm.ValType, result wasm.ValType, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{result}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func runBulkCopyOwnerCases(t *testing.T, module []byte, extraArgs []uint64, wantResult uint64) {
	t.Helper()
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if GuardPageSupported() {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, mode := range modes {
		t.Run(fmt.Sprintf("bounds=%d", mode), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(mode), module)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			mem := instance.Memory().UnsafeBytes()
			for _, n := range []int{0, 1, 7, 8, 15, 16, 23, 24, 31, 248, 255, 256, 257, 1023, 1024, 1025, 4096} {
				for _, offsets := range [][2]int{{1, 0}, {0, 1}, {0, 0}, {8192, 0}} {
					dst, src := offsets[0], offsets[1]
					t.Run(fmt.Sprintf("n=%d/dst=%d/src=%d", n, dst, src), func(t *testing.T) {
						for i := range mem {
							mem[i] = byte(i*31 + 7)
						}
						want := append([]byte(nil), mem...)
						copy(want[dst:dst+n], want[src:src+n])
						args := append([]uint64{I32(int32(dst)), I32(int32(src)), I32(int32(n))}, extraArgs...)
						got, err := instance.Invoke("run", args...)
						if err != nil {
							t.Fatal(err)
						}
						if len(got) != 1 || got[0] != wantResult {
							t.Errorf("result bits = %x; want %x", got, wantResult)
						}
						if !bytes.Equal(mem, want) {
							t.Error("memory.copy differs from Go copy")
						}
					})
				}
			}
		})
	}
}

func FuzzMemoryCopyCachedValueAndRanges(f *testing.F) {
	for _, seed := range [][3]uint16{{0, 0, 0}, {1, 0, 8}, {0, 1, 15}, {1, 0, 256}, {1, 0, 1024}, {65535, 0, 2}, {0, 65535, 2}} {
		f.Add(seed[0], seed[1], seed[2])
	}
	body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0a, 0, 0, 0x20, 3, 0x44}
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(100))
	body = append(body, 0xa2, 0x0b)
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), bulkCopyOwnerModule(
		[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.F64}, wasm.F64, body))
	if err != nil {
		f.Fatal(err)
	}
	defer compiled.Close()
	f.Fuzz(func(t *testing.T, dst, src, length uint16) {
		n := int(length) % 4097
		instance, err := Instantiate(compiled)
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		memory := instance.Memory().UnsafeBytes()
		for i := range memory {
			memory[i] = byte(i*37 + 11)
		}
		want := bytes.Clone(memory)
		valid := int(dst)+n <= len(memory) && int(src)+n <= len(memory)
		if valid {
			copy(want[int(dst):int(dst)+n], want[int(src):int(src)+n])
		}
		got, err := instance.Invoke("run", uint64(dst), uint64(src), uint64(n), F64(2))
		if valid {
			if err != nil || len(got) != 1 || got[0] != F64(200) {
				t.Fatalf("copy result = %x, %v; want 200", got, err)
			}
		} else if err == nil {
			t.Fatal("copy accepted an out-of-bounds range")
		}
		if !bytes.Equal(memory, want) {
			t.Fatal("copy differs from the Go oracle or wrote before a bounds trap")
		}
	})
}
