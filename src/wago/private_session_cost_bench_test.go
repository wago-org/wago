//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"sync/atomic"
	"testing"
)

// Full session round trips, with the same checked atomic callback as the
// standalone latency harness. Profiles are diagnostic and are not latency claims.
func BenchmarkPrivateSessionRoundtrip(b *testing.B) {
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		b.Run(kind, func(b *testing.B) {
			var counter atomic.Uint64
			step := func(v int32) int32 { counter.Add(1); return v + 1 }
			var callback any = step
			if kind == "HostCall" {
				callback = func(c HostCall) { c.SetI32(0, step(c.I32(0))) }
			}
			if kind == "Caller" {
				callback = func(_ Caller, c HostCall) { c.SetI32(0, step(c.I32(0))) }
			}
			imports := NewImports()
			imports.HostFunc("env", "step", callback).Params(ValI32).Results(ValI32)
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
			fn, err := in.WasmFunc("run")
			if err != nil {
				b.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				b.Fatal(err)
			}
			defer session.Close()
			if session.state.privateHost == nil {
				b.Fatal("private session not admitted")
			}
			for i := 0; i < 16; i++ {
				if _, err := session.Invoke2(1, 0); err != nil {
					b.Fatal(err)
				}
			}
			counter.Store(0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := session.Invoke2(1, 0)
				if err != nil || len(got) != 1 || got[0] != 1 {
					b.Fatalf("call = %v, %v", got, err)
				}
			}
			b.StopTimer()
			if counter.Load() != uint64(b.N) {
				b.Fatalf("callbacks = %d, want %d", counter.Load(), b.N)
			}
		})
	}
}
