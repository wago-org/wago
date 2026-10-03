//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestStoreValueLoadFold(t *testing.T) {
	body := []byte{0x00,
		0x20, 0x00, // destination address
		0x20, 0x01, 0x28, 0x02, 0x00, // first i32.load
		0x41, 0x07, // constant addend
		0x6a,             // i32.add
		0x36, 0x02, 0x00, // i32.store
		0x20, 0x00, 0x28, 0x02, 0x00, // read the stored result
		0x0b,
	}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	saved := storeValueLoadFoldEnabled
	defer func() { storeValueLoadFoldEnabled = saved }()
	storeValueLoadFoldEnabled = false
	off := compileWithStats(t, m, false).Funcs[0].MemRefsForcedByStore
	storeValueLoadFoldEnabled = true
	on := compileWithStats(t, m, false).Funcs[0].MemRefsForcedByStore
	if off != 1 || on != 0 {
		t.Fatalf("forced pre-store loads: off=%d on=%d, want 1/0", off, on)
	}
	result, memory, err := runMemAmd64(t, m, func(mem []byte) {
		binary.LittleEndian.PutUint32(mem[8:], 5)
	}, 0, 8)
	if err != nil || result != 12 || binary.LittleEndian.Uint32(memory) != 12 {
		t.Fatalf("result=%d stored=%d err=%v, want 12/12", result, binary.LittleEndian.Uint32(memory), err)
	}
	twoLoads := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil, []byte{0x00,
		0x20, 0x00,
		0x20, 0x01, 0x28, 0x02, 0x00,
		0x20, 0x02, 0x28, 0x02, 0x00,
		0x6a, 0x36, 0x02, 0x00, 0x0b,
	})
	if got := compileWithStats(t, twoLoads, true).Funcs[0].MemRefsForcedByStore; got != 1 {
		t.Fatalf("two trapping loads forced before store = %d, want 1", got)
	}
	_, memory, err = runMemAmd64(t, twoLoads, func(mem []byte) {
		binary.LittleEndian.PutUint32(mem[8:], 5)
		binary.LittleEndian.PutUint32(mem[16:], 7)
	}, 0, 8, 16)
	if err != nil || binary.LittleEndian.Uint32(memory) != 12 {
		t.Fatalf("two-load store = %d, err=%v, want 12", binary.LittleEndian.Uint32(memory), err)
	}
}
