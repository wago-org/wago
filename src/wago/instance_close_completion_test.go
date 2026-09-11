package wago

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := rt.CloseContext(ctx); errors.Is(err, context.DeadlineExceeded) {
			t.Error("runtime cleanup timed out")
		}
		_ = mod.Close()
	})
	return rt, in
}

func TestClosePreparationOrdersTerminalHooks(t *testing.T) {
	for _, phase := range []string{"internal", "public", "panic"} {
		t.Run(phase, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var after atomic.Int32
			block := func() {
				close(entered)
				awaitCloseHookRelease(release)
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
			awaitCloseSignal(t, entered)
			in.endInvocation()
			gotEarly := after.Load()
			close(release)
			err := awaitCloseResult(t, closed)
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
				var owned *ManagedInstance
				rt, in := closeCompletionInstance(t, &hookRegistry{afterClose: []func(InstanceCloseEvent){func(event InstanceCloseEvent) {
					close(entered)
					awaitCloseHookRelease(release)
					// Callback reentry must not wait for its own terminal phase.
					if err := event.Instance.value.Close(); err != nil {
						panic(err)
					}
					if err := owned.Close(); err != nil {
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
				var err error
				owned, err = manager.adopt(in, 0)
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
					if err := owned.Close(); err != nil {
						t.Fatal(err)
					}
				}
				ended := make(chan struct{})
				go func() { in.endInvocation(); close(ended) }()
				awaitCloseSignal(t, entered)
				state := in.ensurePluginState().close.Load()
				select {
				case <-state.quiesced:
				default:
					t.Fatal("invocations did not quiesce")
				}
				done := make(chan error, 1)
				go func() {
					switch waiter {
					case "instance":
						done <- in.closeAndWait()
					case "managed":
						done <- owned.WaitClosed()
					case "drain", "drain-logically-closed":
						done <- manager.close()
					}
				}()
				awaitTerminalWait(t, done)
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
				err = awaitCloseResult(t, done)
				awaitCloseSignal(t, ended)
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

func TestManagedCloseCallbackReentry(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		t.Run(phase, func(t *testing.T) {
			var owned *ManagedInstance
			var calls atomic.Int32
			hook := func(InstanceCloseEvent) {
				calls.Add(1)
				if err := owned.Close(); err != nil {
					panic(err)
				}
			}
			hooks := &hookRegistry{}
			if phase == "before" {
				hooks.beforeClose = []func(InstanceCloseEvent){hook}
			} else {
				hooks.afterClose = []func(InstanceCloseEvent){hook}
			}
			rt, in := closeCompletionInstance(t, hooks)
			manager := newPendingInstanceManager("close-reentry", AuthorityScope{})
			manager.activate(rt)
			manager.live = 1
			var err error
			owned, err = manager.adopt(in, 0)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if err := owned.Close(); err != nil {
					done <- err
					return
				}
				done <- owned.WaitClosed()
			}()
			if err := awaitCloseResult(t, done); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || owned.Instance() != nil {
				t.Fatal("logical callback close did not complete exactly once")
			}
			manager.mu.Lock()
			remaining := len(manager.instances)
			manager.mu.Unlock()
			if remaining != 0 {
				t.Fatal("terminal close retained managed ownership")
			}
		})
	}
}

func awaitCloseHookRelease(release <-chan struct{}) {
	select {
	case <-release:
	case <-time.After(10 * time.Second):
		panic("close hook release timed out")
	}
}

func awaitCloseSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatal("close signal timed out")
	}
}

func awaitCloseResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("close completion timed out")
	}
}

// Observe the blocked receive itself, not a signal sent before entering Close.
// This is test-only: no wait probes or allocations enter production close paths.
func awaitTerminalWait(t *testing.T, done chan error) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n == 0 {
			// TinyGo has no goroutine stack snapshots. Its tasks scheduler
			// runs the queued waiter until it blocks when this task yields.
			runtime.Gosched()
			return
		}
		for _, stack := range strings.Split(string(buf[:n]), "\n\n") {
			if strings.Contains(stack, "[chan receive]:") && strings.Contains(stack, "(*Instance).waitTerminalClose(") && strings.Contains(stack, strings.Split(t.Name(), "/")[0]+".func") {
				return
			}
		}
		select {
		case err := <-done:
			t.Errorf("terminal waiter returned before blocking: %v", err)
			done <- err
			return
		case <-deadline.C:
			t.Error("terminal waiter did not enter its channel wait")
			return
		default:
			runtime.Gosched()
		}
	}
}
