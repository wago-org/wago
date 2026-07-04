package frontend

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// Frontend profiling quickstart:
//
// 	go test ./src/core/compiler/frontend -bench=. -benchmem
// 	go test ./src/core/compiler/frontend -bench=BenchmarkDecodeValidate -benchmem -cpuprofile cpu.out -memprofile mem.out

var benchModuleSink *wasm.Module

func BenchmarkDecodeValidateSmallScalar(b *testing.B) {
	benchmarkDecodeValidateBytes(b, benchFrontendSmallScalarModuleBytes())
}

func BenchmarkDecodeValidateMediumControl(b *testing.B) {
	benchmarkDecodeValidateBytes(b, benchFrontendMediumControlModuleBytes())
}

func BenchmarkDecodeValidateSIMDHeavy(b *testing.B) {
	benchmarkDecodeValidateBytes(b, benchFrontendSIMDHeavyModuleBytes())
}

func benchmarkDecodeValidateBytes(b *testing.B, data []byte) {
	b.Helper()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, err := DecodeValidate(data)
		if err != nil {
			b.Fatal(err)
		}
		benchModuleSink = m
	}
}

func benchFrontendSmallScalarModuleBytes() []byte {
	body := append([]byte{0x00},
		0x20, 0x00, // local.get 0 (address)
		0x20, 0x01, // local.get 1 (value)
		0x36, 0x02, 0x00, // i32.store align=4 offset=0
		0x20, 0x00, // local.get 0
		0x28, 0x02, 0x00, // i32.load align=4 offset=0
		0x20, 0x01, // local.get 1
		0x6a, // i32.add
		0x0b, // end
	)
	return benchFrontendModuleBytes([]benchFrontendFuncDef{{
		params:  []wasm.ValType{wasm.I32, wasm.I32},
		results: []wasm.ValType{wasm.I32},
		body:    body,
	}}, true)
}

func benchFrontendMediumControlModuleBytes() []byte {
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 local
		0x41, 0x00, // i32.const 0
		0x21, 0x01, // local.set 1
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x01, // local.get 1
		0x20, 0x00, // local.get 0
		0x4e,       // i32.ge_s
		0x0d, 0x01, // br_if 1
		0x20, 0x01, // local.get 1
		0x41, 0x01, // i32.const 1
		0x6a,       // i32.add
		0x21, 0x01, // local.set 1
		0x0c, 0x00, // br 0
		0x0b,       // end loop
		0x0b,       // end block
		0x20, 0x01, // local.get 1
		0x0b, // end function
	}
	return benchFrontendModuleBytes([]benchFrontendFuncDef{{
		params:  []wasm.ValType{wasm.I32},
		results: []wasm.ValType{wasm.I32},
		body:    body,
	}}, false)
}

func benchFrontendSIMDHeavyModuleBytes() []byte {
	body := []byte{0x00}
	body = append(body,
		0x41, 0x20, // i32.const 32
	)
	body = append(body, benchFrontendV128Const(0x1011121314151617, 0x18191a1b1c1d1e1f)...)
	body = append(body,
		0xfd, 0x0b, 0x04, 0x00, // v128.store align=16 offset=0
	)
	body = append(body, benchFrontendV128Const(0x0001020304050607, 0x08090a0b0c0d0e0f)...)
	body = append(body, benchFrontendV128Const(0x2021222324252627, 0x28292a2b2c2d2e2f)...)
	body = append(body, benchFrontendFD(174)...) // i32x4.add
	body = append(body, benchFrontendV128Const(0x3031323334353637, 0x38393a3b3c3d3e3f)...)
	body = append(body, benchFrontendFD(55)...) // i32x4.eq
	body = append(body, benchFrontendV128Const(0x4041424344454647, 0x48494a4b4c4d4e4f)...)
	body = append(body, benchFrontendFD(81)...) // v128.xor
	body = append(body,
		0x41, 0x00, // i32.const 0
		0xfd, 0x00, 0x04, 0x00, // v128.load align=16 offset=0
	)
	body = append(body, benchFrontendFD(80)...) // v128.or
	body = append(body, benchFrontendV128Const(0x5051525354555657, 0x58595a5b5c5d5e5f)...)
	body = append(body,
		0x41, 0x07, // i32.const 7
	)
	body = append(body, benchFrontendFD(23, 0x03)...) // i8x16.replace_lane 3
	body = append(body, benchFrontendFD(78)...)       // v128.and
	body = append(body, 0x0b)

	return benchFrontendModuleBytes([]benchFrontendFuncDef{{
		results: []wasm.ValType{wasm.V128},
		body:    body,
	}}, true)
}

type benchFrontendFuncDef struct {
	params, results []wasm.ValType
	body            []byte
}

func benchFrontendModuleBytes(funcs []benchFrontendFuncDef, memory bool) []byte {
	types := make([][]byte, 0, len(funcs))
	funcSec := make([][]byte, 0, len(funcs))
	codes := make([][]byte, 0, len(funcs))
	for i, fn := range funcs {
		types = append(types, wasmtest.FuncType(fn.params, fn.results))
		funcSec = append(funcSec, wasmtest.ULEB(uint32(i)))
		codes = append(codes, append(wasmtest.ULEB(uint32(len(fn.body))), fn.body...))
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcSec...)),
	}
	if memory {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	return wasmtest.Module(sections...)
}

func benchFrontendFD(sub uint32, imm ...byte) []byte {
	out := []byte{0xfd}
	out = append(out, wasmtest.ULEB(sub)...)
	out = append(out, imm...)
	return out
}

func benchFrontendV128Const(lo, hi uint64) []byte {
	out := []byte{0xfd, 0x0c}
	for i := 0; i < 8; i++ {
		out = append(out, byte(lo>>(8*i)))
	}
	for i := 0; i < 8; i++ {
		out = append(out, byte(hi>>(8*i)))
	}
	return out
}
