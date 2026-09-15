package filelock

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkFileLockAcquire(b *testing.B) {
	for _, retries := range []int{0, 1, 10} {
		b.Run(fmt.Sprint(retries), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "lock")
			if retries > 0 {
				owner, err := Acquire(context.Background(), path)
				if err != nil {
					b.Fatal(err)
				}
				defer owner.Close()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ctx := context.Background()
				cancel := func() {}
				if retries > 0 {
					b.StopTimer()
					ctx, cancel = context.WithTimeout(ctx, time.Duration(retries)*pollInterval+pollInterval/2)
					b.StartTimer()
				}
				lock, err := Acquire(ctx, path)
				if lock != nil {
					if err := lock.Close(); err != nil {
						b.Fatal(err)
					}
				}
				if retries == 0 && err != nil || retries > 0 && !errors.Is(err, context.DeadlineExceeded) {
					b.Fatalf("lock result: %v", err)
				}
				b.StopTimer()
				cancel()
				b.StartTimer()
			}
		})
	}
}
