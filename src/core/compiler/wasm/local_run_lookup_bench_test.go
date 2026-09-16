package wasm_test

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func localRunLookupModule(tb testing.TB, runs, reads int) *wasm.Module {
	tb.Helper()
	body := wasmtest.ULEB(uint32(runs))
	for i := 0; i < runs; i++ {
		body = append(body, 1, 0x7f)
	}
	for i := 0; i < reads; i++ {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(runs-1))...)
		body = append(body, 0x1a)
	}
	body = append(body, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		tb.Fatal(err)
	}
	return m
}

func BenchmarkValidateLocalRunLookup(b *testing.B) {
	for _, runs := range []int{1, 8, 64, 1024} {
		for _, reads := range []int{1, 256} {
			b.Run(fmt.Sprintf("runs=%d/reads=%d", runs, reads), func(b *testing.B) {
				m := localRunLookupModule(b, runs, reads)
				if err := wasm.ValidateModule(m); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := wasm.ValidateModule(m); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestValidateLocalRunBenchmarkFixtures(t *testing.T) {
	for _, runs := range []int{1, 8, 64, 1024} {
		for _, reads := range []int{1, 256} {
			if err := wasm.ValidateModule(localRunLookupModule(t, runs, reads)); err != nil {
				t.Fatal(err)
			}
		}
	}
}
