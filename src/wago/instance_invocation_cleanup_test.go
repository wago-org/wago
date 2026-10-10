//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func invocationCleanupCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_INVOCATION_CLEANUP_ARTIFACT_DIR"), os.Getenv("WAGO_INVOCATION_CLEANUP_ARTIFACT_MODE")
	path := filepath.Join(dir, "invocation-cleanup.wago")
	if mode == "load" {
		if !artifact {
			t.Fatal("compiler-free runner selected source")
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		c, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 17, 0x0b}))),
	)
	c, err := Compile(nil, module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !artifact {
		return c
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, blob, 0600); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	return loaded
}

func TestCloseMappedPhysicalPanicReportsAndCompletes(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, panicKind := range []string{"none", "value", "nil"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, panicKind), func(t *testing.T) {
				in, err := Instantiate(invocationCleanupCompile(t, artifact))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { cleanupTestPanic(func() { _ = in.Close() }) })
				if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
					t.Fatal("mapped invocation", got, err)
				}
				var callbacks [2]int
				in.referenceLifetime().afterPhysicalRelease(func() {
					callbacks[0]++
					switch panicKind {
					case "value":
						panic("physical release")
					case "nil":
						panic(nil)
					}
				})
				in.referenceLifetime().afterPhysicalRelease(func() { callbacks[1]++ })
				closeErr := in.Close()
				panics := panicKind != "none"
				if panics && !errors.Is(closeErr, ErrCallbackPanic) || !panics && closeErr != nil {
					t.Fatal("Close panic report", closeErr, panicKind)
				}
				state := in.closeState.Load()
				if state == nil || !state.completed.Load() {
					t.Fatal("Close completion was not published", state)
				}
				if err := in.WaitClosed(nil); panics && !errors.Is(err, ErrCallbackPanic) || !panics && err != nil {
					t.Fatal("completed Close panic report", err, panicKind)
				}
				in.lifeMu.Lock()
				resourcesClosed, resourceRefs := in.resourcesClosed, in.resourceRefs
				in.lifeMu.Unlock()
				if !resourcesClosed || resourceRefs != 0 || callbacks != [2]int{1, 1} || in.jm != nil || in.ar != nil || in.eng != nil {
					t.Fatal("physical panic interrupted mapped Close", state, resourcesClosed, resourceRefs, callbacks, in.jm, in.ar, in.eng)
				}
				if err := in.Close(); panics && !errors.Is(err, ErrCallbackPanic) || !panics && err != nil || callbacks != [2]int{1, 1} {
					t.Fatal("repeated Close changed result or callbacks", err, callbacks)
				}
			})
		}
	}
}

// A producer's real physical callback closes a protected mapped instance. Its
// last invocation then owns that instance's finalization and Runtime operation.
func TestInvocationCleanupMappedRuntimeFinalizer(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, panics := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-panic%t", artifact, panics), func(t *testing.T) {
				rt := NewRuntime()
				defer func() {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					if err := rt.CloseContext(ctx); err != nil {
						t.Error("Runtime cleanup", err)
					}
				}()
				c := invocationCleanupCompile(t, artifact)
				m, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				defer m.Close()
				instantiate := func() *Instance {
					in, err := rt.Instantiate(context.Background(), m)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if in.jm == nil || in.ar == nil || in.eng == nil {
							t.Fatal("fixture lacks mapped resources")
						}
					}()
					got, err := in.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != 17 {
						t.Fatal("mapped invocation", got, err)
					}
					return in
				}
				owner, producer := instantiate(), instantiate()
				defer owner.Close()
				defer producer.Close()
				entered, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				var workerDone chan struct{}
				ownedInvocations := 0
				producerRoot := false
				defer func() {
					unblock()
					if workerDone != nil {
						select {
						case <-workerDone:
						case <-time.After(3 * time.Second):
							t.Error("invocation cleanup worker did not join")
						}
					}
					var cleanup resourceCleanup
					if producerRoot {
						producerRoot = false
						cleanup.run(producer.releaseResourceRoot)
					}
					for ownedInvocations != 0 {
						ownedInvocations--
						cleanup.run(owner.endInvocation)
					}
					if cleanup.panicked {
						t.Error("fixture failure cleanup panicked", cleanup.value)
					}
				}()
				marker := new(int)
				ownerCallbacks, producerCallbacks := 0, 0
				owner.referenceLifetime().afterPhysicalRelease(func() {
					ownerCallbacks++
					if !rt.mu.TryLock() {
						panic("physical callback under Runtime.mu")
					}
					active := rt.activeOperations
					rt.mu.Unlock()
					if active != 1 {
						panic("Runtime operation ended before physical callback")
					}
					close(entered)
					<-release
					if panics {
						panic(marker)
					}
				})
				producer.referenceLifetime().afterPhysicalRelease(func() {
					producerCallbacks++
					if err := owner.Close(); err != nil {
						panic(err)
					}
				})
				if err := owner.beginInvocation(); err != nil {
					t.Fatal(err)
				}
				ownedInvocations++
				if err := owner.beginInvocation(); err != nil {
					t.Fatal(err)
				}
				ownedInvocations++
				ownedInvocations--
				owner.endInvocation() // The nonterminal path owns only its own Runtime unit.
				if !producer.retainResourceRoot() {
					t.Fatal("actual producer root")
				}
				producerRoot = true
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				producerRoot = false
				producer.releaseResourceRoot()
				if owner.resourcesClosed || owner.invocationState.Load() != instanceInvocationClosed+1 {
					t.Fatal("producer callback lost the protected owner")
				}
				type result struct {
					value    any
					panicked bool
				}
				done := make(chan result, 1)
				workerDone = make(chan struct{})
				ownedInvocations-- // The worker now owns this exact invocation unit.
				go func() {
					defer close(workerDone)
					value, panicked := cleanupTestPanic(owner.endInvocation)
					done <- result{value, panicked}
				}()
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("finalizer did not enter")
				}
				if err := rt.Close(); err != nil {
					t.Fatal(err)
				}
				select {
				case <-rt.Closed():
					t.Fatal("Runtime shutdown preceded callback completion")
				default:
				}
				unblock()
				var got result
				select {
				case got = <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("last invocation did not finish")
				}
				if got.panicked != panics || panics && got.value != marker {
					t.Error("first raw panic", got)
				}
				rt.mu.Lock()
				active := rt.activeOperations
				rt.mu.Unlock()
				if active != 0 {
					t.Error("finalizer stranded Runtime operation", active)
					// Only a failed oracle repairs the old accounting bug so its baseline
					// process can finish teardown. Production counters are never substituted.
					rt.mu.Lock()
					rt.activeOperations = 0
					rt.stateCond.Broadcast()
					rt.mu.Unlock()
				}
				if ownerCallbacks != 1 || producerCallbacks != 1 {
					t.Error("callback count", ownerCallbacks, producerCallbacks)
				}
				for _, in := range []*Instance{owner, producer} {
					if in.invocationState.Load() != instanceInvocationClosed || !in.resourcesClosed || in.finalizing || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil {
						t.Error("mapped teardown incomplete")
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := rt.WaitClosed(ctx); err != nil {
					t.Error("Runtime drain", err)
				}
			})
		}
	}
}

func TestInvocationCleanupRuntimeWarmAllocation(t *testing.T) {
	rt := NewRuntime()
	in := &Instance{rt: rt}
	allocations := testing.AllocsPerRun(100, func() {
		if err := in.beginInvocation(); err != nil {
			panic(err)
		}
		in.endInvocation()
	})
	if allocations != 0 || rt.activeOperations != 0 || in.invocationState.Load() != 0 {
		t.Fatal("warm Runtime invocation", allocations, rt.activeOperations)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}
