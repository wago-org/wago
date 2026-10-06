//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func BenchmarkWorkerScratchReuse(b *testing.B) {
	c := workerResetLoad(b, 32, false, shared.AMD64ModernBaseline)
	for _, target := range []int{0, 1, 2, 4, 5, 7, 10} {
		for _, reuse := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/reuse=%t", workerResetNames[target], reuse), func(b *testing.B) {
				var sc *scratch
				// Both cases start with a compiled module context. Reused workers retain
				// scratch from this bounded heterogeneous warm-up; fresh workers do not.
				if reuse {
					sc = c.worker()
					defer workerResetClose(sc)
					for i := range workerResetNames {
						workerResetCompile(b, c, sc, i)
					}
				}
				b.SetBytes(int64(len(c.m.Code[target].BodyBytes)))
				b.ReportAllocs()
				var retained shared.WorkerScratchStats
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if !reuse {
						sc = c.worker()
					}
					_, err := c.compile(sc, target, false)
					if err != nil {
						b.Fatal(err)
					}
					if !reuse {
						workerResetClose(sc)
					}
				}
				b.StopTimer()
				if !reuse {
					sc = c.worker()
					if _, err := c.compile(sc, target, false); err != nil {
						b.Fatal(err)
					}
				}
				retained = workerScratchStats(sc)
				if !reuse {
					workerResetClose(sc)
				}
				b.ReportMetric(float64(retained.ScalarRetained+retained.NodeRetained+retained.ControlRetained), "scratch-B")
			})
		}
	}
}
