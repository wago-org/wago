//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func swarLaneMemoryFixture(kind, align, offset uint32, lane byte) fixtureFunc {
	body := uleb([]byte{0x20, 0, 0x20, 1, 0xfd}, kind)
	body = uleb(body, align)
	body = uleb(body, offset)
	body = append(body, lane)
	if kind >= 88 {
		body = append(body, 0x20, 1)
	}
	return fixtureFunc{params: []byte{0x7f, 0x7b}, result: 0x7b, body: append(body, 0x0b)}
}

func TestSWARLaneAndSplatMemoryAdmission(t *testing.T) {
	for kind := uint32(7); kind <= 10; kind++ {
		maxAlign, _ := swarMemoryOpcode(kind)
		for align := uint32(0); align <= maxAlign; align++ {
			fixture := swarPartialLoadFixture(kind, align, ^uint32(0))
			fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
			if code, _, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true}); len(code) == 0 || err != nil {
				t.Fatal("valid splat load", kind, align, err)
			}
		}
	}
	for kind := uint32(84); kind <= 91; kind++ {
		maxAlign, _ := swarMemoryOpcode(kind)
		for lane := byte(0); uint32(lane) < 16>>maxAlign; lane++ {
			for align := uint32(0); align <= maxAlign; align++ {
				fixture := swarLaneMemoryFixture(kind, align, ^uint32(0), lane)
				fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
				if code, _, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true}); len(code) == 0 || err != nil {
					t.Fatal("valid lane memory", kind, align, lane, err)
				}
			}
		}
		fixture := swarLaneMemoryFixture(kind, 0, 0, 0)
		fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("lane memory missing resource", kind, err)
		}
		if compiled, err := CompileProduction(stateModule([]fixtureFunc{fixture}, false, 1, 1, true, nil, nil), ProductionOptions{DeferCodeMapping: true}); compiled != nil || err == nil {
			t.Fatal("public SIMD policy widened", kind, err)
		}
		for _, typ := range []wasm.ValType{wasm.I64, wasm.F32, wasm.F64, wasm.V128} {
			badType := *ft
			badType.Params = []wasm.ValType{typ, wasm.V128}
			if code, _, err := compileSWARPairsWithMemory(fn, &badType, memory, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("lane address class", kind, typ, err)
			}
		}
		for _, body := range [][]byte{
			swarLaneMemoryFixture(kind, 0, 0, byte(16>>maxAlign)).body,
			swarLaneMemoryFixture(kind, maxAlign+1, 0, 0).body,
			swarLaneMemoryFixture(kind, 64, 0, 0).body,
			append(uleb([]byte{0x20, 0, 0x20, 1, 0xfd}, kind), 0, 0),    // missing lane and end
			append(uleb([]byte{0x20, 0, 0x20, 1, 0xfd}, kind), 0, 0, 0), // complete lane, missing end
		} {
			bad := *fn
			bad.BodyBytes = body
			if code, _, err := compileSWARPairsWithMemory(&bad, ft, memory, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("bad lane memarg emitted", kind, body, err)
			}
		}
		for _, prefix := range [][]byte{nil, {0x20, 0}, {0x20, 0, 0x20, 0}, {0x20, 1, 0x20, 0}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[4:]...)
			if code, _, err := compileSWARPairsWithMemory(&bad, ft, memory, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("lane carrier/underflow", kind, prefix, err)
			}
		}
	}
}

func TestSWARLaneMemoryHelperErrorsBeforeBytes(t *testing.T) {
	for _, test := range []struct {
		offset      uint64
		width, lane uint8
	}{{0, 0, 0}, {0, 24, 0}, {0, 8, 16}, {0, 16, 8}, {0, 32, 4}, {0, 64, 2}, {1 << 32, 8, 0}} {
		for _, store := range []bool{false, true} {
			f := integerEmitter{}
			f.a.Addi(enc.X5, enc.Zero, 0)
			before := len(f.a.B)
			f.swarLaneMemory(test.offset, test.width, test.lane, store)
			if f.err == nil || len(f.a.B) != before {
				t.Fatal("invalid lane memory partially emitted", test, store, f.err)
			}
			f.swarLaneMemory(0, 8, 0, false)
			f.swarSplatMemory(0, 8)
			if len(f.a.B) != before {
				t.Fatal("sticky lane memory error appended")
			}
		}
	}
	for _, test := range []struct {
		offset uint64
		width  uint8
	}{{0, 0}, {0, 24}, {0, 128}, {1 << 32, 8}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarSplatMemory(test.offset, test.width)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid splat memory partially emitted", test, f.err)
		}
	}
}
