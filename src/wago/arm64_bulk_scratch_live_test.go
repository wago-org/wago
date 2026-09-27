//go:build (linux || darwin || windows) && arm64 && !tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Sixteen computed locals stay live across the helper, then are read both by
// stores and by a sum. This exceeds the ordinary pin pool, reaching X12/X13/X14
// when the helper's scratch requirement is missing from the function hints.
func arm64BulkScratchLiveModule(operation string, inline bool) []byte {
	var op []byte
	switch operation {
	case "scalar":
	case "fill", "copy", "init", "table.copy", "table.init":
		op = []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc}
		switch operation {
		case "fill":
			op = append(op, 11, 0)
		case "copy":
			op = append(op, 10, 0, 0)
		case "init":
			op = append(op, 8, 0, 0)
		case "table.copy":
			op = append(op, 14, 0, 0)
		case "table.init":
			op = append(op, 12, 0, 0)
		}
	case "data.drop":
		op = []byte{0xfc, 9, 0}
	case "elem.drop":
		op = []byte{0xfc, 13, 0}
	case "table.fill":
		op = []byte{0x20, 0, 0xd0, 0x70, 0x20, 2, 0xfc, 17, 0}
	default:
		panic(operation)
	}
	body := []byte{1, 16, 0x7f}
	for i := byte(0); i < 16; i++ {
		body = append(body, 0x20, 3, 0x41, i+1, 0x6a, 0x21, i+4)
	}
	if inline {
		body = append(body, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1)
	} else {
		body = append(body, op...)
	}
	for i := byte(0); i < 16; i++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(8192)...)
		body = append(body, 0x20, i+4, 0x36, 2, i*4)
	}
	body = append(body, 0x41, 0)
	for i := byte(0); i < 16; i++ {
		body = append(body, 0x20, i+4, 0x6a)
	}
	body = append(body, 0x0b)
	codes := [][]byte{append(wasmtest.ULEB(uint32(len(body))), body...)}
	funcs := [][]byte{{0}}
	if inline {
		helper := append([]byte{0}, op...)
		helper = append(helper, 0x0b)
		codes = append(codes, append(wasmtest.ULEB(uint32(len(helper))), helper...))
		funcs = append(funcs, []byte{1})
	}
	// Passive segments keep init/drop legal; no start function changes pinning.
	data := append([]byte{1}, wasmtest.ULEB(4096)...)
	data = append(data, make([]byte, 4096)...)
	elem := append([]byte{1, 0}, wasmtest.ULEB(4096)...)
	elem = append(elem, make([]byte, 4096)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 0x80, 0x20})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("probe", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(12, []byte{1}),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
		wasmtest.Section(11, wasmtest.Vec(data)),
	)
}

func TestARM64BulkScratchPreservesLiveLocals(t *testing.T) {
	for _, op := range []string{"scalar", "fill", "copy", "init", "data.drop", "table.fill", "table.copy", "table.init", "elem.drop"} {
		for _, inline := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/inline=%t", op, inline), func(t *testing.T) {
				compiled, err := Compile(NewRuntimeConfig(), arm64BulkScratchLiveModule(op, inline))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				instance, err := Instantiate(compiled)
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				for _, length := range []uint64{0, 1, 16, 1024} {
					for _, seed := range []uint32{1, 0x1234567} {
						got, err := instance.Invoke("probe", 64, 32, length, uint64(seed))
						if err != nil {
							t.Fatal(err)
						}
						if want := seed*16 + 136; uint32(got[0]) != want {
							t.Errorf("length %d seed %#x: result %#x, want %#x", length, seed, got[0], want)
						}
						memory := instance.Memory().UnsafeBytes()[8192:]
						for i := uint32(0); i < 16; i++ {
							if got := binary.LittleEndian.Uint32(memory[i*4:]); got != seed+i+1 {
								t.Errorf("length %d local %d: %#x, want %#x", length, i+4, got, seed+i+1)
							}
						}
					}
				}
			})
		}
	}
}

func TestARM64SegmentDropPreservesLiveLocalsWithoutMemoryOrTable(t *testing.T) {
	for _, elem := range []bool{false, true} {
		t.Run(fmt.Sprintf("element=%t", elem), func(t *testing.T) {
			body := []byte{1, 16, 0x7f}
			for i := byte(1); i <= 16; i++ {
				body = append(body, 0x20, 0, 0x41, i, 0x6a, 0x21, i)
			}
			sub := byte(9)
			if elem {
				sub = 13
			}
			body = append(body, 0xfc, sub, 0, 0x41, 0)
			for i := byte(1); i <= 16; i++ {
				body = append(body, 0x20, i, 0x6a)
			}
			body = append(body, 0x0b)
			sections := [][]byte{
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("probe", 0, 0))),
			}
			if elem {
				sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{1, 0, 0})))
			} else {
				sections = append(sections, wasmtest.Section(12, []byte{1}))
			}
			sections = append(sections, wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
			if !elem {
				sections = append(sections, wasmtest.Section(11, wasmtest.Vec([]byte{1, 0})))
			}
			compiled, err := Compile(NewRuntimeConfig(), wasmtest.Module(sections...))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("probe", 1)
			if err != nil {
				t.Fatal(err)
			}
			if got[0] != 152 {
				t.Fatalf("result = %#x, want 152", got[0])
			}
		})
	}
}

func BenchmarkARM64BulkScratchLiveLocals(b *testing.B) {
	for _, op := range []string{"scalar", "fill", "init"} {
		b.Run(op, func(b *testing.B) {
			compiled, err := Compile(NewRuntimeConfig(), arm64BulkScratchLiveModule(op, false))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled)
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			fn, err := instance.WasmFunc("probe")
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := fn.Invoke(64, 32, 16, 1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
