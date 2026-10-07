package wagobench

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

var execBatchAllocationSink []byte
var execBatchWork uint64

// Run the real timed loop in its own process so other tests cannot contribute
// allocations. A fixed batch tests accounting without a wall-clock threshold.
func TestExecBatchAllocationUnits(t *testing.T) {
	const child = "WAGO_TEST_EXEC_BATCH_ACCOUNTING"
	if os.Getenv(child) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExecBatchAllocationUnits$", "-test.benchtime=64x")
		cmd.Env = append(os.Environ(), child+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated accounting regression: %v\n%s", err, out)
		}
		return
	}
	for _, allocate := range []bool{false, true} {
		name := "no_allocations"
		if allocate {
			name = "64_bytes_per_call"
		}
		t.Run(name, func(t *testing.T) {
			const batch = 32
			var calls int
			result := testing.Benchmark(func(b *testing.B) {
				// Deliberate setup traffic must not appear in per-call metrics.
				execBatchAllocationSink = make([]byte, 1<<20)
				calls = 0
				benchmarkExecBatch(b, func() error {
					calls++
					if allocate {
						execBatchAllocationSink = make([]byte, 64)
						execBatchAllocationSink[0] = byte(calls)
					}
					return nil
				}, batch)
			})
			if calls != result.N*batch || result.Extra["calls/batch"] != batch {
				t.Fatalf("completed %d calls for N=%d, metrics=%v", calls, result.N, result.Extra)
			}
			wantAllocs, wantBytes := 0.0, 0.0
			if allocate {
				wantAllocs, wantBytes = 1, 64
				if execBatchAllocationSink[0] != byte(calls) {
					t.Fatal("final invocation did not complete")
				}
			}
			allocs, ok := result.Extra["allocs/op"]
			if !ok {
				allocs = float64(result.MemAllocs) / float64(result.N)
			}
			bytes, ok := result.Extra["B/op"]
			if !ok {
				bytes = float64(result.MemBytes) / float64(result.N)
			}
			// MemStats is process-wide. Permit small background traffic, but
			// reject batch units and setup allocations by a wide margin.
			if allocs < wantAllocs || allocs > wantAllocs+0.01 || bytes < wantBytes || bytes > wantBytes+1 {
				t.Fatalf("per-call allocations = %.4f, bytes = %.4f; want %.0f and %.0f (batch=%d)", allocs, bytes, wantAllocs, wantBytes, batch)
			}
			wantNS := float64(result.T.Nanoseconds()) / (float64(result.N) * batch)
			if result.Extra["ns/op"] != wantNS {
				t.Fatalf("ns/op=%g, want per-call %g", result.Extra["ns/op"], wantNS)
			}
		})
	}
}

// These controls exercise the same calibration and reporting used by corpus
// Exec rows. The allocating control exposes accidental per-batch memory units.
func BenchmarkExecBatchAccounting(b *testing.B) {
	b.Run("no_allocations", func(b *testing.B) {
		benchmarkExecCalls(b, func() error {
			execBatchWork++
			return nil
		})
	})
	b.Run("64_bytes_per_call", func(b *testing.B) {
		benchmarkExecCalls(b, func() error {
			execBatchAllocationSink = make([]byte, 64)
			execBatchAllocationSink[0] = byte(execBatchWork)
			execBatchWork++
			return nil
		})
	})
}
