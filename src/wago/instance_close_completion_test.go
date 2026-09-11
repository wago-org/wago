package wago

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func closeCompletionInstance(t *testing.T, hooks *hookRegistry) (*Runtime, *Instance) {
	t.Helper()
	rt := NewRuntime()
	rt.storeHooks(hooks)
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.CloseContext(context.Background()); _ = mod.Close() })
	return rt, in
}

func TestClosePreparationOrdersTerminalHooks(t *testing.T) {
	for _, phase := range []string{"internal", "public", "panic"} {
		t.Run(phase, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var after atomic.Int32
			block := func() {
				close(entered)
				<-release
				if phase == "panic" {
					panic("before")
				}
			}
			hooks := &hookRegistry{afterClose: []func(InstanceCloseEvent){func(InstanceCloseEvent) { after.Add(1) }}}
			if phase == "internal" {
				hooks.internalBeforeClose = []func(*Instance){func(*Instance) { block() }}
			} else {
				hooks.beforeClose = []func(InstanceCloseEvent){func(InstanceCloseEvent) { block() }}
			}
			_, in := closeCompletionInstance(t, hooks)
			if err := in.beginInvocation(); err != nil {
				t.Fatal(err)
			}
			closed := make(chan error, 1)
			go func() { closed <- in.Close() }()
			<-entered
			in.endInvocation()
			gotEarly := after.Load()
			close(release)
			err := <-closed
			if gotEarly != 0 {
				t.Fatal("AfterClose ran before BeforeClose completed")
			}
			if errors.Is(err, ErrCallbackPanic) != (phase == "panic") {
				t.Fatalf("Close = %v", err)
			}
			_ = in.closeAndWait()
			_ = in.Close()
			if after.Load() != 1 {
				t.Fatalf("AfterClose calls = %d, want 1", after.Load())
			}
		})
	}
}

func TestCloseTerminalWaitsForConstruction(t *testing.T) {
	var after atomic.Int32
	_, in := closeCompletionInstance(t, &hookRegistry{afterClose: []func(InstanceCloseEvent){func(InstanceCloseEvent) { after.Add(1) }}})
	in.beginConstruction(nil)
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if after.Load() != 0 {
		t.Error("AfterClose ran during construction")
	}
	in.endConstruction()
	if err := in.closeAndWait(); err != nil {
		t.Fatal(err)
	}
	if after.Load() != 1 {
		t.Fatalf("AfterClose calls = %d, want 1", after.Load())
	}
}

func TestCloseTerminalWaiters(t *testing.T) {
	for _, waiter := range []string{"instance", "managed", "drain", "drain-logically-closed"} {
		for _, panics := range []bool{false, true} {
			name := waiter
			if panics {
				name += "/panic"
			}
			t.Run(name, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				rt, in := closeCompletionInstance(t, &hookRegistry{afterClose: []func(InstanceCloseEvent){func(event InstanceCloseEvent) {
					close(entered)
					<-release
					// Callback reentry must not wait for its own terminal phase.
					if err := event.Instance.value.Close(); err != nil {
						panic(err)
					}
					event.Instance.value.releaseResourceRoot()
					if panics {
						panic("after")
					}
				}}})
				manager := newPendingInstanceManager("close-test", AuthorityScope{})
				manager.activate(rt)
				manager.live = 1
				owned, err := manager.adopt(in, 0)
				if err != nil {
					t.Fatal(err)
				}
				if !in.retainResourceRoot() || !in.retainResourceRoot() {
					t.Fatal("retain resources")
				}
				t.Cleanup(in.releaseResourceRoot)
				if err := in.beginInvocation(); err != nil {
					t.Fatal(err)
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				if waiter == "drain-logically-closed" {
					if _, err := owned.closeLogical(); err != nil {
						t.Fatal(err)
					}
				}
				ended := make(chan struct{})
				go func() { in.endInvocation(); close(ended) }()
				<-entered
				state := in.ensurePluginState().close.Load()
				select {
				case <-state.quiesced:
				default:
					t.Fatal("invocations did not quiesce")
				}
				started, done := make(chan struct{}), make(chan error, 1)
				go func() {
					close(started)
					switch waiter {
					case "instance":
						done <- in.closeAndWait()
					case "managed":
						done <- owned.Close()
					case "drain", "drain-logically-closed":
						done <- manager.close()
					}
				}()
				<-started
				select {
				case <-state.terminalDone:
					t.Error("terminal completion preceded AfterClose")
				default:
				}
				select {
				case err := <-done:
					t.Errorf("terminal waiter returned early: %v", err)
					done <- err
				default:
				}
				close(release)
				err = <-done
				<-ended
				if errors.Is(err, ErrCallbackPanic) != panics {
					t.Fatalf("terminal waiter = %v", err)
				}
				if !in.referenceLifetime().snapshot().PhysicalResources {
					t.Fatal("retained resources were released")
				}
			})
		}
	}
}
