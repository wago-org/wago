//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var callScratchBenchSink any

func BenchmarkCompileDirectTailCallScratch(b *testing.B) {
	m := modFuncs(b,
		funcDef{
			params:  []wasm.ValType{wasm.I32, wasm.I64, wasm.F64},
			results: []wasm.ValType{wasm.I64},
			body:    []byte{0x00, 0x20, 0x00, 0x1a, 0x20, 0x01, 0x20, 0x02, 0x1a, 0x12, 0x01, 0x0b},
		},
		funcDef{
			params:  []wasm.ValType{wasm.I64},
			results: []wasm.ValType{wasm.I64},
			body:    []byte{0x00, 0x20, 0x00, 0x0b},
		},
	)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		callScratchBenchSink = cm
	}
}

func BenchmarkCompileWideTailCallScratch(b *testing.B) {
	params := []wasm.ValType{wasm.I64}
	body := []byte{0x00}
	for range 80 {
		body = append(body, 0x42, 0x00) // i64.const 0 below the tail arguments
	}
	body = append(body, 0x20, 0x00)       // local.get 0
	body = append(body, 0x12, 0x08, 0x0b) // return_call 8; end
	fns := make([]funcDef, 9)
	for i := range fns[:8] {
		fns[i] = funcDef{params: params, results: []wasm.ValType{wasm.I64}, body: body}
	}
	fns[8] = funcDef{
		params:  params,
		results: []wasm.ValType{wasm.I64},
		body:    []byte{0x00, 0x20, 0x00, 0x0b},
	}
	m := modFuncs(b, fns...)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		cm, err := CompileModule(m)
		if err != nil {
			b.Fatal(err)
		}
		callScratchBenchSink = cm
	}
}
