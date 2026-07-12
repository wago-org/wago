package wago

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type disposalTestPlugin struct {
	id          string
	requires    []PluginCapability
	hostFn      HostFunc
	hostName    string
	hostParams  []ValType
	hostResults []ValType
	resolver    *CallerResolver
	manager     *InstanceManager
	afterInst   []func(*InstantiateContext, *Instance) error
	onInstErr   []func(*InstantiateContext, error)
	beforeClose []func(*InstanceContext)
	afterClose  []func(*InstanceContext)
	afterInvoke []func(*InvokeContext, []Value, error)
}

func (p *disposalTestPlugin) Info() ExtensionInfo {
	return ExtensionInfo{ID: p.id, Version: "1.0.0", Stability: Stable, RequiresCapabilities: p.requires}
}

func (p *disposalTestPlugin) Register(reg *Registry) error {
	if p.hostFn != nil {
		host, err := reg.HostImports()
		if err != nil {
			return err
		}
		p.resolver = host.CallerResolver()
		name := p.hostName
		if name == "" {
			name = "f"
		}
		host.Module("env").Func(name, p.hostFn).Params(p.hostParams...).Results(p.hostResults...)
	}
	if len(p.afterInst)+len(p.onInstErr)+len(p.beforeClose)+len(p.afterClose) != 0 {
		lifecycle, err := reg.InstanceLifecycle()
		if err != nil {
			return err
		}
		lifecycle.AfterInstantiate(p.afterInst...)
		lifecycle.OnInstantiateError(p.onInstErr...)
		lifecycle.BeforeClose(p.beforeClose...)
		lifecycle.AfterClose(p.afterClose...)
	}
	if len(p.afterInvoke) != 0 {
		invocation, err := reg.InstanceInvocation()
		if err != nil {
			return err
		}
		invocation.After(p.afterInvoke...)
	}
	if hasPluginCapability(p.requires, PluginManagedInstances) {
		var err error
		p.manager, err = reg.ManagedInstances()
		if err != nil {
			return err
		}
	}
	return nil
}

func hasPluginCapability(caps []PluginCapability, want PluginCapability) bool {
	for _, cap := range caps {
		if cap == want {
			return true
		}
	}
	return false
}

func voidImportCallModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func failingLocalStartModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(8, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x1a, 0x00, 0x0b}))),
	)
}

func trapExportModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("boom", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))),
	)
}

func TestExactInstanceLifecycleDirect(t *testing.T) {
	var afterInstance, beforeInstance, afterCloseInstance *Instance
	var beforeCount, afterCount int
	p := &disposalTestPlugin{
		id:       "test.lifecycle.direct",
		requires: []PluginCapability{PluginInstanceHooks},
		afterInst: []func(*InstantiateContext, *Instance) error{func(ctx *InstantiateContext, in *Instance) error {
			if ctx.Origin != InstantiateDirect {
				t.Fatalf("AfterInstantiate origin = %v, want direct", ctx.Origin)
			}
			afterInstance = in
			return nil
		}},
		beforeClose: []func(*InstanceContext){func(ctx *InstanceContext) {
			beforeCount++
			beforeInstance = ctx.Instance
			if ctx.Origin != InstantiateDirect || ctx.Runtime == nil || ctx.Compiled == nil {
				t.Fatalf("BeforeClose context = %+v", ctx)
			}
			ctx.Metadata["shared"] = "yes"
		}},
		afterClose: []func(*InstanceContext){func(ctx *InstanceContext) {
			afterCount++
			afterCloseInstance = ctx.Instance
			if ctx.Metadata["shared"] != "yes" {
				t.Fatalf("close metadata was not shared: %v", ctx.Metadata)
			}
		}},
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginInstanceHooks)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if afterInstance != in {
		t.Fatalf("AfterInstantiate instance = %p, want %p", afterInstance, in)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if beforeInstance != in || afterCloseInstance != in {
		t.Fatalf("close pointers = %p/%p, want %p", beforeInstance, afterCloseInstance, in)
	}
	if beforeCount != 1 || afterCount != 1 {
		t.Fatalf("close hook counts = %d/%d, want 1/1", beforeCount, afterCount)
	}
}

func TestExactInstanceLifecycleManagedExplicitAndRuntimeClose(t *testing.T) {
	var mu sync.Mutex
	origins := map[*Instance]InstantiateOrigin{}
	closed := map[*Instance]int{}
	p := &disposalTestPlugin{
		id:       "test.lifecycle.managed",
		requires: []PluginCapability{PluginInstanceHooks, PluginManagedInstances},
		afterInst: []func(*InstantiateContext, *Instance) error{func(ctx *InstantiateContext, in *Instance) error {
			mu.Lock()
			origins[in] = ctx.Origin
			mu.Unlock()
			return nil
		}},
		beforeClose: []func(*InstanceContext){func(ctx *InstanceContext) {
			mu.Lock()
			closed[ctx.Instance]++
			if ctx.Origin != InstantiateManaged {
				t.Errorf("managed close origin = %v, want managed", ctx.Origin)
			}
			mu.Unlock()
		}},
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginInstanceHooks, PluginManagedInstances)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	first, err := p.manager.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("managed Instantiate: %v", err)
	}
	firstIn := first.Instance()
	if origins[firstIn] != InstantiateManaged {
		t.Fatalf("managed AfterInstantiate origin = %v", origins[firstIn])
	}
	if err := first.Close(); err != nil {
		t.Fatalf("managed Close: %v", err)
	}
	second, err := p.manager.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("second managed Instantiate: %v", err)
	}
	secondIn := second.Instance()
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	if second.Instance() != nil {
		t.Fatal("runtime close retained managed instance")
	}
	mu.Lock()
	defer mu.Unlock()
	if closed[firstIn] != 1 || closed[secondIn] != 1 {
		t.Fatalf("managed close counts = %d/%d, want 1/1", closed[firstIn], closed[secondIn])
	}
}

func TestConcurrentInstanceCloseWaitsAndRunsOnce(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	closePanic := errors.New("concurrent close hook panic")
	var before, after atomic.Int32
	p := &disposalTestPlugin{
		id:       "test.lifecycle.concurrent-close",
		requires: []PluginCapability{PluginInstanceHooks},
		beforeClose: []func(*InstanceContext){func(*InstanceContext) {
			before.Add(1)
			close(entered)
			<-release
			panic(closePanic)
		}},
		afterClose: []func(*InstanceContext){func(*InstanceContext) { after.Add(1) }},
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginInstanceHooks)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, _ := rt.Compile(wasmtest.Module())
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	const callers = 32
	start := make(chan struct{})
	results := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			results <- in.Close()
		}()
	}
	close(start)
	<-entered
	select {
	case err := <-results:
		t.Fatalf("Close returned before lifecycle completion: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	var first error
	for i := 0; i < callers; i++ {
		err := <-results
		if !errors.Is(err, closePanic) {
			t.Fatalf("Close[%d] = %v, want close hook panic", i, err)
		}
		if i == 0 {
			first = err
		} else if err != first {
			t.Fatalf("Close[%d] result identity differs: %v != %v", i, err, first)
		}
	}
	if before.Load() != 1 || after.Load() != 1 {
		t.Fatalf("hook counts = %d/%d, want 1/1", before.Load(), after.Load())
	}
}

func TestPanickingCloseHookDoesNotSkipCleanup(t *testing.T) {
	panicErr := errors.New("close hook exploded")
	var mu sync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	p := &disposalTestPlugin{
		id:       "test.lifecycle.panic-close",
		requires: []PluginCapability{PluginInstanceHooks},
		beforeClose: []func(*InstanceContext){
			func(*InstanceContext) { record("before-first") },
			func(*InstanceContext) { record("before-panic"); panic(panicErr) },
			func(*InstanceContext) { record("before-last") },
		},
		afterClose: []func(*InstanceContext){
			func(*InstanceContext) { record("after-first") },
			func(*InstanceContext) { record("after-last") },
		},
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginInstanceHooks)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, _ := rt.Compile(wasmtest.Module())
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	closeErr := in.Close()
	if !errors.Is(closeErr, panicErr) {
		t.Fatalf("Close error = %v, want panic error", closeErr)
	}
	if second := in.Close(); second != closeErr {
		t.Fatalf("second Close error identity = %v, want same %v", second, closeErr)
	}
	in.lifeMu.Lock()
	closed, resourcesClosed := in.closed, in.resourcesClosed
	in.lifeMu.Unlock()
	if !closed || !resourcesClosed {
		t.Fatalf("internal cleanup closed=%v resourcesClosed=%v, want true/true", closed, resourcesClosed)
	}
	want := "[before-last before-panic before-first after-last after-first]"
	if got := fmt.Sprint(events); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
}

func TestFailedImportedStartResolvesAndClosesExactInstance(t *testing.T) {
	var mu sync.Mutex
	state := map[*Instance]bool{}
	var resolved, closed *Instance
	var observed error
	p := &disposalTestPlugin{
		id:       "test.lifecycle.imported-start",
		requires: []PluginCapability{PluginHostImports, PluginInstanceHooks},
		hostName: "start",
		onInstErr: []func(*InstantiateContext, error){func(_ *InstantiateContext, err error) {
			observed = err
		}},
		beforeClose: []func(*InstanceContext){func(ctx *InstanceContext) {
			closed = ctx.Instance
			mu.Lock()
			delete(state, ctx.Instance)
			mu.Unlock()
		}},
	}
	p.hostFn = func(caller HostModule, _, _ []uint64) {
		in, err := p.resolver.Resolve(caller)
		if err != nil {
			panic(err)
		}
		resolved = in
		mu.Lock()
		state[in] = true
		mu.Unlock()
		panic(HostExit{Code: 17})
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginHostImports, PluginInstanceHooks)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(importedStartModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, err = rt.Instantiate(context.Background(), mod)
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 17 {
		t.Fatalf("Instantiate error = %v, want ExitError(17)", err)
	}
	if observed == nil || !errors.As(observed, &exit) {
		t.Fatalf("OnInstantiateError = %v, want original exit", observed)
	}
	if resolved == nil || closed != resolved {
		t.Fatalf("resolved/closed instances = %p/%p", resolved, closed)
	}
	mu.Lock()
	remaining := len(state)
	mu.Unlock()
	if remaining != 0 {
		t.Fatalf("plugin state retained %d failed-start instance(s)", remaining)
	}
}

func TestFailedLocalStartResolvesAndClosesExactInstance(t *testing.T) {
	var resolved, closed *Instance
	state := map[*Instance]struct{}{}
	p := &disposalTestPlugin{
		id:          "test.lifecycle.local-start",
		requires:    []PluginCapability{PluginHostImports, PluginInstanceHooks},
		hostResults: []ValType{ValI32},
		beforeClose: []func(*InstanceContext){func(ctx *InstanceContext) {
			closed = ctx.Instance
			delete(state, ctx.Instance)
		}},
	}
	p.hostFn = func(caller HostModule, _ []uint64, results []uint64) {
		var err error
		resolved, err = p.resolver.Resolve(caller)
		if err != nil {
			panic(err)
		}
		results[0] = I32(1)
		state[resolved] = struct{}{}
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginHostImports, PluginInstanceHooks)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, err := rt.Compile(failingLocalStartModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	_, err = rt.Instantiate(context.Background(), mod)
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != TrapUnreachable {
		t.Fatalf("Instantiate error = %v, want unreachable trap", err)
	}
	if resolved == nil || closed != resolved || len(state) != 0 {
		t.Fatalf("resolved=%p closed=%p state=%d", resolved, closed, len(state))
	}
}

func TestAfterInstantiateFailureClosesAndJoinsCleanupError(t *testing.T) {
	instantiateErr := errors.New("attach failed")
	cleanupErr := errors.New("detach panicked")
	var before, after int
	var observed error
	p := &disposalTestPlugin{
		id:       "test.lifecycle.after-instantiate-failure",
		requires: []PluginCapability{PluginInstanceHooks},
		afterInst: []func(*InstantiateContext, *Instance) error{func(*InstantiateContext, *Instance) error {
			return instantiateErr
		}},
		onInstErr: []func(*InstantiateContext, error){func(_ *InstantiateContext, err error) { observed = err }},
		beforeClose: []func(*InstanceContext){func(*InstanceContext) {
			before++
			panic(cleanupErr)
		}},
		afterClose: []func(*InstanceContext){func(*InstanceContext) { after++ }},
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginInstanceHooks)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, _ := rt.Compile(wasmtest.Module())
	in, err := rt.Instantiate(context.Background(), mod)
	if in != nil {
		t.Fatalf("Instantiate returned failed instance %p", in)
	}
	if !errors.Is(err, instantiateErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Instantiate error = %v, want original and cleanup errors", err)
	}
	if !errors.Is(observed, instantiateErr) || !errors.Is(observed, cleanupErr) {
		t.Fatalf("OnInstantiateError = %v, want original and cleanup errors", observed)
	}
	if before != 1 || after != 1 {
		t.Fatalf("close hook counts = %d/%d, want 1/1", before, after)
	}
}

func TestPanickingOnInstantiateErrorPreservesFailureAndCleanup(t *testing.T) {
	instantiateErr := errors.New("attach failed")
	observerPanic := errors.New("observer panicked")
	var closed int
	p := &disposalTestPlugin{
		id:       "test.lifecycle.on-error-panic",
		requires: []PluginCapability{PluginInstanceHooks},
		afterInst: []func(*InstantiateContext, *Instance) error{func(*InstantiateContext, *Instance) error {
			return instantiateErr
		}},
		onInstErr:   []func(*InstantiateContext, error){func(*InstantiateContext, error) { panic(observerPanic) }},
		beforeClose: []func(*InstanceContext){func(*InstanceContext) { closed++ }},
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginInstanceHooks)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, _ := rt.Compile(wasmtest.Module())
	_, err := rt.Instantiate(context.Background(), mod)
	if !errors.Is(err, instantiateErr) {
		t.Fatalf("Instantiate error = %v, want original failure", err)
	}
	if !errors.Is(err, observerPanic) {
		t.Fatalf("Instantiate error = %v, want reported observer panic", err)
	}
	if closed != 1 {
		t.Fatalf("cleanup count = %d, want 1", closed)
	}
}

type forgedHostModule struct{}

func (forgedHostModule) Memory() []byte { return nil }

func TestCallerResolverAuthorityAndExpiry(t *testing.T) {
	var resolved *Instance
	var resolveErr error
	var retained HostModule
	p := &disposalTestPlugin{id: "test.caller.direct", requires: []PluginCapability{PluginHostImports}}
	p.hostFn = func(caller HostModule, _, _ []uint64) {
		retained = caller
		resolved, resolveErr = p.resolver.Resolve(caller)
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginHostImports)); err != nil {
		t.Fatalf("host.imports-only Use: %v", err)
	}
	mod, err := rt.Compile(voidImportCallModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Call(context.Background(), "call"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resolveErr != nil || resolved != in {
		t.Fatalf("Resolve = %p, %v; want %p, nil", resolved, resolveErr, in)
	}
	if _, err := p.resolver.Resolve(retained); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expired Resolve error = %v, want permission denied", err)
	}
	if _, err := p.resolver.Resolve(forgedHostModule{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("forged Resolve error = %v, want permission denied", err)
	}

	var crossErr error
	cross := &disposalTestPlugin{id: "test.caller.cross-runtime", requires: []PluginCapability{PluginHostImports}}
	cross.hostFn = func(caller HostModule, _, _ []uint64) {
		_, crossErr = p.resolver.Resolve(caller)
	}
	rt2 := NewRuntime()
	if err := rt2.Use(cross, WithPluginGrants(PluginHostImports)); err != nil {
		t.Fatalf("cross Use: %v", err)
	}
	mod2, _ := rt2.Compile(voidImportCallModule())
	in2, err := rt2.Instantiate(context.Background(), mod2)
	if err != nil {
		t.Fatalf("cross Instantiate: %v", err)
	}
	if _, err := in2.Call(context.Background(), "call"); err != nil {
		t.Fatalf("cross Call: %v", err)
	}
	_ = in2.Close()
	if !errors.Is(crossErr, ErrPermissionDenied) {
		t.Fatalf("cross-runtime Resolve error = %v, want permission denied", crossErr)
	}

	resolved, resolveErr = nil, nil
	low, err := Instantiate(mod.Compiled(), InstantiateOptions{Imports: rt.HostImports()})
	if err != nil {
		t.Fatalf("low-level Instantiate: %v", err)
	}
	if _, err := low.Invoke("call"); err != nil {
		t.Fatalf("low-level Invoke: %v", err)
	}
	_ = low.Close()
	if resolved != nil || !errors.Is(resolveErr, ErrPermissionDenied) {
		t.Fatalf("low-level Resolve = %p, %v; want nil, permission denied", resolved, resolveErr)
	}
}

func TestCallerResolverManagedInstance(t *testing.T) {
	var resolved *Instance
	var resolveErr error
	p := &disposalTestPlugin{
		id:       "test.caller.managed",
		requires: []PluginCapability{PluginHostImports, PluginManagedInstances},
	}
	p.hostFn = func(caller HostModule, _, _ []uint64) {
		resolved, resolveErr = p.resolver.Resolve(caller)
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginHostImports, PluginManagedInstances)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, _ := rt.Compile(voidImportCallModule())
	managed, err := p.manager.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("managed Instantiate: %v", err)
	}
	in := managed.Instance()
	if _, err := in.Call(context.Background(), "call"); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resolveErr != nil || resolved != in {
		t.Fatalf("Resolve = %p, %v; want %p, nil", resolved, resolveErr, in)
	}
	_ = managed.Close()
}

func TestManagedFailedStartUsesManagedOriginAndCleansUp(t *testing.T) {
	var resolved, closed *Instance
	p := &disposalTestPlugin{
		id:          "test.lifecycle.managed-failed-start",
		requires:    []PluginCapability{PluginHostImports, PluginInstanceHooks, PluginManagedInstances},
		hostResults: []ValType{ValI32},
		beforeClose: []func(*InstanceContext){func(ctx *InstanceContext) {
			closed = ctx.Instance
			if ctx.Origin != InstantiateManaged {
				t.Errorf("failed managed start origin = %v, want managed", ctx.Origin)
			}
		}},
	}
	p.hostFn = func(caller HostModule, _ []uint64, results []uint64) {
		var err error
		resolved, err = p.resolver.Resolve(caller)
		if err != nil {
			panic(err)
		}
		results[0] = I32(1)
	}
	rt := NewRuntime()
	grants := []PluginCapability{PluginHostImports, PluginInstanceHooks, PluginManagedInstances}
	if err := rt.Use(p, WithPluginGrants(grants...)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, _ := rt.Compile(failingLocalStartModule())
	managed, err := p.manager.Instantiate(context.Background(), mod)
	if managed != nil || err == nil {
		t.Fatalf("managed failed-start result = %v, %v", managed, err)
	}
	if resolved == nil || closed != resolved {
		t.Fatalf("resolved/closed = %p/%p", resolved, closed)
	}
}

func TestManagedForkLifecycleAndRuntimeOwnership(t *testing.T) {
	var child *ManagedInstance
	var childCreateErr error
	var mu sync.Mutex
	origin := map[*Instance]InstantiateOrigin{}
	closed := map[*Instance]int{}
	p := &disposalTestPlugin{
		id:       "test.lifecycle.managed-fork",
		requires: []PluginCapability{PluginHostImports, PluginInstanceHooks, PluginManagedInstances},
		afterInst: []func(*InstantiateContext, *Instance) error{func(ctx *InstantiateContext, in *Instance) error {
			mu.Lock()
			origin[in] = ctx.Origin
			mu.Unlock()
			return nil
		}},
		beforeClose: []func(*InstanceContext){func(ctx *InstanceContext) {
			mu.Lock()
			closed[ctx.Instance]++
			mu.Unlock()
		}},
	}
	p.hostFn = func(caller HostModule, _, _ []uint64) {
		child, childCreateErr = p.manager.Fork(context.Background(), caller)
	}
	rt := NewRuntime()
	grants := []PluginCapability{PluginHostImports, PluginInstanceHooks, PluginManagedInstances}
	if err := rt.Use(p, WithPluginGrants(grants...)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, _ := rt.Compile(voidImportCallModule())
	parent, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("parent Instantiate: %v", err)
	}
	if _, err := parent.Call(context.Background(), "call"); err != nil {
		t.Fatalf("parent Call: %v", err)
	}
	if childCreateErr != nil || child == nil || child.Instance() == nil {
		t.Fatalf("Fork = %v, %v", child, childCreateErr)
	}
	childIn := child.Instance()
	mu.Lock()
	childOrigin := origin[childIn]
	mu.Unlock()
	if childOrigin != InstantiateManaged {
		t.Fatalf("forked origin = %v, want managed", childOrigin)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Runtime.Close: %v", err)
	}
	if child.Instance() != nil {
		t.Fatal("runtime close retained forked managed instance")
	}
	mu.Lock()
	childClosed, parentClosed := closed[childIn], closed[parent]
	mu.Unlock()
	if childClosed != 1 || parentClosed != 0 {
		t.Fatalf("runtime close child/parent counts = %d/%d, want 1/0", childClosed, parentClosed)
	}
	if err := parent.Close(); err != nil {
		t.Fatalf("caller-owned parent Close: %v", err)
	}
}

func TestTrapReportsAfterInvokeButDoesNotClose(t *testing.T) {
	var invokeErr error
	var closed int
	p := &disposalTestPlugin{
		id:       "test.lifecycle.trap-not-close",
		requires: []PluginCapability{PluginInstanceHooks, PluginInvokeHooks},
		beforeClose: []func(*InstanceContext){func(*InstanceContext) {
			closed++
		}},
		afterInvoke: []func(*InvokeContext, []Value, error){func(_ *InvokeContext, _ []Value, err error) {
			invokeErr = err
		}},
	}
	rt := NewRuntime()
	if err := rt.Use(p, WithPluginGrants(PluginInstanceHooks, PluginInvokeHooks)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	mod, _ := rt.Compile(trapExportModule())
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if _, err := in.Call(context.Background(), "boom"); err == nil {
		t.Fatal("trapping Call returned nil error")
	}
	if invokeErr == nil {
		t.Fatal("AfterInvoke did not observe trap")
	}
	if closed != 0 {
		t.Fatalf("trap emitted %d close hook(s), want 0", closed)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed != 1 {
		t.Fatalf("explicit close emitted %d hook(s), want 1", closed)
	}
}
