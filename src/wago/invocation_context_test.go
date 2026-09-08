package wago

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/tests/wasmtest"
)

type invocationContextTestState struct {
	resolver *CallerResolver
	manager  *InstanceManager
	outer    HostFunc
	inner    HostFunc
}

type invocationContextTestPlugin struct{ state *invocationContextTestState }

func invocationContextTestProvider(state *invocationContextTestState) PluginProvider {
	definition := testDefinition("example.com/invocation-context")
	definition.Authorities = []AuthorityRequest{
		{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "define context test imports", Scope: AuthorityScope{Modules: []string{"env"}}},
		{Name: AuthorityHostCallerIdentify, Mode: AuthorityRequired, Reason: "resolve callback invocation context"},
		{Name: AuthorityInstanceManage, Mode: AuthorityRequired, Reason: "exercise managed invocation context", Scope: AuthorityScope{MaxInstances: 4, MaxMemoryBytes: 4 << 16}},
	}
	return PluginProvider{
		Definition: definition,
		New: func() Plugin {
			return invocationContextTestPlugin{state: state}
		},
	}
}

func (p invocationContextTestPlugin) Register(reg *Registrar) error {
	resolver, err := reg.HostCallers()
	if err != nil {
		return err
	}
	manager, err := reg.ManagedInstances()
	if err != nil {
		return err
	}
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	module, err := imports.Module("env")
	if err != nil {
		return err
	}
	p.state.resolver = resolver
	p.state.manager = manager
	module.Func("outer", func(m HostModule, params, results []uint64) {
		if p.state.outer != nil {
			p.state.outer(m, params, results)
		}
	})
	module.Func("inner", func(m HostModule, params, results []uint64) {
		if p.state.inner != nil {
			p.state.inner(m, params, results)
		}
	})
	return nil
}

func newInvocationContextTestRuntime(t testing.TB, state *invocationContextTestState) *Runtime {
	t.Helper()
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, invocationContextTestProvider(state))); err != nil {
		rt.Close()
		t.Fatal(err)
	}
	return rt
}

func invocationContextImportModule(name string) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", name, 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func invocationContextReexportModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "outer", 0, 0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
	)
}

func invocationContextImportedStartModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "outer", 0, 0))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
	)
}

func invocationContextNestedModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(
			importEntry("env", "outer", 0, 0),
			importEntry("env", "inner", 0, 0),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call", 0, 2),
			wasmtest.ExportEntry("nested", 0, 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
		)),
	)
}

func invocationContextManagedTableModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "outer", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

// Callback lifetimes also apply to schedulers that cannot interrupt native
// execution. Those schedulers exercise these paths with a background parent;
// the cancellation test below separately requires rejection before host entry.
func invocationContextTestParent(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	if nativeCancellationSupported() {
		return context.WithDeadline(parent, deadline)
	}
	return parent, func() {}
}

func invocationContextTestDeadline(ctx context.Context, want time.Time) bool {
	got, ok := ctx.Deadline()
	return ok == nativeCancellationSupported() && (!ok || got.Equal(want))
}

func TestCallerResolverInvocationContextContract(t *testing.T) {
	state := new(invocationContextTestState)
	rt := newInvocationContextTestRuntime(t, state)
	defer rt.Close()

	type contextKey struct{}
	deadline := time.Now().Add(time.Hour)
	parent, cancel := invocationContextTestParent(context.WithValue(context.Background(), contextKey{}, "private"), deadline)
	defer cancel()
	var retained context.Context
	var retainedCaller HostModule
	var callbackErr error
	var same, hidValue, deadlineMatches bool
	state.outer = func(caller HostModule, _, _ []uint64) {
		retainedCaller = caller
		first, err := state.resolver.InvocationContext(caller)
		if err != nil {
			callbackErr = err
			return
		}
		second, err := state.resolver.InvocationContext(caller)
		if err != nil {
			callbackErr = err
			return
		}
		retained = first
		same = first == second
		hidValue = first.Value(contextKey{}) == nil
		deadlineMatches = invocationContextTestDeadline(first, deadline)
		callbackErr = first.Err()
	}
	module, err := rt.Compile(invocationContextImportModule("outer"))
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Call(parent, "call"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if callbackErr != nil || retained == nil || !same || !hidValue || !deadlineMatches {
		t.Fatalf("callback context = err %v retained %v same %v hidden-value %v deadline %v", callbackErr, retained != nil, same, hidValue, deadlineMatches)
	}
	select {
	case <-retained.Done():
	default:
		t.Fatal("retained invocation context remained live after callback return")
	}
	if retained.Err() != context.Canceled {
		t.Fatalf("retained context error = %v, want context.Canceled", retained.Err())
	}
	if _, err := state.resolver.InvocationContext(retainedCaller); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expired invocation context lookup = %v, want permission denied", err)
	}
	if _, err := state.resolver.InvocationContext(forgedHostModule{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("forged invocation context lookup = %v, want permission denied", err)
	}
	var nilResolver *CallerResolver
	if _, err := nilResolver.InvocationContext(retainedCaller); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("nil resolver invocation context lookup = %v, want permission denied", err)
	}

	crossState := new(invocationContextTestState)
	crossRuntime := newInvocationContextTestRuntime(t, crossState)
	defer crossRuntime.Close()
	var crossErr error
	crossState.outer = func(caller HostModule, _, _ []uint64) {
		_, crossErr = state.resolver.InvocationContext(caller)
	}
	crossModule, err := crossRuntime.Compile(invocationContextImportModule("outer"))
	if err != nil {
		t.Fatal(err)
	}
	crossInstance, err := crossRuntime.Instantiate(context.Background(), crossModule)
	if err != nil {
		t.Fatal(err)
	}
	defer crossInstance.Close()
	if _, err := crossInstance.Invoke("call"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(crossErr, ErrPermissionDenied) {
		t.Fatalf("cross-runtime invocation context lookup = %v, want permission denied", crossErr)
	}

	var lowLevelErr error
	state.outer = func(caller HostModule, _, _ []uint64) {
		_, lowLevelErr = state.resolver.InvocationContext(caller)
	}
	lowLevel, err := Instantiate(module.Compiled(), InstantiateOptions{Imports: rt.HostImports()})
	if err != nil {
		t.Fatal(err)
	}
	defer lowLevel.Close()
	if _, err := lowLevel.Invoke("call"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(lowLevelErr, ErrPermissionDenied) {
		t.Fatalf("low-level invocation context lookup = %v, want permission denied", lowLevelErr)
	}
}

func TestCallerResolverInvocationContextParentCancellationAndTrap(t *testing.T) {
	t.Run("parent cancellation", func(t *testing.T) {
		state := new(invocationContextTestState)
		rt := newInvocationContextTestRuntime(t, state)
		defer rt.Close()
		entered := make(chan struct{})
		var callbackErr error
		state.outer = func(caller HostModule, _, _ []uint64) {
			ctx, err := state.resolver.InvocationContext(caller)
			if err != nil {
				callbackErr = err
				close(entered)
				return
			}
			close(entered)
			<-ctx.Done()
			callbackErr = ctx.Err()
		}
		module, err := rt.Compile(invocationContextImportModule("outer"))
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), module)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		parent, cancel := context.WithCancel(context.Background())
		defer cancel()
		if !nativeCancellationSupported() {
			_, err := in.Call(parent, "call")
			if err == nil || !strings.Contains(err.Error(), "requires a concurrent scheduler") {
				t.Errorf("Call error = %v, want explicit scheduler rejection", err)
			}
			select {
			case <-entered:
				t.Error("unsupported cancellation entered the host callback")
			default:
			}
			return
		}
		callDone := make(chan error, 1)
		go func() {
			_, err := in.Call(parent, "call")
			callDone <- err
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("host callback did not enter")
		}
		cancel()
		select {
		case err := <-callDone:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("Call error = %v, want nil or context.Canceled", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("parent cancellation did not release host callback")
		}
		if callbackErr != context.Canceled {
			t.Fatalf("callback context error = %v, want context.Canceled", callbackErr)
		}
	})

	t.Run("host trap", func(t *testing.T) {
		state := new(invocationContextTestState)
		rt := newInvocationContextTestRuntime(t, state)
		defer rt.Close()
		trapErr := errors.New("context trap")
		var retained context.Context
		state.outer = func(caller HostModule, _, _ []uint64) {
			retained, _ = state.resolver.InvocationContext(caller)
			panic(HostTrap{Err: trapErr})
		}
		module, err := rt.Compile(invocationContextImportModule("outer"))
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), module)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Invoke("call"); !errors.Is(err, trapErr) {
			t.Fatalf("Invoke error = %v, want host trap", err)
		}
		if retained == nil || retained.Err() != context.Canceled {
			t.Fatalf("trapped callback context = %v/%v, want canceled", retained, retained.Err())
		}
	})
}

func TestCallerResolverInvocationContextWithoutNativeInterruption(t *testing.T) {
	state := new(invocationContextTestState)
	rt := newInvocationContextTestRuntime(t, state)
	defer rt.Close()
	module, err := rt.Compile(invocationContextImportModule("outer"))
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	deadline := time.Now().Add(time.Hour)
	parent, cancel := context.WithDeadline(context.Background(), deadline)
	entered := make(chan struct{})
	var callbackErr error
	var deadlineOK bool
	state.outer = func(caller HostModule, _, _ []uint64) {
		ctx, err := state.resolver.InvocationContext(caller)
		if err != nil {
			callbackErr = err
			close(entered)
			return
		}
		got, ok := ctx.Deadline()
		deadlineOK = ok && got.Equal(deadline)
		close(entered)
		<-ctx.Done()
		callbackErr = ctx.Err()
	}
	callDone := make(chan error, 1)
	go func() {
		_, err := in.invokeEntry("call", nil, invocationContextSet{callback: parent}, false)
		callDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("host callback did not enter")
	}
	cancel()
	select {
	case err := <-callDone:
		if err != nil {
			t.Fatalf("invoke without native interruption: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback context did not observe cancellation")
	}
	if callbackErr != context.Canceled || !deadlineOK {
		t.Fatalf("callback context = %v, deadline %v; want canceled with parent deadline", callbackErr, deadlineOK)
	}
}

func TestCallerResolverInvocationContextBackgroundEntries(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *Runtime)
	}{
		{"raw invoke", func(t *testing.T, rt *Runtime) {
			module, err := rt.Compile(invocationContextImportModule("outer"))
			if err != nil {
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), module)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("call"); err != nil {
				t.Fatal(err)
			}
		}},
		{"prepared invoke", func(t *testing.T, rt *Runtime) {
			module, err := rt.Compile(invocationContextImportModule("outer"))
			if err != nil {
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), module)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			prepared, err := in.PrepareFunction("call")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := prepared.Invoke0(); err != nil {
				t.Fatal(err)
			}
		}},
		{"imported start", func(t *testing.T, rt *Runtime) {
			module, err := rt.Compile(invocationContextImportedStartModule())
			if err != nil {
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), module)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := new(invocationContextTestState)
			rt := newInvocationContextTestRuntime(t, state)
			defer rt.Close()
			var callbackContext context.Context
			var callbackErr error
			var live, withoutDeadline bool
			state.outer = func(caller HostModule, _, _ []uint64) {
				callbackContext, callbackErr = state.resolver.InvocationContext(caller)
				if callbackErr == nil {
					live = callbackContext.Err() == nil
					_, hasDeadline := callbackContext.Deadline()
					withoutDeadline = !hasDeadline
				}
			}
			test.run(t, rt)
			if callbackErr != nil || callbackContext == nil || !live || !withoutDeadline {
				t.Fatalf("background callback context = %v, %v, live %v, without deadline %v", callbackContext, callbackErr, live, withoutDeadline)
			}
			if callbackContext.Err() != context.Canceled {
				t.Fatalf("background callback context after return = %v, want context.Canceled", callbackContext.Err())
			}
		})
	}
}

func TestCallbackInvocationContextPreservesDeadlineError(t *testing.T) {
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	ctx := newCallbackInvocationContext(1, parent)
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("context error = %v, want context.DeadlineExceeded", ctx.Err())
	}
}

func TestCallerResolverInvocationContextReentryLifetimes(t *testing.T) {
	state := new(invocationContextTestState)
	rt := newInvocationContextTestRuntime(t, state)
	defer rt.Close()
	module, err := rt.Compile(invocationContextNestedModule())
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	outerDeadline := time.Now().Add(2 * time.Hour)
	nestedDeadline := time.Now().Add(time.Hour)
	outerParent, outerCancel := invocationContextTestParent(context.Background(), outerDeadline)
	defer outerCancel()
	nestedParent, nestedCancel := invocationContextTestParent(context.Background(), nestedDeadline)
	defer nestedCancel()
	var outerContext, nestedContext context.Context
	var nestedCallErr error
	var outerLiveAfterNested, nestedExpired, outerDeadlineOK, nestedDeadlineOK bool
	state.inner = func(caller HostModule, _, _ []uint64) {
		nestedContext, nestedCallErr = state.resolver.InvocationContext(caller)
		if nestedCallErr == nil {
			nestedDeadlineOK = invocationContextTestDeadline(nestedContext, nestedDeadline)
		}
	}
	state.outer = func(caller HostModule, _, _ []uint64) {
		outerContext, nestedCallErr = state.resolver.InvocationContext(caller)
		if nestedCallErr != nil {
			return
		}
		outerDeadlineOK = invocationContextTestDeadline(outerContext, outerDeadline)
		_, nestedCallErr = in.InvokeFromHost(nestedParent, caller, "nested")
		outerLiveAfterNested = outerContext.Err() == nil
		nestedExpired = nestedContext != nil && nestedContext.Err() == context.Canceled
	}
	if _, err := in.Call(outerParent, "call"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if nestedCallErr != nil || !outerLiveAfterNested || !nestedExpired || !outerDeadlineOK || !nestedDeadlineOK {
		t.Fatalf("reentry contexts = err %v outer-live %v nested-expired %v deadlines %v/%v", nestedCallErr, outerLiveAfterNested, nestedExpired, outerDeadlineOK, nestedDeadlineOK)
	}
	if outerContext.Err() != context.Canceled {
		t.Fatalf("outer context after return = %v, want context.Canceled", outerContext.Err())
	}
}

func TestCallerResolverInvocationContextEntryPaths(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *Runtime, *invocationContextTestState, context.Context)
	}{
		{"direct host reexport", func(t *testing.T, rt *Runtime, _ *invocationContextTestState, parent context.Context) {
			module, err := rt.Compile(invocationContextReexportModule())
			if err != nil {
				t.Fatal(err)
			}
			in, err := rt.Instantiate(context.Background(), module)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err := in.InvokeContext(parent, "call"); err != nil {
				t.Fatal(err)
			}
		}},
		{"cross instance", func(t *testing.T, rt *Runtime, _ *invocationContextTestState, parent context.Context) {
			producerModule, err := rt.Compile(invocationContextImportModule("outer"))
			if err != nil {
				t.Fatal(err)
			}
			producer, err := rt.Instantiate(context.Background(), producerModule)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			target, err := producer.ExportedFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			consumerModule, err := rt.Compile(invocationContextImportModule("next"))
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(Imports{"env.next": target}))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if _, err := consumer.Call(parent, "call"); err != nil {
				t.Fatal(err)
			}
		}},
		{"managed table", func(t *testing.T, rt *Runtime, state *invocationContextTestState, parent context.Context) {
			module, err := rt.Compile(invocationContextManagedTableModule())
			if err != nil {
				t.Fatal(err)
			}
			managed, err := state.manager.Instantiate(context.Background(), module)
			if err != nil {
				t.Fatal(err)
			}
			defer managed.Close()
			if err := managed.InvokeVoidTable(parent, 0); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := new(invocationContextTestState)
			rt := newInvocationContextTestRuntime(t, state)
			defer rt.Close()
			deadline := time.Now().Add(time.Hour)
			parent, cancel := invocationContextTestParent(context.Background(), deadline)
			defer cancel()
			var callbackContext context.Context
			var callbackErr error
			var deadlineOK bool
			state.outer = func(caller HostModule, _, _ []uint64) {
				callbackContext, callbackErr = state.resolver.InvocationContext(caller)
				if callbackErr == nil {
					deadlineOK = invocationContextTestDeadline(callbackContext, deadline)
				}
			}
			test.run(t, rt, state, parent)
			if callbackErr != nil || callbackContext == nil || !deadlineOK {
				t.Fatalf("callback context = %v, %v, deadline %v", callbackContext, callbackErr, deadlineOK)
			}
			if callbackContext.Err() != context.Canceled {
				t.Fatalf("callback context after entry return = %v, want context.Canceled", callbackContext.Err())
			}
		})
	}
}

func TestCallerResolverInvocationContextDoesNotReplaceCallerWatcher(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	resolver := &CallerResolver{}
	resolver.activate(rt)
	defer resolver.close()
	manager := newPendingInstanceManager("test.invocation-context", AuthorityScope{})
	manager.activate(rt)
	in := &Instance{rt: rt}
	managed := &ManagedInstance{manager: manager, value: in}
	manager.byInstance[in] = managed
	caller := in.beginHostCallScope()
	wake, stop, err := manager.WatchCaller(caller)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	ctx, err := resolver.InvocationContext(caller)
	if err != nil {
		t.Fatal(err)
	}
	caller.scope.end(caller.generation, caller.parentGeneration)
	select {
	case <-wake:
	default:
		t.Fatal("caller watcher was not signaled")
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("invocation context error = %v, want context.Canceled", ctx.Err())
	}
}

func TestCallerResolverInvocationContextExitRace(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	resolver := &CallerResolver{}
	resolver.activate(rt)
	defer resolver.close()
	in := &Instance{rt: rt}
	for i := 0; i < 1000; i++ {
		caller := in.beginHostCallScope()
		result := make(chan context.Context, 1)
		go func() {
			ctx, _ := resolver.InvocationContext(caller)
			result <- ctx
		}()
		caller.scope.end(caller.generation, caller.parentGeneration)
		if ctx := <-result; ctx != nil {
			select {
			case <-ctx.Done():
			default:
				t.Fatal("context acquired during callback exit remained live")
			}
		}
	}
}

var invocationContextBenchmarkSink context.Context

func BenchmarkCallerResolverInvocationContext(b *testing.B) {
	rt := NewRuntime()
	defer rt.Close()
	resolver := &CallerResolver{}
	resolver.activate(rt)
	defer resolver.close()
	in := &Instance{rt: rt}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		caller := in.beginHostCallScope()
		ctx, err := resolver.InvocationContext(caller)
		if err != nil {
			b.Fatal(err)
		}
		invocationContextBenchmarkSink = ctx
		caller.scope.end(caller.generation, caller.parentGeneration)
	}
	b.ReportMetric(float64(unsafe.Sizeof(hostCallScope{})), "scope-B")
	b.ReportMetric(float64(unsafe.Sizeof(instancePluginState{})), "plugin-state-B")
}
