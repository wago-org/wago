//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func swarBulkMemoryFixture(fill bool) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7b, 0x7f, 0x7f, 0x7f}, result: 0x7b, locals: []byte{1, 124, 0x7b}, body: []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0xfc}}
	if fill {
		f.body = append(f.body, 11, 0)
	} else {
		f.body = append(f.body, 10, 0, 0)
	}
	f.body = append(f.body, 0x0b)
	return f
}

func TestSWARBulkMemoryAdmissionAndSourceGates(t *testing.T) {
	for _, fill := range []bool{false, true} {
		fn, ft, memory := swarValidatedMemoryFunction(t, swarBulkMemoryFixture(fill))
		for _, mode := range []emissionMode{{}, {production: true}, {production: true, interruptible: true}} {
			code, info, err := compileSWARPairsWithMemory(fn, ft, memory, mode)
			if mode.production && mode.interruptible {
				if err != nil || len(code) == 0 || info.FrameBytes <= 2048 {
					t.Fatal("bounded interruptible bulk memory", fill, info, err)
				}
			} else if code != nil || err == nil {
				t.Fatal("bulk without production interruption", fill, mode, err)
			}
		}
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true, interruptible: true}); code != nil || err == nil {
			t.Fatal("bulk without memory", fill, err)
		}
	}
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.V128}}
	memory := &wasm.MemType{Limits: wasm.Limits{Min: 1, HasMax: true, Max: 1}}
	for _, tail := range [][]byte{
		{0x20, 0, 0xfc, 11, 0},                            // known V cannot become an I32 count
		{2, 0x40, 0xfc, 10, 0, 0, 0x0b},                   // nested ordinary stack underflow
		{0xfc, 10, 1, 0}, {0xfc, 10, 0, 1}, {0xfc, 11, 1}, // distinct/nonzero memories
		{0xfc, 10, 0}, {0xfc, 11, 0x80}, // incomplete immediates
		{0xfc, 8, 0, 0}, {0xfc, 9, 0}, // data initializers/drop remain closed
	} {
		body := append(append([]byte{0x20, 0, 0x0f}, tail...), 0x0b)
		if code, _, err := compileSWARPairsWithMemory(&wasm.Func{BodyBytes: body}, ft, memory, emissionMode{production: true, interruptible: true}); code != nil || err == nil {
			t.Fatal("bad dead bulk memory emitted", tail, err)
		}
	}
}
