package wagobench

import (
	"math"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep the operand values dynamic and amortize the public invocation over a
// dependent chain of 65536 scalar operations. This isolates instruction latency;
// application-level throughput is covered separately by the corpus benchmarks.
func BenchmarkScalarFloat(b *testing.B) {
	const iterations, unroll = 4096, 16
	for _, wide := range []bool{false, true} {
		typ, first, width := wasm.F32, byte(0x96), "f32"
		a, c := uint64(math.Float32bits(1)), uint64(math.Float32bits(-2))
		if wide {
			typ, first, width = wasm.F64, 0xa4, "f64"
			a, c = math.Float64bits(1), math.Float64bits(-2)
		}
		for k, name := range []string{"min", "max", "copysign", "abs", "neg"} {
			b.Run(width+"/"+name, func(b *testing.B) {
				body := []byte{0x03, 0x40} // loop
				for range unroll {
					if k < 3 {
						body = append(body, 0x20, 1, 0x20, 2, first+byte(k), 0x21, 1)
					} else {
						op := byte(0x8b) + byte(k-3)
						if wide {
							op = 0x99 + byte(k-3)
						}
						body = append(body, 0x20, 1, op, 0x21, 1)
					}
				}
				body = append(body, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 1, 0x0b)
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, typ}, []wasm.ValType{typ}))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
				)
				compiled, err := wago.Compile(nil, module)
				if err != nil {
					b.Fatal(err)
				}
				defer compiled.Close()
				in, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					b.Fatal(err)
				}
				want := a
				if k == 0 {
					want = c
				} else if k == 2 {
					if wide {
						want = a | 1<<63
					} else {
						want = a | 1<<31
					}
				}
				got, err := fn.Invoke(iterations, a, c)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("run = %x, %v; want %x", got, err, want)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					got, err = fn.Invoke(iterations, a, c)
					if err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*iterations*unroll), "ns/scalar-op")
				if len(got) != 1 || got[0] != want {
					b.Fatalf("run = %x; want %x", got, want)
				}
			})
		}
	}
}
