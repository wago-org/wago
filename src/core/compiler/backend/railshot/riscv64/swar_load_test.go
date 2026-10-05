//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var swarPartialLoadKinds = []uint32{1, 2, 3, 4, 5, 6, 92, 93}

func swarPartialLoadFixture(kind, align, offset uint32) fixtureFunc {
	body := uleb([]byte{0x20, 0, 0xfd}, kind)
	body = uleb(body, align)
	body = uleb(body, offset)
	return fixtureFunc{params: []byte{0x7f}, result: 0x7b, body: append(body, 0x0b)}
}

func TestSWARPartialLoadAdmissionAndMemargs(t *testing.T) {
	for _, kind := range swarPartialLoadKinds {
		maxAlign, ok := swarMemoryOpcode(kind)
		if !ok {
			t.Fatal("partial load not mapped", kind)
		}
		for align := uint32(0); align <= maxAlign; align++ {
			fixture := swarPartialLoadFixture(kind, align, ^uint32(0))
			fn, ft, memory := swarValidatedMemoryFunction(t, fixture)
			if code, _, err := compileSWARPairsWithMemory(fn, ft, memory, emissionMode{production: true}); len(code) == 0 || err != nil {
				t.Fatal("valid partial load", kind, align, err)
			}
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("partial load without memory", kind, err)
			}
			if compiled, err := CompileProduction(stateModule([]fixtureFunc{fixture}, false, 1, 1, true, nil, nil), ProductionOptions{DeferCodeMapping: true}); compiled != nil || err == nil {
				t.Fatal("public SIMD policy widened", kind, err)
			}
		}
		fn, ft, memory := swarValidatedMemoryFunction(t, swarPartialLoadFixture(kind, 0, 0))
		for _, typ := range []wasm.ValType{wasm.I64, wasm.F32, wasm.F64, wasm.V128} {
			badType := *ft
			badType.Params = []wasm.ValType{typ}
			if code, _, err := compileSWARPairsWithMemory(fn, &badType, memory, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("partial address class", kind, typ, err)
			}
		}
		prefix := uleb([]byte{0x20, 0, 0xfd}, kind)
		for _, tail := range [][]byte{nil, {0}, {0, 0x80}, {64, 0, 0, 0x0b}, {0, 0x80, 0x80, 0x80, 0x80, 0x10, 0x0b}, {0, 0, 0xfd, 227, 1, 0x0b}, {byte(maxAlign + 1), 0, 0x0b}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), tail...)
			if code, _, err := compileSWARPairsWithMemory(&bad, ft, memory, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("malformed/unsupported partial memarg", kind, tail, err)
			}
		}
		for _, badPrefix := range [][]byte{nil, {0x20, 0, 0xfd, 0, 0, 0}} {
			bad := *fn
			bad.BodyBytes = append(uleb(append(append([]byte(nil), badPrefix...), 0xfd), kind), 0, 0, 0x0b)
			if code, _, err := compileSWARPairsWithMemory(&bad, ft, memory, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("partial load address underflow/vector", kind, err)
			}
		}
	}
}

func TestSWARMemoryFootprintAndHelperAdmission(t *testing.T) {
	for _, test := range []struct{ kind, align uint32 }{{0, 4}, {11, 4}, {1, 3}, {2, 3}, {3, 3}, {4, 3}, {5, 3}, {6, 3}, {92, 2}, {93, 3}} {
		if align, ok := swarMemoryOpcode(test.kind); !ok || align != test.align {
			t.Fatal(test, align, ok)
		}
	}
	for _, kind := range []uint32{7, 10, 12, 91, 94, ^uint32(0)} {
		if _, ok := swarMemoryOpcode(kind); ok {
			t.Fatal("pending memory op mapped", kind)
		}
	}
	for _, kind := range []uint32{0, 7, 11, 12, 91, 94, ^uint32(0)} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarPartialLoad(0, kind)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid partial helper emitted", kind, f.err)
		}
		f.swarPartialLoad(0, 92)
		if len(f.a.B) != before {
			t.Fatal("sticky partial helper error appended")
		}
	}
	for _, test := range []struct {
		offset    uint64
		footprint uint8
	}{{1 << 32, 8}, {0, 0}, {0, 3}, {0, 7}, {0, 17}, {0, 255}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarMemoryAddress(test.offset, test.footprint)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid preflight partially emitted", test, f.err)
		}
		f.swarMemory(0, false)
		if len(f.a.B) != before {
			t.Fatal("sticky preflight appended")
		}
	}
}
