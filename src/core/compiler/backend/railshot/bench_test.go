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

func BenchmarkRailshotCompileSIMDWrapperCalls(b *testing.B) {
	m := benchSIMDWrapperCallModule(b)
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileSIMDControl(b *testing.B) {
	m := benchSIMDControlModule(b)
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileSIMDLoopParams(b *testing.B) {
	m := benchSIMDLoopParamModule(b)
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileSIMDMixedResults(b *testing.B) {
	m := benchSIMDMixedResultsModule(b)
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileSIMDWrapperCallsWithBelowStack(b *testing.B) {
	m := benchSIMDWrapperCallBelowStackModule(b)
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileBrTable(b *testing.B) {
	m := benchBrTableModule(b)
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileBrTableUnique(b *testing.B) {
	m := benchDecodeValidateModule(b, benchBrTableModuleBytesFrom([]uint32{0, 1, 2, 3, 4, 5, 6, 7}, 0))
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileBrTableDuplicate(b *testing.B) {
	m := benchDecodeValidateModule(b, benchBrTableModuleBytesFrom([]uint32{0, 0, 0, 0, 0, 0, 0, 0}, 0))
	benchmarkCompileModule(b, m)
}

func BenchmarkRailshotCompileBrTableMixed(b *testing.B) {
	m := benchDecodeValidateModule(b, benchBrTableModuleBytesFrom([]uint32{0, 1, 1, 2, 0, 2, 1, 0}, 2))
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

func benchSIMDWrapperCallModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	return benchDecodeValidateModule(tb, benchSIMDWrapperCallModuleBytes())
}

func benchSIMDControlModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	return benchDecodeValidateModule(tb, benchSIMDControlModuleBytes())
}

func benchSIMDLoopParamModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	return benchDecodeValidateModule(tb, benchSIMDLoopParamModuleBytes())
}

func benchSIMDMixedResultsModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	return benchDecodeValidateModule(tb, benchSIMDMixedResultsModuleBytes())
}

func benchSIMDWrapperCallBelowStackModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	return benchDecodeValidateModule(tb, benchSIMDWrapperCallBelowStackModuleBytes())
}

func benchBrTableModule(tb testing.TB) *wasm.Module {
	tb.Helper()
	return benchDecodeValidateModule(tb, benchBrTableModuleBytes())
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

func benchSIMDWrapperCallModuleBytes() []byte {
	callee := append([]byte{0x00}, // local decl count
		0x20, 0x00, // local.get 0
		0x0b, // end
	)
	caller := []byte{0x00} // local decl count
	caller = append(caller, benchV128Const(0x0001020304050607, 0x08090a0b0c0d0e0f)...)
	for i := 0; i < 12; i++ {
		caller = append(caller,
			0x10, 0x00, // call 0: v128 -> v128, wrapper ABI path
		)
	}
	caller = append(caller, 0x0b) // end
	return benchModuleBytes([]benchFuncDef{
		{
			params:  []wasm.ValType{wasm.V128},
			results: []wasm.ValType{wasm.V128},
			body:    callee,
		},
		{
			results: []wasm.ValType{wasm.V128},
			body:    caller,
		},
	}, true)
}

func benchSIMDControlModuleBytes() []byte {
	body := []byte{0x00, 0x41, 0x07} // no locals; i32 value below every v128 branch result
	for i := 0; i < 24; i++ {
		body = append(body,
			0x02, 0x7b, // block (result v128)
			0x02, 0x7b, // nested block (result v128)
			0x20, 0x00, // local.get 0: if condition
			0x04, 0x7b, // if (result v128)
		)
		body = append(body, benchV128Const(uint64(i), 0x1011121314151617)...)
		body = append(body, 0x05) // else
		body = append(body, benchV128Const(0x2021222324252627, uint64(i))...)
		body = append(body,
			0x0b,       // end if; leaves v128 above the preserved i32
			0x0c, 0x00, // br 0 carrying v128 to the nested block label
			0x0b, // end nested block
			0x0b, // end outer block
			0x1a, // drop v128; keep the below-stack i32 for the next construct
		)
	}
	body = append(body, 0x0b) // return the preserved i32
	return benchModuleBytes([]benchFuncDef{{
		params:  []wasm.ValType{wasm.I32},
		results: []wasm.ValType{wasm.I32},
		body:    body,
	}}, false)
}

func benchSIMDLoopParamModuleBytes() []byte {
	body := []byte{0x00, 0x41, 0x2a} // no locals; i32 below repeated loop params/results
	for i := 0; i < 16; i++ {
		body = append(body, benchV128Const(0x0001020304050607, uint64(i))...) // initial loop param
		body = append(body, 0x03, 0x01)                                      // loop type 1: (v128) -> (v128)
		body = append(body, 0x1a)                                            // drop incoming loop param; backedge/result use the next value
		body = append(body, benchV128Const(uint64(i+1), 0x08090a0b0c0d0e0f)...)
		body = append(body,
			0x20, 0x00, // local.get 0: br_if condition
			0x0d, 0x00, // br_if 0 carrying the v128 loop param on the backedge
			0x0b, // end loop; not-taken path leaves the carried v128 as result
			0x1a, // drop loop result; keep the below-stack i32 for the next loop
		)
	}
	body = append(body, 0x0b) // return the preserved i32
	return benchModuleBytesWithExtraTypes([]benchFuncDef{{
		params:  []wasm.ValType{wasm.I32},
		results: []wasm.ValType{wasm.I32},
		body:    body,
	}}, false, wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
}

func benchSIMDMixedResultsModuleBytes() []byte {
	f0 := append([]byte{0x00, 0x41, 0x7b}, benchV128Const(0x0001020304050607, 0x08090a0b0c0d0e0f)...)
	f0 = append(f0, 0x42)
	f0 = append(f0, wasmtest.SLEB64(0x1122334455667788)...)
	f0 = append(f0, 0x0b)

	f1 := []byte{0x00}
	f1 = append(f1, benchV128Const(0x1011121314151617, 0x18191a1b1c1d1e1f)...)
	f1 = append(f1, benchV128Const(0x2021222324252627, 0x28292a2b2c2d2e2f)...)
	f1 = append(f1, 0x0f, 0x0b) // return; end

	f2 := []byte{0x00, 0x02, 0x02} // block type 2: same () -> (i32, v128, f64, v128) as this function
	f2 = append(f2, 0x41, 0x2a)
	f2 = append(f2, benchV128Const(0x3031323334353637, 0x38393a3b3c3d3e3f)...)
	f2 = append(f2, 0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x3f) // f64.const 1.5
	f2 = append(f2, benchV128Const(0x4041424344454647, 0x48494a4b4c4d4e4f)...)
	f2 = append(f2, 0x0c, 0x00, 0x0b, 0x0b) // br 0; end block; end function

	return benchModuleBytes([]benchFuncDef{
		{results: []wasm.ValType{wasm.I32, wasm.V128, wasm.I64}, body: f0},
		{results: []wasm.ValType{wasm.V128, wasm.V128}, body: f1},
		{results: []wasm.ValType{wasm.I32, wasm.V128, wasm.F64, wasm.V128}, body: f2},
	}, false)
}

func benchSIMDWrapperCallBelowStackModuleBytes() []byte {
	callee := append([]byte{0x00},
		0x20, 0x00, // local.get 0
		0x0b, // end
	)
	caller := []byte{0x00, 0x41, 0x00} // local decl count; i32 accumulator below each call's scalar+v128 args
	for i := 0; i < 12; i++ {
		caller = append(caller, 0x41, byte(i+1)) // scalar value below the v128 call argument
		caller = append(caller, benchV128Const(uint64(i), 0x08090a0b0c0d0e0f)...)
		caller = append(caller,
			0x10, 0x00, // call 0: v128 -> v128, wrapper ABI path; leaves accumulator+scalar below result
		)
		caller = append(caller, benchFD(22, 0x00)...) // i8x16.extract_lane_u 0; uses call result
		caller = append(caller,
			0x6a, // scalar + extracted lane
			0x6a, // accumulator += sum
		)
	}
	caller = append(caller, 0x0b)
	return benchModuleBytes([]benchFuncDef{
		{params: []wasm.ValType{wasm.V128}, results: []wasm.ValType{wasm.V128}, body: callee},
		{results: []wasm.ValType{wasm.I32}, body: caller},
	}, true)
}

func benchBrTableModuleBytes() []byte {
	return benchBrTableModuleBytesFrom([]uint32{0, 1, 2}, 0)
}

func benchBrTableModuleBytesFrom(labels []uint32, def uint32) []byte {
	funcs := make([]benchFuncDef, 8)
	nest := 1
	for _, lbl := range labels {
		if int(lbl)+1 > nest {
			nest = int(lbl) + 1
		}
	}
	if int(def)+1 > nest {
		nest = int(def) + 1
	}
	for i := range funcs {
		body := []byte{0x00} // local decl count
		for j := 0; j < 8; j++ {
			for k := 0; k < nest; k++ {
				body = append(body, 0x02, 0x7f) // block (result i32)
			}
			body = append(body,
				0x41, 0x0a, // i32.const 10: branch value
				0x20, 0x00, // local.get 0: br_table selector
				0x0e, // br_table
			)
			body = append(body, wasmtest.ULEB(uint32(len(labels)))...)
			for _, lbl := range labels {
				body = append(body, wasmtest.ULEB(lbl)...)
			}
			body = append(body, wasmtest.ULEB(def)...)
			for k := 0; k < nest; k++ {
				body = append(body, 0x0b) // end block
			}
			if j != 7 {
				body = append(body, 0x1a) // drop block result before the next table
			}
		}
		body = append(body, 0x0b) // end function
		funcs[i] = benchFuncDef{
			params:  []wasm.ValType{wasm.I32},
			results: []wasm.ValType{wasm.I32},
			body:    body,
		}
	}
	return benchModuleBytes(funcs, false)
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
	return benchModuleBytesWithExtraTypes(funcs, memory)
}

func benchModuleBytesWithExtraTypes(funcs []benchFuncDef, memory bool, extraTypes ...[]byte) []byte {
	types := make([][]byte, 0, len(funcs)+len(extraTypes))
	funcSec := make([][]byte, 0, len(funcs))
	codes := make([][]byte, 0, len(funcs))
	for i, fn := range funcs {
		types = append(types, wasmtest.FuncType(fn.params, fn.results))
		funcSec = append(funcSec, wasmtest.ULEB(uint32(i)))
		codes = append(codes, append(wasmtest.ULEB(uint32(len(fn.body))), fn.body...))
	}
	types = append(types, extraTypes...)
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
