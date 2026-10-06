//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"sync/atomic"
	"testing"
	"unsafe"
)

// These are Go-only diagnostics, not Wasm call latency. Every loop checks the
// same atomic counter and result. Separately measured costs must not be
// subtracted from the full-runtime measurements: loop/call layouts differ.
func BenchmarkBoundedHostCallbackCosts(b *testing.B) {
	for _, mode := range []string{"callback", "mutex-pair", "method-dispatch", "context-dispatch", "caller-scope"} {
		b.Run(mode, func(b *testing.B) {
			var counter atomic.Uint64
			imports := NewImports()
			imports.HostFunc("env", "step", func(v int32) int32 { counter.Add(1); return v + 1 }).Params(ValI32).Results(ValI32)
			c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("run", 1, 0); err != nil {
				b.Fatal(err)
			}
			counter.Store(0)
			state := in.ensurePluginState()
			mu := &state.nativeExecutionMu
			activation := boundedTypedHostActivation{root: in, ctrl: offHeapSlicePtr(in.ctrl), state: state, entryNativeMu: mu}
			if activation.localNativeMu() != mu {
				b.Fatal("requires independent native lease")
			}
			callback := in.syncHosts[0].typedI32
			var scope hostCallScope
			var total uint64
			mu.Lock()
			defer mu.Unlock()
			b.ReportAllocs()
			b.ResetTimer()
			switch mode {
			case "callback":
				for i := 0; i < b.N; i++ {
					total += uint64(callback(0))
				}
			case "mutex-pair":
				for i := 0; i < b.N; i++ {
					mu.Unlock()
					total += uint64(callback(0))
					mu.Lock()
				}
			case "method-dispatch":
				for i := 0; i < b.N; i++ {
					total += activation.dispatchI32(0, 0)
				}
			case "context-dispatch":
				for i := 0; i < b.N; i++ {
					total += boundedTypedHostDispatchI32(unsafe.Pointer(&activation), 0, 0)
				}
			case "caller-scope":
				for i := 0; i < b.N; i++ {
					generation, parent := scope.beginGeneration(nil)
					total += uint64(callback(0))
					scope.end(generation, parent)
				}
			}
			b.StopTimer()
			if total != uint64(b.N) || counter.Load() != uint64(b.N) {
				b.Fatalf("result=%d counter=%d want %d", total, counter.Load(), b.N)
			}
			if scope.active.Load() != 0 || mode == "caller-scope" && scope.sequence.Load() != uint64(b.N) {
				b.Fatal("scope lifetime")
			}
		})
	}
}
