//go:build (linux || darwin || windows) && arm64 && !tinygo

package wago

import (
	"encoding/binary"
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestARM64MemoryFillPreservesLiveIntegerValues(t *testing.T) {
	body := []byte{1, 9, 0x7f}
	body = append(body,
		0x20, 3, 0x41, 3, 0x6c, 0x21, 4,
		0x20, 3, 0x41, 7, 0x6a, 0x21, 5,
		0x20, 3, 0x41,
	)
	body = append(body, wasmtest.SLEB32(0x5a5a)...)
	body = append(body,
		0x73, 0x21, 6,
		0x20, 3, 0x41, 3, 0x74, 0x21, 7,
		0x20, 3, 0x41, 11, 0x6b, 0x21, 8,
		0x20, 3, 0x41, 17, 0x6c, 0x21, 9,
		0x20, 3, 0x41,
	)
	body = append(body, wasmtest.SLEB32(0x100)...)
	body = append(body,
		0x72, 0x21, 10,
		0x20, 3, 0x41, 5, 0x77, 0x21, 11,
		0x20, 0, 0x20, 2, 0x6a, 0x21, 12,
		0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 0x0b, 0,
	)
	for local, offset := byte(4), byte(0); local <= 11; local, offset = local+1, offset+4 {
		body = append(body, 0x20, 12, 0x20, local, 0x36, 2, offset)
	}
	body = append(body,
		0x20, 4, 0x20, 5, 0x6a,
		0x20, 6, 0x20, 7, 0x6a, 0x6a,
		0x20, 8, 0x20, 9, 0x6a,
		0x20, 10, 0x20, 11, 0x20, 12, 0x6a, 0x6a, 0x6a, 0x6a,
		0x0b,
	)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("probe", 0, 0), wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig(), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	const dst, length = uint32(64), uint32(0)
	for _, seed := range []uint32{1, 2, 0x1234567} {
		want := []uint32{
			seed * 3,
			seed + 7,
			seed ^ 0x5a5a,
			seed << 3,
			seed - 11,
			seed * 17,
			seed | 0x100,
			bits.RotateLeft32(seed, 5),
		}
		wantResult := dst + length
		for _, value := range want {
			wantResult += value
		}
		got, err := instance.Invoke("probe", uint64(dst), 255, uint64(length), uint64(seed))
		if err != nil {
			t.Fatalf("seed %#x: %v", seed, err)
		}
		if gotResult := uint32(got[0]); gotResult != wantResult {
			t.Errorf("seed %#x: result = %#x, want %#x", seed, gotResult, wantResult)
		}
		memory := instance.Memory().UnsafeBytes()[dst+length : dst+length+32]
		for i, wantValue := range want {
			if gotValue := binary.LittleEndian.Uint32(memory[i*4:]); gotValue != wantValue {
				t.Errorf("seed %#x: value %d = %#x, want %#x", seed, i, gotValue, wantValue)
			}
		}
	}
}
