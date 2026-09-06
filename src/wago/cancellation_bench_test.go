//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/tests/wasmtest"
)

// Report cancel-to-return tails separately from the scheduled work interval.
// Keep only a bounded window; benchmark duration cannot grow retained memory.
func BenchmarkInterruptCancelLatency(b *testing.B) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}))),
	)
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(data)
	if err != nil {
		b.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	samples := make([]int64, min(b.N, 1024))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		var canceledAt atomic.Int64
		timer := time.AfterFunc(100*time.Microsecond, func() {
			canceledAt.Store(time.Now().UnixNano())
			cancel()
		})
		_, err := in.Call(ctx, "spin")
		returnedAt := time.Now().UnixNano()
		timer.Stop()
		cancel()
		if !errors.Is(err, context.Canceled) || canceledAt.Load() == 0 {
			b.Fatalf("cancelled invocation: %v, timestamp %d", err, canceledAt.Load())
		}
		samples[i%len(samples)] = returnedAt - canceledAt.Load()
	}
	b.StopTimer()
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	for _, q := range []struct {
		name    string
		percent int
	}{{"p50-ns", 50}, {"p95-ns", 95}, {"p99-ns", 99}} {
		b.ReportMetric(float64(samples[(len(samples)-1)*q.percent/100]), q.name)
	}
}
