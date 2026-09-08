package wago

import (
	"context"
	"testing"
)

func TestCancellationWatchInertContexts(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{{"nil", nil}, {"background", context.Background()}, {"todo", context.TODO()}, {"without-cancel", context.WithoutCancel(context.Background())}} {
		t.Run(tc.name, func(t *testing.T) {
			var in Instance
			trap := []byte{0, 0, 0, 0}
			allocs := testing.AllocsPerRun(100, func() {
				stop, err := in.startCancellationWatch(tc.ctx, trap)
				if err != nil {
					t.Fatal(err)
				}
				stop()
				stop()
			})
			if allocs != 0 {
				t.Fatalf("inert watcher allocates: %g allocs/op", allocs)
			}
		})
	}
}

func BenchmarkCancellationWatch(b *testing.B) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{{"background", context.Background()}, {"todo", context.TODO()}, {"without-cancel", context.WithoutCancel(context.Background())}} {
		b.Run(tc.name, func(b *testing.B) {
			var in Instance
			trap := make([]byte, 4)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				stop, err := in.startCancellationWatch(tc.ctx, trap)
				if err != nil {
					b.Fatal(err)
				}
				stop()
			}
		})
	}
}
