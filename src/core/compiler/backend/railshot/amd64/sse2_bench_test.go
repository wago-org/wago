//go:build linux && amd64 && !tinygo

package amd64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"hash/crc32"
	"math"
	"testing"
)

func BenchmarkAMD64Tiers(b *testing.B) {
	type workload struct {
		name            string
		params, results []wasm.ValType
		op              []byte
		memory          bool
	}
	unary := func(name string, p, r wasm.ValType, op byte) workload {
		return workload{name, []wasm.ValType{p}, []wasm.ValType{r}, []byte{op}, false}
	}
	binaryOp := func(name string, p wasm.ValType, op byte) workload {
		return workload{name, []wasm.ValType{p, p}, []wasm.ValType{p}, []byte{op}, false}
	}
	vector := func(name string, n int, result wasm.ValType, op uint32) workload {
		params := make([]wasm.ValType, n)
		for i := range params {
			params[i] = wasm.V128
		}
		return workload{name, params, []wasm.ValType{result}, simdOp(op), false}
	}
	cases := []workload{
		binaryOp("f32_add", wasm.F32, 0x92), binaryOp("f64_add", wasm.F64, 0xa0),
		unary("f32_nearest", wasm.F32, wasm.F32, 0x90), unary("f64_nearest", wasm.F64, wasm.F64, 0x9e),
		unary("f64_convert_i64", wasm.I64, wasm.F64, 0xb9),
		unary("i64_clz", wasm.I64, wasm.I64, 0x79), unary("i64_ctz", wasm.I64, wasm.I64, 0x7a), unary("i64_popcnt", wasm.I64, wasm.I64, 0x7b),
		{"memory_copy", []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil, []byte{0xfc, 10, 0, 0}, true},
		vector("swizzle", 2, wasm.V128, 14), vector("i32_mul", 2, wasm.V128, 181), vector("i64_gt", 2, wasm.V128, 217), vector("i32_min", 2, wasm.V128, 182),
		vector("f64x2_nearest", 1, wasm.V128, 148), vector("relaxed_q15", 2, wasm.V128, 273),
	}
	lane := vector("lane_extract", 1, wasm.I32, 27)
	lane.op = append(lane.op, 3)
	cases = append(cases, lane)
	for _, tc := range cases {
		body := []byte{0}
		for i := range tc.params {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, tc.op...)
		body = append(body, 0x0b)
		sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(tc.params, tc.results))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0)))}
		if tc.memory {
			sections = append(sections, wasmtest.Section(5, []byte{1, 0, 1}))
		}
		sections = append(sections, wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
		m, err := wasm.DecodeModule(wasmtest.Module(sections...))
		if err != nil {
			b.Fatal(err)
		}
		for _, modern := range []bool{false, true} {
			opts := CompileOptions{AMD64FeaturesSet: true, DeferCodeMapping: true, BitCountFeatures: 7}
			if modern {
				opts.AMD64Features = shared.AMD64KnownFeatures
			}
			b.Run(fmt.Sprintf("%s/modern=%v/compile", tc.name, modern), func(b *testing.B) {
				b.ReportAllocs()
				size := 0
				for i := 0; i < b.N; i++ {
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						b.Fatal(err)
					}
					size = len(cm.Code)
					if cm.CodeImage != nil {
						cm.CodeImage.Close()
					}
				}
				b.ReportMetric(float64(size), "code-bytes")
			})
			b.Run(fmt.Sprintf("%s/modern=%v/execute", tc.name, modern), func(b *testing.B) {
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					b.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				eng, err := runtime.NewEngine()
				if err != nil {
					b.Fatal(err)
				}
				defer eng.Close()
				jm, err := runtime.NewJobMemory(65536)
				if err != nil {
					b.Fatal(err)
				}
				defer jm.Close()
				ar, err := runtime.NewArena(4096)
				if err != nil {
					b.Fatal(err)
				}
				defer ar.Close()
				mem, entry, err := runtime.MapCode(cm.Code)
				if err != nil {
					b.Fatal(err)
				}
				defer runtime.Unmap(mem)
				args, out, trap := ar.Alloc(256), ar.Alloc(256), ar.Alloc(runtime.TrapBufferBytes)
				for i := 0; i < 256; i += 8 {
					binary.LittleEndian.PutUint64(args[i:], math.Float64bits(3.75))
				}
				if len(tc.params) > 0 && tc.params[0] == wasm.F32 {
					binary.LittleEndian.PutUint32(args, math.Float32bits(3.75))
					binary.LittleEndian.PutUint32(args[8:], math.Float32bits(1.25))
				}
				if tc.memory {
					binary.LittleEndian.PutUint64(args, 0)
					binary.LittleEndian.PutUint64(args[8:], 8192)
					binary.LittleEndian.PutUint64(args[16:], 1024)
				}
				lin := jm.LinearMemory()
				address := entry + uintptr(cm.Entry[0])
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := eng.Call(address, args, lin, trap, out); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(crc32.ChecksumIEEE(cm.Code)), "code-crc32")
				b.ReportMetric(float64(len(cm.Code)), "code-bytes")
			})
		}
	}
}
