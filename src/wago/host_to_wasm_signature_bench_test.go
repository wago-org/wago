//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"
)

// BenchmarkHostToWasmSignatureMatrix measures public host-to-Wasm entry without
// hiding multi-value signatures behind the single-result specialized API.
func BenchmarkHostToWasmSignatureMatrix(b *testing.B) {
	for _, shape := range [][2]int{
		{0, 0},
		{1, 0},
		{1, 1},
		{2, 1},
		{2, 2},
		{4, 1},
		{4, 4},
		{8, 1},
		{8, 8},
		{16, 16},
		{24, 24},
		{32, 32},
		{48, 48},
		{64, 64},
	} {
		params, results := shape[0], shape[1]
		b.Run(fmt.Sprintf("i32x%d-i32x%d", params, results), func(b *testing.B) {
			compiled := benchMustCompile(b, hostToWasmI32SignatureModule(params, results))
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			prepared, err := in.PrepareFunction("f")
			if err != nil {
				b.Fatal(err)
			}
			args := make([]uint64, params)
			for i := range args {
				args[i] = I32(int32(i + 1))
			}
			check := func(got []uint64, err error) {
				b.Helper()
				if err != nil || len(got) != results {
					b.Fatalf("result = %v, %v", got, err)
				}
				for i := range got {
					if got[i] != uint64(i+1) {
						b.Fatalf("result[%d] = %d, want %d", i, got[i], i+1)
					}
				}
			}
			check(in.Invoke("f", args...))
			check(prepared.Invoke(args...))

			b.Run("invoke", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchResultSink, err = in.Invoke("f", args...)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("prepared", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchResultSink, err = prepared.Invoke(args...)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
