//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type swarScalarMemoryCase struct {
	op, class, size byte
	signed, store   bool
}

// Explicit Wasm opcode expectations keep the memory oracle independent of the
// emitter's memoryOpcode table, including raw FP load/store carrier classes.
func swarScalarMemoryCases() []swarScalarMemoryCase {
	return []swarScalarMemoryCase{
		{0x28, 0x7f, 4, false, false}, {0x29, 0x7e, 8, false, false},
		{0x2a, 0x7d, 4, false, false}, {0x2b, 0x7c, 8, false, false},
		{0x2c, 0x7f, 1, true, false}, {0x2d, 0x7f, 1, false, false},
		{0x2e, 0x7f, 2, true, false}, {0x2f, 0x7f, 2, false, false},
		{0x30, 0x7e, 1, true, false}, {0x31, 0x7e, 1, false, false},
		{0x32, 0x7e, 2, true, false}, {0x33, 0x7e, 2, false, false},
		{0x34, 0x7e, 4, true, false}, {0x35, 0x7e, 4, false, false},
		{0x36, 0x7f, 4, false, true}, {0x37, 0x7e, 8, false, true},
		{0x38, 0x7d, 4, false, true}, {0x39, 0x7c, 8, false, true},
		{0x3a, 0x7f, 1, false, true}, {0x3b, 0x7f, 2, false, true},
		{0x3c, 0x7e, 1, false, true}, {0x3d, 0x7e, 2, false, true}, {0x3e, 0x7e, 4, false, true},
	}
}

func swarScalarMemoryFixture(c swarScalarMemoryCase, align byte, offset uint32) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7f, 0x7b, c.class}, results: []byte{0x7b, c.class}, locals: []byte{1, 125, 0x7b}, body: []byte{0x20, 1, 0x20, 0}}
	if c.store {
		f.body = append(f.body, 0x20, 2)
	}
	f.body = append(f.body, c.op, align)
	f.body = uleb(f.body, offset)
	if c.store {
		f.body = append(f.body, 0x20, 2)
	}
	f.body = append(f.body, 0x0b)
	return f
}

func swarScalarMemorySizeFixture(grow bool) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7b, 0x7f}, results: []byte{0x7b, 0x7f}, locals: []byte{1, 126, 0x7b}, body: []byte{0x20, 0}}
	op := byte(0x3f)
	if grow {
		op = 0x40
		f.body = append(f.body, 0x20, 1)
	}
	f.body = append(f.body, op, 0, 0x0b)
	return f
}

func TestSWARScalarMemoryAllClassesAdmissionAndExactGates(t *testing.T) {
	for _, c := range swarScalarMemoryCases() {
		for align := byte(0); align <= swarMemoryAlignment(int(c.size)); align++ {
			fn, ft, memory := swarValidatedMemoryFunction(t, swarScalarMemoryFixture(c, align, 0xffffffff))
			code, info, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true})
			if err != nil || len(code) == 0 || info.FrameBytes <= 2048 {
				t.Fatal("scalar memory in vector function", c, align, info, err)
			}
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("memory access without resource", c, err)
			}
			bad := swarScalarMemoryFixture(c, swarMemoryAlignment(int(c.size))+1, 0)
			if code, _, err := compileSWARPairsWithMemory(&wasm.Func{Locals: fn.Locals, BodyBytes: bad.body}, ft, memory, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("overaligned scalar access emitted", c, err)
			}
		}
	}
	for _, grow := range []bool{false, true} {
		fn, ft, memory := swarValidatedMemoryFunction(t, swarScalarMemorySizeFixture(grow))
		if code, _, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("scalar size/grow lowering", grow, err)
		}
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("size/grow without resource", grow, err)
		}
	}
	// Unexecuted operands cannot bypass precise classes, current control floors,
	// implicit memory0, offset32, immediate completeness or natural alignment.
	ft := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}, Results: []wasm.ValType{wasm.V128}}
	memory := &wasm.MemType{Limits: wasm.Limits{Min: 1, HasMax: true, Max: 1}}
	for _, tail := range [][]byte{
		{0x20, 0, 0x28, 0, 0, 0x1a},                   // vector address
		{0x43, 0, 0, 0, 0, 0x40, 0, 0x1a},             // FP grow delta
		{0x41, 0, 0x20, 0, 0x36, 0, 0},                // vector store payload
		{0x41, 0, 0x41, 0, 0x38, 0, 0},                // integer payload for FP store
		{2, 0x40, 0x28, 0, 0, 0x1a, 0x0b},             // nested dead underflow
		{0x28, 64, 0, 0, 0x1a},                        // explicit memory0 is outside this pilot
		{0x28, 0, 0x80, 0x80, 0x80, 0x80, 0x10, 0x1a}, // offset32 overflow
		{0x28, 0, 0x80}, {0x40, 1, 0x1a},              // truncated/nonzero index
	} {
		body := append(append([]byte{0x20, 0, 0x0f}, tail...), 0x0b)
		if code, _, err := compileSWARPairsWithMemory(&wasm.Func{BodyBytes: body}, ft, memory, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("bad dead scalar memory shape emitted", tail, err)
		}
	}
}
