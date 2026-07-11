package wago

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// hookExt is an extension that registers invoke/compile hooks driven by callbacks,
// so a test can observe when they fire.
type hookExt struct {
	afterCompile      func(*Module)
	beforeInstantiate func(*InstantiateContext) error
	afterInstantiate  func(*InstantiateContext, *Instance) error
	onInstantiateErr  func(*InstantiateContext, error)
	beforeClose       func(*InstanceContext)
	afterClose        func(*InstanceContext)
	beforeInvoke      func(*InvokeContext) error
	afterInvoke       func(*InvokeContext, []Value, error)
}

func (hookExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.hooks", Version: "1.0.0", Stability: Stable}
}

func (e hookExt) Register(reg *Registry) error {
	// Provide env.f so callsEnvF modules instantiate.
	reg.ImportModule("env").
		Func("f", func(_ HostModule, p, r []uint64) { r[0] = p[0] }).
		Params(ValI32).Results(ValI32)
	if e.afterCompile != nil {
		reg.Hooks().AfterCompile(func(_ *CompileContext, m *Module) error { e.afterCompile(m); return nil })
	}
	if e.beforeInstantiate != nil {
		reg.Hooks().BeforeInstantiate(e.beforeInstantiate)
	}
	if e.afterInstantiate != nil {
		reg.Hooks().AfterInstantiate(e.afterInstantiate)
	}
	if e.onInstantiateErr != nil {
		reg.Hooks().OnInstantiateError(e.onInstantiateErr)
	}
	if e.beforeClose != nil {
		reg.Hooks().BeforeClose(e.beforeClose)
	}
	if e.afterClose != nil {
		reg.Hooks().AfterClose(e.afterClose)
	}
	if e.beforeInvoke != nil {
		reg.Hooks().BeforeInvoke(e.beforeInvoke)
	}
	if e.afterInvoke != nil {
		reg.Hooks().AfterInvoke(e.afterInvoke)
	}
	return nil
}

func TestInvokeHooksFire(t *testing.T) {
	var before, after int
	var sawExport string
	var sawResult int32
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		beforeInvoke: func(ic *InvokeContext) error { before++; sawExport = ic.Export; return nil },
		afterInvoke: func(_ *InvokeContext, out []Value, _ error) {
			after++
			if len(out) == 1 {
				sawResult = out[0].I32()
			}
		},
	}); err != nil {
		t.Fatalf("use: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	if _, err := in.Call(context.Background(), "g", ValueI32(41)); err != nil {
		t.Fatalf("call: %v", err)
	}
	if before != 1 || after != 1 {
		t.Fatalf("hooks fired before=%d after=%d, want 1/1", before, after)
	}
	if sawExport != "g" {
		t.Fatalf("BeforeInvoke saw export %q, want g", sawExport)
	}
	if sawResult != 41 {
		t.Fatalf("AfterInvoke saw result %d, want 41", sawResult)
	}
}

func TestBeforeInvokeVetoAbortsCall(t *testing.T) {
	afterErr := "none"
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		beforeInvoke: func(_ *InvokeContext) error { return fmt.Errorf("denied") },
		afterInvoke: func(_ *InvokeContext, out []Value, err error) {
			if err != nil {
				afterErr = err.Error()
			}
		},
	}); err != nil {
		t.Fatalf("use: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	_, err = in.Call(context.Background(), "g", ValueI32(1))
	if err == nil || err.Error() != "denied" {
		t.Fatalf("Call error = %v, want denied", err)
	}
	if afterErr != "denied" {
		t.Fatalf("AfterInvoke saw err %q, want denied", afterErr)
	}
}

type closeHookExt struct {
	id   string
	hook func(*InstanceContext)
}

func (e closeHookExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: e.id, Version: "1.0.0", Stability: Stable}
}

func (e closeHookExt) Register(reg *Registry) error {
	reg.Hooks().BeforeClose(e.hook)
	return nil
}

func TestInstanceBeforeCloseHooksRunOnceInReverseOrder(t *testing.T) {
	var events []string
	rt := NewRuntime()
	for _, ext := range []closeHookExt{
		{id: "test.close.first", hook: func(ic *InstanceContext) {
			if ic.Runtime != rt || ic.Compiled == nil || ic.Instance == nil {
				t.Fatalf("bad close context: %+v", ic)
			}
			if len(ic.Instance.Memory().Bytes()) == 0 {
				t.Fatal("linear memory was invalid before close hook")
			}
			events = append(events, "first")
		}},
		{id: "test.close.second", hook: func(*InstanceContext) { events = append(events, "second") }},
	} {
		if err := rt.Use(ext); err != nil {
			t.Fatalf("use %s: %v", ext.id, err)
		}
	}
	mod, err := rt.Compile(memprogWasm)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	want := []string{"second", "first"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("close events = %v, want %v", events, want)
	}
}

func TestInstanceCloseLifecycleRunsReverseAndIsolatesPanics(t *testing.T) {
	var events []string
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		beforeClose: func(ctx *InstanceContext) {
			events = append(events, "first-before")
			ctx.Metadata["before"] = true
		},
		afterClose: func(ctx *InstanceContext) {
			if ctx.Metadata["before"] != true {
				t.Fatal("AfterClose did not receive BeforeClose metadata")
			}
			events = append(events, "first-after")
		},
	}); err != nil {
		t.Fatalf("use first: %v", err)
	}
	if err := rt.Use(closeLifecycleExt{
		id: "test.close.panic",
		before: func(*InstanceContext) {
			events = append(events, "second-before")
			panic("before boom")
		},
		after: func(*InstanceContext) { events = append(events, "second-after") },
	}); err != nil {
		t.Fatalf("use second: %v", err)
	}
	mod, err := rt.Compile(memprogWasm)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if err := in.Close(); err == nil || !strings.Contains(err.Error(), "before boom") {
		t.Fatalf("Close error = %v, want isolated hook panic", err)
	}
	in.lifeMu.Lock()
	resourcesClosed := in.resourcesClosed
	in.lifeMu.Unlock()
	if !resourcesClosed {
		t.Fatal("hook panic prevented instance resource release")
	}
	want := []string{"second-before", "first-before", "second-after", "first-after"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("close lifecycle = %v, want %v", events, want)
	}
}

type closeLifecycleExt struct {
	id     string
	before func(*InstanceContext)
	after  func(*InstanceContext)
}

func (e closeLifecycleExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: e.id, Version: "1.0.0", Stability: Stable}
}

func (e closeLifecycleExt) Register(reg *Registry) error {
	reg.Hooks().BeforeClose(e.before)
	reg.Hooks().AfterClose(e.after)
	return nil
}

func TestInstanceBeforeCloseConcurrentCloseOnce(t *testing.T) {
	var fired atomic.Int32
	rt := NewRuntime()
	if err := rt.Use(closeHookExt{id: "test.close.concurrent", hook: func(*InstanceContext) { fired.Add(1) }}); err != nil {
		t.Fatalf("use: %v", err)
	}
	mod, err := rt.Compile(memprogWasm)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = in.Close()
		}()
	}
	wg.Wait()
	if got := fired.Load(); got != 1 {
		t.Fatalf("BeforeClose fired %d times, want 1", got)
	}
}

func TestInstanceBeforeCloseRunsAfterInstantiateFailure(t *testing.T) {
	fired := 0
	setupErr := errors.New("post-instantiate setup failed")
	var observed error
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		afterInstantiate: func(*InstantiateContext, *Instance) error { return setupErr },
		onInstantiateErr: func(ctx *InstantiateContext, err error) {
			if ctx.Origin != InstantiateDirect {
				t.Fatalf("instantiate error origin = %v, want direct", ctx.Origin)
			}
			observed = err
		},
		beforeClose: func(*InstanceContext) { fired++ },
	}); err != nil {
		t.Fatalf("use: %v", err)
	}
	if _, err := rt.Instantiate(context.Background(), callsEnvF(t, rt)); !errors.Is(err, setupErr) {
		t.Fatalf("instantiate error = %v, want %v", err, setupErr)
	}
	if observed != setupErr {
		t.Fatalf("OnInstantiateError saw %v, want %v", observed, setupErr)
	}
	if fired != 1 {
		t.Fatalf("BeforeClose fired %d times, want 1", fired)
	}
}

func TestInstantiateErrorObserverCannotSuppressFailure(t *testing.T) {
	veto := errors.New("instantiate denied")
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		beforeInstantiate: func(*InstantiateContext) error { return veto },
		onInstantiateErr:  func(*InstantiateContext, error) { panic("observer boom") },
	}); err != nil {
		t.Fatalf("use: %v", err)
	}
	_, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if !errors.Is(err, veto) || !strings.Contains(err.Error(), "observer boom") {
		t.Fatalf("Instantiate error = %v, want veto plus observer panic", err)
	}
}

func TestLifecycleOriginPropagatesWithoutChangingLowLevelAPI(t *testing.T) {
	var instantiateOrigin, closeOrigin InstantiateOrigin
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		beforeInstantiate: func(ctx *InstantiateContext) error {
			instantiateOrigin = ctx.Origin
			return nil
		},
		beforeClose: func(ctx *InstanceContext) { closeOrigin = ctx.Origin },
	}); err != nil {
		t.Fatalf("use: %v", err)
	}
	mod := callsEnvF(t, rt)
	in, err := rt.instantiateWithHooksOrigin(mod, rt.HostImports(), GCConfig{}, false, InstantiateWorker)
	if err != nil {
		t.Fatalf("worker-origin instantiate: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if instantiateOrigin != InstantiateWorker || closeOrigin != InstantiateWorker {
		t.Fatalf("origins = instantiate %v close %v, want worker", instantiateOrigin, closeOrigin)
	}
}

func TestInstanceBeforeCloseRunsOnClassRelease(t *testing.T) {
	fired := 0
	rt := NewRuntime()
	if err := rt.Use(closeHookExt{id: "test.close.class", hook: func(*InstanceContext) { fired++ }}); err != nil {
		t.Fatalf("use: %v", err)
	}
	class, err := rt.Class(counterModule(t), ClassOptions{Pool: PoolOptions{MaxInstances: 1, Reset: ResetReinstantiate}})
	if err != nil {
		t.Fatalf("class: %v", err)
	}
	lease, err := class.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if fired != 1 {
		t.Fatalf("BeforeClose after release = %d, want 1", fired)
	}
	if err := class.Close(); err != nil {
		t.Fatalf("class close: %v", err)
	}
	if fired != 2 {
		t.Fatalf("BeforeClose after class close = %d, want 2", fired)
	}
}

func TestLowLevelInstanceCloseSkipsHooks(t *testing.T) {
	fired := 0
	rt := NewRuntime()
	if err := rt.Use(closeHookExt{id: "test.close.low-level", hook: func(*InstanceContext) { fired++ }}); err != nil {
		t.Fatalf("use: %v", err)
	}
	mod, err := rt.Compile(memprogWasm)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(mod.Compiled(), InstantiateOptions{})
	if err != nil {
		t.Fatalf("low-level instantiate: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if fired != 0 {
		t.Fatalf("low-level Close fired %d hooks, want 0", fired)
	}
}

type rejectedCloseHookExt struct{ fired *int }

func (rejectedCloseHookExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.close.rejected", Version: "1.0.0", Stability: Stable}
}

func (e rejectedCloseHookExt) Register(reg *Registry) error {
	reg.Hooks().BeforeClose(func(*InstanceContext) { *e.fired++ })
	reg.ImportModule("env").Func("other", func(HostModule, []uint64, []uint64) {})
	return nil
}

func TestRejectedExtensionDoesNotLeakCloseHooks(t *testing.T) {
	fired := 0
	rt := NewRuntime()
	if err := rt.Use(hookExt{}); err != nil {
		t.Fatalf("use owner: %v", err)
	}
	if err := rt.Use(rejectedCloseHookExt{fired: &fired}); !errors.Is(err, ErrExtensionConflict) {
		t.Fatalf("use rejected extension = %v, want ErrExtensionConflict", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	_ = in.Close()
	if fired != 0 {
		t.Fatalf("rejected extension leaked %d close hooks", fired)
	}
}

func TestAfterCompileHookFires(t *testing.T) {
	fired := 0
	var gotExports []string
	rt := NewRuntime()
	if err := rt.Use(hookExt{afterCompile: func(m *Module) { fired++; gotExports = m.Exports() }}); err != nil {
		t.Fatalf("use: %v", err)
	}
	_ = callsEnvF(t, rt) // rt.Compile triggers AfterCompile
	if fired != 1 {
		t.Fatalf("AfterCompile fired %d times, want 1", fired)
	}
	if len(gotExports) != 1 || gotExports[0] != "g" {
		t.Fatalf("AfterCompile module exports = %v, want [g]", gotExports)
	}
}

// TestLowLevelInvokeSkipsHooks confirms the low-level Invoke path is hook-free.
func TestLowLevelInvokeSkipsHooks(t *testing.T) {
	rt := NewRuntime()
	fired := 0
	if err := rt.Use(hookExt{beforeInvoke: func(_ *InvokeContext) error { fired++; return nil }}); err != nil {
		t.Fatalf("use: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	// Low-level Invoke (untyped) must not fire the runtime's invoke hooks.
	if _, err := in.Invoke("g", I32(3)); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if fired != 0 {
		t.Fatalf("low-level Invoke fired %d invoke hooks, want 0", fired)
	}
}
