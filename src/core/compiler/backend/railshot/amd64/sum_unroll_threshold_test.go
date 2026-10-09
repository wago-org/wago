//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"testing"

	rt "github.com/wago-org/wago/src/core/runtime"
)

// Resolve the gap between the short-loop and cache-resident comparisons. This
// uses the same kernel, data pattern, seed, and native-call timer as the matrix.
func BenchmarkSumUnrollThreshold(b *testing.B) {
	selectSumUnroll(b)
	m := sumUnrollModule(b, 0)
	mem, err := rt.NewJobMemory(65536)
	if err != nil {
		b.Fatal(err)
	}
	defer mem.Close()
	data := mem.CurrentBytes()
	for i := range data {
		data[i] = byte(i*37 + 11)
	}
	native := sumUnrollNative(b, m, CompileOptions{})
	for _, addr := range []uint32{0, 1} {
		for _, count := range []uint32{64, 128, 256, 1024, 2048, 4096} {
			b.Run(fmt.Sprintf("addr%d/n%d", addr, count), func(b *testing.B) {
				want, trap := sumOracle(data, addr, count, 7, 0)
				if trap {
					b.Fatal("invalid benchmark input")
				}
				got, err := native.call(mem, addr, count, 7, 0)
				if err != nil || got != want {
					b.Fatalf("preflight: %x want %x: %v", got, want, err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := native.eng.Call(native.entry, native.args, mem.LinearMemory(), native.trap, native.results); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				if binary.LittleEndian.Uint64(native.results) != want[0] {
					b.Fatal("incorrect sum")
				}
			})
		}
	}
}
