package wagobench

import (
	"math"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Floating predicates select dependent integer updates. Both float inputs are
// parameters so the backend cannot fold the predicate at compile time.
func BenchmarkScalarFloatSelect(b *testing.B) {
	benchmarkScalarFloatPredicate(b, false)
}

func BenchmarkScalarFloatBranch(b *testing.B) {
	benchmarkScalarFloatPredicate(b, true)
}

func benchmarkScalarFloatPredicate(b *testing.B, branch bool) {
	const iterations, unroll = 4096, 16
	for _, wide := range []bool{false, true} {
		typ, first, width := wasm.F32, byte(0x5b), "f32"
		x, y := uint64(math.Float32bits(1)), uint64(math.Float32bits(2))
		if wide {
			typ, first, width = wasm.F64, 0x61, "f64"
			x, y = math.Float64bits(1), math.Float64bits(2)
		}
		for k, name := range []string{"eq", "ne", "lt", "gt", "le", "ge"} {
			b.Run(width+"/"+name, func(b *testing.B) {
				body := []byte{0x03, 0x40}
				for range unroll {
					if branch {
						body = append(body, 0x20, 1, 0x20, 2, first+byte(k), 0x04, 0x7e, 0x20, 3, 0x42, 1, 0x7c, 0x05, 0x20, 3, 0x42, 2, 0x7c, 0x0b, 0x21, 3)
					} else {
						body = append(body, 0x20, 3, 0x42, 1, 0x7c, 0x20, 3, 0x42, 2, 0x7c, 0x20, 1, 0x20, 2, first+byte(k), 0x1b, 0x21, 3)
					}
				}
				body = append(body, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 3, 0x0b)
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, typ, wasm.I64}, []wasm.ValType{wasm.I64}))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
				)
				c, err := wago.Compile(nil, module)
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				in, err := wago.Instantiate(c, wago.InstantiateOptions{})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					b.Fatal(err)
				}
				step := uint64(2)
				if k == 1 || k == 2 || k == 4 {
					step = 1
				}
				want := uint64(iterations*unroll) * step
				got, err := fn.Invoke(iterations, x, y, 0)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("run=%v, %v; want %d", got, err, want)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					got, err = fn.Invoke(iterations, x, y, 0)
					if err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				metric := "ns/select"
				if branch {
					metric = "ns/branch"
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*iterations*unroll), metric)
				if len(got) != 1 || got[0] != want {
					b.Fatalf("run=%v; want %d", got, want)
				}
			})
		}
	}
}
