//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package runtime

import (
	"fmt"
	"sync/atomic"
	"testing"
)

func BenchmarkInterruptSignalNegotiation(b *testing.B) {
	executableCodeMu.Lock()
	defer executableCodeMu.Unlock()
	if atomic.LoadUint32(&executableCodeRangeLimit) != 0 {
		b.Fatal("benchmark requires an idle code registry")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := installInterruptHandler(); err != nil {
			b.Fatal(err)
		}
		restoreInterruptHandler()
	}
}

func BenchmarkExecutableRegistryScaling(b *testing.B) {
	for _, count := range []int{0, 16, 256, 2048} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			mappings := make([][]byte, count)
			for i := range mappings {
				code, _, err := MapCode([]byte{0})
				if err != nil {
					b.Fatal(err)
				}
				mappings[i] = code
			}
			defer func() {
				for _, code := range mappings {
					if err := Unmap(code); err != nil {
						b.Error(err)
					}
				}
			}()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				code, _, err := MapCode([]byte{0})
				if err != nil {
					b.Fatal(err)
				}
				if err := Unmap(code); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
