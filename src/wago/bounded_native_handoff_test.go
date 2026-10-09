//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBoundedDirectGuardedNativeHandoff(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, api := range []string{"instance", "prepared", "session"} {
		for _, kind := range []string{"typed", "HostCall", "Caller"} {
			for _, panics := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/panic%t", api, kind, panics), func(t *testing.T) {
					entered, leave, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
					calls := 0
					sentinel := &struct{}{}
					step := func(v int32) int32 {
						calls++
						if calls == 3 {
							close(entered)
							<-leave
							close(exited)
							if panics {
								panic(sentinel)
							}
						}
						return v + 1
					}
					var callback any = step
					if kind == "HostCall" {
						callback = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
					} else if kind == "Caller" {
						callback = func(_ Caller, call HostCall) { call.SetI32(0, step(call.I32(0))) }
					}
					imports := NewImports()
					imports.HostFunc("env", "step", callback).Params(ValI32).Results(ValI32)
					in, err := Instantiate(c, InstantiateOptions{Imports: imports})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if numericContextDetached(in) {
						t.Skip("mutable-context handoff; detached contexts are covered by TestDetachedNumericResourceIsolation")
					}
					invoke := func() ([]uint64, error) { return in.Invoke("run", 1, 0) }
					fn, err := in.WasmFunc("run")
					if err != nil {
						t.Fatal(err)
					}
					if api == "prepared" {
						invoke = func() ([]uint64, error) { return fn.Invoke(1, 0) }
					} else if api == "session" {
						session, err := fn.OpenSession()
						if err != nil {
							t.Fatal(err)
						}
						defer session.Close()
						invoke = func() ([]uint64, error) { return session.Invoke2(1, 0) }
					}
					check := func() {
						t.Helper()
						got, err := invoke()
						if err != nil || len(got) != 1 || got[0] != 1 {
							t.Fatalf("result %v, %v", got, err)
						}
					}
					check()
					check()
					state := in.ensurePluginState()
					version, id := state.nativeContextVersion.Load(), state.invocationID
					type outcome struct {
						result     []uint64
						err        error
						panicValue any
					}
					returned := make(chan outcome, 1)
					go func() {
						var out outcome
						defer func() { out.panicValue = recover(); returned <- out }()
						out.result, out.err = invoke()
					}()
					select {
					case <-entered:
					case <-time.After(2 * time.Second):
						t.Fatal("callback did not enter")
					}
					guarded := make(chan *sync.Mutex, 1)
					go func() { guarded <- in.acquireInstanceNativeStateForHostAccess() }()
					var mu *sync.Mutex
					select {
					case mu = <-guarded:
					case <-time.After(2 * time.Second):
						t.Fatal("parked callback blocked guarded native access")
					}
					close(leave)
					<-exited
					select {
					case out := <-returned:
						mu.Unlock()
						t.Fatalf("callback resumed through active native guard: %+v", out)
					case <-time.After(5 * time.Millisecond):
					}
					mu.Unlock()
					select {
					case out := <-returned:
						if panics {
							if out.panicValue != sentinel {
								t.Fatalf("panic %v", out.panicValue)
							}
						} else if out.panicValue != nil || out.err != nil || len(out.result) != 1 || out.result[0] != 1 {
							t.Fatalf("handoff result: %+v", out)
						}
					case <-time.After(2 * time.Second):
						t.Fatal("callback did not reacquire native ownership")
					}
					if state.nativeContextVersion.Load() <= version || state.invocationID != id {
						t.Fatal("handoff retained stale context or changed reservation")
					}
					check()
					if calls != 4 {
						t.Fatal("callback count", calls)
					}
				})
			}
		}
	}
}

// An accessor can be descheduled between selecting the local mutex and locking
// it. A later callback must still let that already-waiting accessor acquire it.
func TestBoundedDirectGuardedAccessAcrossCallbacks(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	first, advance, second, accessed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	calls := 0
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 {
		calls++
		if calls == 3 {
			close(first)
			<-advance
		}
		if calls == 4 {
			close(second)
			<-accessed
		}
		return v + 1
	}).Params(ValI32).Results(ValI32)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 2; i++ {
		if _, err := in.Invoke("run", 1, 0); err != nil {
			t.Fatal(err)
		}
	}
	type outcome struct {
		got []uint64
		err error
	}
	returned := make(chan outcome, 1)
	go func() { got, err := in.Invoke("run", 2, 0); returned <- outcome{got, err} }()
	<-first
	// These are the same two steps as guarded host access, with scheduling
	// deliberately paused after mutex selection and before mutex acquisition.
	mu := in.independentNativeExecutionMu()
	close(advance)
	<-second
	go func() { mu.Lock(); in.invalidateNativeContext(); mu.Unlock(); close(accessed) }()
	timedOut := false
	select {
	case <-accessed:
	case <-time.After(500 * time.Millisecond):
		timedOut = true
		// Select again to release the experimental parked lease and drain the
		// invocation, so a failed interleaving does not strand test resources.
		in.independentNativeExecutionMu()
	}
	select {
	case out := <-returned:
		if out.err != nil || len(out.got) != 1 || out.got[0] != 2 {
			t.Fatalf("result %v, %v", out.got, out.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guarded access stranded invocation")
	}
	if timedOut {
		t.Fatal("later callback blocked an accessor that had already selected the native mutex")
	}
}
