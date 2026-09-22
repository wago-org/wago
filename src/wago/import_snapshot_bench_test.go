package wago

import (
	"fmt"
	"testing"
)

// Generic HostCall signatures match the raw WASI registration path. No command
// state is shared here; these callbacks do not execute or capture an instance.
func BenchmarkImportSnapshot(b *testing.B) {
	for _, count := range []int{1, 46} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			im := NewImports()
			for i := 0; i < count; i++ {
				im.HostFunc("wasi_snapshot_preview1", fmt.Sprintf("diagnostic_%02d", i), CallerHostCallFunc(func(Caller, HostCall) {})).Params(ValI32, ValI32).Results(ValI32)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bindings, err := im.snapshot()
				if err != nil || len(bindings) != count {
					b.Fatalf("snapshot: count=%d, error=%v", len(bindings), err)
				}
			}
		})
	}
}
