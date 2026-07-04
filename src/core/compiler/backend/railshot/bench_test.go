//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// Codegen profiling quickstart:
//
// 	go test ./src/core/compiler/backend/railshot -bench=. -benchmem
// 	go test ./src/core/compiler/backend/railshot -bench=BenchmarkRailshotCompile -benchmem -cpuprofile cpu.out -memprofile mem.out
// 	go tool pprof -top mem.out
// 	go tool pprof -top cpu.out

var benchCompiledSink *amd64.CompiledModule

func BenchmarkRailshotCompileSmallScalar(b *testing.B) {
	m := benchSmallScalarModule(b)
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileMediumControl(b *testing.B) {
	m := benchMediumControlModule(b)
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileSIMDHeavy(b *testing.B) {
	m := benchSIMDHeavyModule(b)
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotEndToEndSIMDHeavy(b *testing.B) {
	data := benchSIMDHeavyModuleBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, err := frontend.DecodeValidate(data)
		if err != nil {
			b.Fatal(err)
		}
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		benchCompiledSink = cm
	}
}

func benchmarkCompileModule(b *testing.B, m *wasm.Module) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		benchCompiledSink = cm
	}
}

func benchSmallScalarModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	return benchDecodeValidateModule(tb, benchSmallScalarModuleBytes())
}

func benchMediumControlModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	return benchDecodeValidateModule(tb, benchMediumControlModuleBytes())
}

func benchSIMDHeavyModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	return benchDecodeValidateModule(tb, benchSIMDHeavyModuleBytes())
}

func benchDecodeValidateModule(tb testing.TB, data []byte) *wasm.Module {
	tb.Helper()
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatalf("decode benchmark module: %v", err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatalf("validate benchmark module: %v", err)
	}
	if err := frontend.RejectUnsupported(m); err != nil {
		tb.Fatalf("support benchmark module: %v", err)
	}
	return m
}

func benchSmallScalarModuleBytes() []byte {
	body := append([]byte{0x00}, // local decl count
		0x20, 0x00, // local.get 0 (address)
		0x20, 0x01, // local.get 1 (value)
		0x36, 0x02, 0x00, // i32.store align=4 offset=0
		0x20, 0x00, // local.get 0
		0x28, 0x02, 0x00, // i32.load align=4 offset=0
		0x20, 0x01, // local.get 1
		0x6a, // i32.add
		0x0b, // end
	)
	return benchModuleBytes([]benchFuncDef{{
		params:  []wasm.ValType{wasm.I32, wasm.I32},
		results: []wasm.ValType{wasm.I32},
		body:    body,
	}}, true)
}

func benchMediumControlModuleBytes() []byte {
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
	return benchModuleBytes([]benchFuncDef{{
		params:  []wasm.ValType{wasm.I32},
		results: []wasm.ValType{wasm.I32},
		body:    body,
	}}, false)
}

func benchSIMDHeavyModuleBytes() []byte {
	body := []byte{0x00} // local decl count
	body = append(body,
		0x41, 0x20, // i32.const 32
	)
	body = append(body, benchV128Const(0x1011121314151617, 0x18191a1b1c1d1e1f)...)
	body = append(body,
		0xfd, 0x0b, 0x04, 0x00, // v128.store align=16 offset=0
	)
	body = append(body, benchV128Const(0x0001020304050607, 0x08090a0b0c0d0e0f)...)
	body = append(body, benchV128Const(0x2021222324252627, 0x28292a2b2c2d2e2f)...)
	body = append(body, benchFD(174)...) // i32x4.add
	body = append(body, benchV128Const(0x3031323334353637, 0x38393a3b3c3d3e3f)...)
	body = append(body, benchFD(55)...) // i32x4.eq
	body = append(body, benchV128Const(0x4041424344454647, 0x48494a4b4c4d4e4f)...)
	body = append(body, benchFD(81)...) // v128.xor
	body = append(body,
		0x41, 0x00, // i32.const 0
		0xfd, 0x00, 0x04, 0x00, // v128.load align=16 offset=0
	)
	body = append(body, benchFD(80)...) // v128.or
	body = append(body, benchV128Const(0x5051525354555657, 0x58595a5b5c5d5e5f)...)
	body = append(body,
		0x41, 0x07, // i32.const 7
	)
	body = append(body, benchFD(23, 0x03)...) // i8x16.replace_lane 3
	body = append(body, benchFD(78)...)       // v128.and
	body = append(body, 0x0b)                 // end

	return benchModuleBytes([]benchFuncDef{{
		results: []wasm.ValType{wasm.V128},
		body:    body,
	}}, true)
}

type benchFuncDef struct {
	params, results []wasm.ValType
	body            []byte // local decls + instruction stream including trailing end
}

func benchModuleBytes(funcs []benchFuncDef, memory bool) []byte {
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
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01}))) // min 1 page
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	return wasmtest.Module(sections...)
}

func benchFD(sub uint32, imm ...byte) []byte {
	out := []byte{0xfd}
	out = append(out, wasmtest.ULEB(sub)...)
	out = append(out, imm...)
	return out
}

func benchV128Const(lo, hi uint64) []byte {
	out := []byte{0xfd, 0x0c}
	for i := 0; i < 8; i++ {
		out = append(out, byte(lo>>(8*i)))
	}
	for i := 0; i < 8; i++ {
		out = append(out, byte(hi>>(8*i)))
	}
	return out
}
