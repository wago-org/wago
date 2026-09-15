package wago

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestInstanceResultStorage(t *testing.T) {
	t.Logf("instance bytes=%d, inline result offset=%d", unsafe.Sizeof(Instance{}), unsafe.Offsetof(Instance{}.resultInline))
	for _, slots := range []int{0, 1, 2, 3, 17} {
		t.Run(fmt.Sprint(slots), func(t *testing.T) {
			types := make([]wasm.ValType, slots)
			body := make([]byte, 0, 2*slots+1)
			args := make([]uint64, slots)
			for i := range types {
				types[i] = wasm.I64
				body = append(body, 0x20, byte(i))
				args[i] = uint64(i) | 1<<63
			}
			body = append(body, 0x0b)
			c := MustCompile(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			))
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if len(in.resultVals) != slots || cap(in.resultVals) != slots {
				t.Fatalf("result len/cap=%d/%d, want %d", len(in.resultVals), cap(in.resultVals), slots)
			}
			if slots > 0 && (&in.resultVals[0] == &in.resultInline[0]) != (slots <= len(in.resultInline)) {
				t.Fatal("wrong inline/fallback result storage")
			}
			fn, err := in.PrepareFunction("f")
			if err != nil {
				t.Fatal(err)
			}
			var saved []uint64
			for _, call := range []func(...uint64) ([]uint64, error){func(a ...uint64) ([]uint64, error) { return in.Invoke("f", a...) }, fn.Invoke} {
				got, err := call(args...)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != slots {
					t.Fatalf("got %d slots, want %d", len(got), slots)
				}
				for i := range args {
					if got[i] != args[i] {
						t.Fatalf("slot %d: got %x, want %x", i, got[i], args[i])
					}
				}
				saved = got
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			// Results remain Go-owned: retaining a returned slice must not expose
			// unmapped native storage when the instance is closed.
			for i := range saved {
				if saved[i] != args[i] {
					t.Fatalf("retained slot %d changed after close", i)
				}
			}
			if _, err := fn.Invoke(args...); err == nil {
				t.Fatal("prepared call accepted closed instance")
			}
		})
	}
}

// Keep both layouts in one binary so compiler/linker placement is not a cause
// of their difference. Packed models tiny result objects sharing a cache line.
func BenchmarkInstanceResultLayout(b *testing.B) {
	for _, independent := range []bool{false, true} {
		for _, packed := range []bool{true, false} {
			b.Run(fmt.Sprintf("independent=%t/packed=%t", independent, packed), func(b *testing.B) {
				c, err := NewRuntimeConfig().WithIndependentInstanceExecution(independent).Compile(benchAddOneModule())
				if err != nil {
					b.Fatal(err)
				}
				workers := runtime.GOMAXPROCS(0)
				functions := make([]*PreparedFunction, workers)
				results := make([]uint64, workers)
				for i := range functions {
					in, err := Instantiate(c, InstantiateOptions{})
					if err != nil {
						b.Fatal(err)
					}
					defer in.Close()
					if packed {
						in.resultVals = results[i : i+1 : i+1]
					}
					functions[i], err = in.PrepareFunction("f")
					if err != nil {
						b.Fatal(err)
					}
					if got, err := functions[i].Invoke1(41); err != nil || len(got) != 1 || got[0] != 42 {
						b.Fatalf("warmup: %v, %v", got, err)
					}
				}
				var next atomic.Uint32
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					fn := functions[next.Add(1)-1]
					for pb.Next() {
						if _, err := fn.Invoke1(41); err != nil {
							b.Error(err)
							return
						}
					}
				})
				b.StopTimer()
				for _, fn := range functions {
					if fn.in.resultVals[0] != 42 {
						b.Fatal("result changed")
					}
				}
			})
		}
	}
}
