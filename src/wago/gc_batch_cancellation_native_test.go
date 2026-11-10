//go:build (linux || (darwin && arm64)) && (amd64 || arm64) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGCReducedBatchHonorsDeadline(t *testing.T) {
	for _, tc := range []struct {
		name   string
		module []byte
	}{
		{"struct", gcStructReductionBytes(false, false)},
		{"cast", gcStructReductionBytes(true, false)},
		{"array", gcArrayReductionBytes(false)},
		{"fill", gcArrayFillReductionBytes(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), tc.module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			const count = 2000000
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err = in.InvokeContext(ctx, "run", count)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline error=%v", err)
			}
			if got := in.gc.Stats().Allocations; got >= count {
				t.Fatalf("deadline returned after %v and all %d allocations; want partial work", time.Since(start), got)
			}
			if got, err := in.Invoke("run", 7); err != nil || len(got) != 1 || got[0] != 21 {
				t.Fatalf("recovery=%v,%v", got, err)
			}
		})
	}
}
