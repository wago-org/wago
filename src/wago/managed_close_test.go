package wago

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func managedCloseRuntime(t *testing.T, p *disposalTestPlugin) (*Runtime, *InstanceManager, *Module) {
	t.Helper()
	p.id = "test.managed-close"
	p.requires = []PluginCapability{PluginInstanceHooks, PluginManagedInstances}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginInstanceHooks, PluginManagedInstances)); err != nil {
		t.Fatal(err)
	}
	p.manager.budget = AuthorityScope{MaxInstances: 1, MaxMemoryBytes: 65536}
	mod, err := rt.Compile(wasmtest.Module(wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1}))))
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
	return rt, p.manager, mod
}

func assertManagedDetached(t *testing.T, manager *InstanceManager, owned *ManagedInstance, in *Instance) {
	t.Helper()
	manager.mu.Lock()
	records, callers := len(manager.instances), len(manager.byInstance)
	manager.mu.Unlock()
	owned.mu.Lock()
	detached := owned.manager == nil && owned.value == nil && owned.closedValue == in
	owned.mu.Unlock()
	if records != 0 || callers != 0 || !detached {
		t.Fatalf("terminal ownership: records=%d callers=%d detached=%v", records, callers, detached)
	}
}

func assertManagedReservationsReleased(t *testing.T, manager *InstanceManager) {
	t.Helper()
	manager.mu.Lock()
	live, memory := manager.live, manager.memoryBytes
	manager.mu.Unlock()
	manager.rt.mu.Lock()
	mappings, reservations := manager.rt.nativeMemoryMappings, len(manager.rt.instanceReservations)
	manager.rt.mu.Unlock()
	if live != 0 || memory != 0 || mappings != 0 || reservations != 0 {
		t.Fatalf("reservations: live=%d memory=%d mappings=%d instances=%d", live, memory, mappings, reservations)
	}
}

func TestManagedCloseWaitClosed(t *testing.T) {
	for _, phase := range []string{"normal", "before-panic", "after-panic", "both-panic"} {
		t.Run(phase, func(t *testing.T) {
			beforePanic, afterPanic := phase == "before-panic" || phase == "both-panic", phase == "after-panic" || phase == "both-panic"
			entered, release := make(chan struct{}), make(chan struct{})
			p := &disposalTestPlugin{
				beforeClose: []func(*InstanceContext){func(*InstanceContext) {
					if beforePanic {
						panic("before close")
					}
				}},
				afterClose: []func(*InstanceContext){func(*InstanceContext) {
					close(entered)
					awaitCloseHookRelease(release)
					if afterPanic {
						panic("after close")
					}
				}},
			}
			_, manager, mod := managedCloseRuntime(t, p)
			owned, err := manager.Instantiate(nil, mod)
			if err != nil {
				t.Fatal(err)
			}
			in := owned.Instance()
			if !in.retainResourceRoot() {
				t.Fatal("retain")
			}
			released := make(chan struct{})
			in.referenceLifetime().afterPhysicalRelease(func() { close(released) })
			closed := make(chan error, 1)
			go func() { closed <- owned.Close() }()
			awaitCloseSignal(t, entered)
			// There are no active invocations: Close must still return while
			// AfterClose is blocked, and must report only logical errors.
			logicalErr := awaitCloseResult(t, closed)
			if errors.Is(logicalErr, ErrCallbackPanic) != beforePanic || (!beforePanic && logicalErr != nil) {
				t.Errorf("Close = %v", logicalErr)
			}
			manager.mu.Lock()
			pending := len(manager.instances) == 1 && len(manager.byInstance) == 0
			manager.mu.Unlock()
			if !pending || owned.Instance() != nil {
				t.Error("logical close lost pending ownership or left admission open")
			}
			done := make(chan error, 1)
			go func() { done <- owned.WaitClosed() }()
			awaitTerminalWait(t, done)
			close(release)
			terminalErr := awaitCloseResult(t, done)
			if errors.Is(terminalErr, ErrCallbackPanic) != (beforePanic || afterPanic) {
				t.Fatalf("WaitClosed = %v", terminalErr)
			}
			if afterPanic && !strings.Contains(terminalErr.Error(), "AfterClose") {
				t.Fatalf("missing terminal error: %v", terminalErr)
			}
			for i := 0; i < 3; i++ {
				if err := owned.WaitClosed(); err != terminalErr && (err == nil || terminalErr == nil || err.Error() != terminalErr.Error()) {
					t.Errorf("repeated WaitClosed = %v, want %v", err, terminalErr)
				}
			}
			assertManagedDetached(t, manager, owned, in)
			manager.mu.Lock()
			retainedBudget := manager.live == 1 && manager.memoryBytes == 65536
			manager.mu.Unlock()
			if !retainedBudget || !in.referenceLifetime().snapshot().PhysicalResources {
				t.Fatal("terminal wait released retained physical resources")
			}
			in.releaseResourceRoot()
			awaitCloseSignal(t, released)
			assertManagedReservationsReleased(t, manager)
		})
	}
}

func TestManagedCloseAutomaticOwnership(t *testing.T) {
	for _, mode := range []string{"inline", "terminal-worker", "retained"} {
		t.Run(mode, func(t *testing.T) {
			p := &disposalTestPlugin{}
			if mode != "inline" {
				p.afterClose = []func(*InstanceContext){func(*InstanceContext) {}}
			}
			_, manager, mod := managedCloseRuntime(t, p)
			for i := 0; i < 20; i++ {
				owned, err := manager.Instantiate(nil, mod)
				if err != nil {
					t.Fatal(err)
				}
				in := owned.Instance()
				if mode == "retained" && !in.retainResourceRoot() {
					t.Fatal("retain")
				}
				released := make(chan struct{})
				in.referenceLifetime().afterPhysicalRelease(func() { close(released) })
				if err := owned.Close(); err != nil {
					t.Fatal(err)
				}
				// No public wait or second close: the terminal finalizer owns
				// detachment, and physical release owns budget accounting.
				awaitCloseSignal(t, in.ensurePluginState().close.Load().terminalDone)
				assertManagedDetached(t, manager, owned, in)
				if mode == "retained" {
					if !in.referenceLifetime().snapshot().PhysicalResources {
						t.Fatal("retained resources released during detachment")
					}
					in.releaseResourceRoot()
				}
				awaitCloseSignal(t, released)
				assertManagedReservationsReleased(t, manager)
			}
		})
	}
}

func TestManagedCloseConcurrentShutdown(t *testing.T) {
	var owned *ManagedInstance
	var before, after atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	p := &disposalTestPlugin{
		beforeClose: []func(*InstanceContext){func(*InstanceContext) { before.Add(1); _ = owned.Close() }},
		afterClose: []func(*InstanceContext){func(*InstanceContext) {
			after.Add(1)
			close(entered)
			awaitCloseHookRelease(release)
			_ = owned.Close()
		}},
	}
	rt, manager, mod := managedCloseRuntime(t, p)
	var err error
	owned, err = manager.Instantiate(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in := owned.Instance()
	if err := in.beginInvocation(); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	in.referenceLifetime().afterPhysicalRelease(func() { close(released) })
	start, results := make(chan struct{}), make(chan error, 18)
	for i := 0; i < 8; i++ {
		go func() { <-start; results <- owned.Close() }()
		go func() { <-start; results <- owned.WaitClosed() }()
	}
	go func() { <-start; results <- manager.close() }()
	go func() { <-start; results <- rt.Close() }()
	close(start)
	// Race the final invocation exit with logical close preparation.
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	in.endInvocation()
	awaitCloseSignal(t, entered)
	close(release)
	for i := 0; i < 18; i++ {
		if err := awaitCloseResult(t, results); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rt.WaitClosed(ctx); err != nil {
		t.Fatal(err)
	}
	awaitCloseSignal(t, released)
	if before.Load() != 1 || after.Load() != 1 {
		t.Fatalf("hook counts = %d/%d", before.Load(), after.Load())
	}
	assertManagedDetached(t, manager, owned, in)
	assertManagedReservationsReleased(t, manager)
}

func TestManagedCloseBeforeAdoption(t *testing.T) {
	p := &disposalTestPlugin{afterInst: []func(*InstantiateContext, *Instance) error{
		func(_ *InstantiateContext, in *Instance) error { return in.Close() },
	}}
	_, manager, mod := managedCloseRuntime(t, p)
	owned, err := manager.Instantiate(nil, mod)
	if owned != nil || err == nil {
		t.Fatalf("adopt closed child = %v, %v", owned, err)
	}
	manager.mu.Lock()
	records, callers := len(manager.instances), len(manager.byInstance)
	manager.mu.Unlock()
	if records != 0 || callers != 0 {
		t.Fatalf("closed child registered: %d/%d", records, callers)
	}
	assertManagedReservationsReleased(t, manager)
}

func TestManagedWaitClosedInitiatesClose(t *testing.T) {
	var after atomic.Int32
	p := &disposalTestPlugin{afterClose: []func(*InstanceContext){func(*InstanceContext) { after.Add(1) }}}
	_, manager, mod := managedCloseRuntime(t, p)
	owned, err := manager.Instantiate(nil, mod)
	if err != nil {
		t.Fatal(err)
	}
	in := owned.Instance()
	done := make(chan error, 1)
	go func() { done <- owned.WaitClosed() }()
	if err := awaitCloseResult(t, done); err != nil {
		t.Fatal(err)
	}
	if after.Load() != 1 {
		t.Fatalf("AfterClose calls = %d", after.Load())
	}
	assertManagedDetached(t, manager, owned, in)
}
