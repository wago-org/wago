package wago

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

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

func TestInstanceLifecycleHooksFire(t *testing.T) {
	var events []string
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		beforeInstantiate: func(ic *InstantiateContext) error {
			events = append(events, "before-instantiate")
			if ic.Runtime != rt || ic.Module == nil || ic.Compiled != ic.Module.Compiled() {
				t.Fatalf("bad instantiate context: %+v", ic)
			}
			ic.Metadata["seen"] = true
			return nil
		},
		afterInstantiate: func(ic *InstantiateContext, in *Instance) error {
			events = append(events, "after-instantiate")
			if ic.Metadata["seen"] != true || in == nil {
				t.Fatalf("instantiate metadata/instance not carried through: %+v, %v", ic.Metadata, in)
			}
			return nil
		},
		beforeClose: func(ic *InstanceContext) {
			events = append(events, "before-close")
			if ic.Runtime != rt || ic.Compiled == nil || ic.Instance == nil {
				t.Fatalf("bad close context: %+v", ic)
			}
			ic.Metadata["closing"] = true
		},
		afterClose: func(ic *InstanceContext) {
			events = append(events, "after-close")
			if ic.Metadata["closing"] != true {
				t.Fatalf("close metadata not carried through: %+v", ic.Metadata)
			}
		},
	}); err != nil {
		t.Fatalf("use: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	want := []string{"before-instantiate", "after-instantiate", "before-close", "after-close"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}
}

func TestInstantiateErrorHookFires(t *testing.T) {
	veto := fmt.Errorf("instantiate denied")
	var got error
	rt := NewRuntime()
	if err := rt.Use(hookExt{
		beforeInstantiate: func(*InstantiateContext) error { return veto },
		onInstantiateErr:  func(_ *InstantiateContext, err error) { got = err },
	}); err != nil {
		t.Fatalf("use: %v", err)
	}
	_, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != veto {
		t.Fatalf("Instantiate error = %v, want %v", err, veto)
	}
	if got != veto {
		t.Fatalf("OnInstantiateError saw %v, want %v", got, veto)
	}
}

type rejectedHookExt struct{ fired *int }

func (rejectedHookExt) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.rejected-hooks", Version: "1.0.0", Stability: Stable}
}

func (e rejectedHookExt) Register(reg *Registry) error {
	reg.Hooks().AfterClose(func(*InstanceContext) { *e.fired++ })
	reg.ImportModule("env").
		Func("other", func(HostModule, []uint64, []uint64) {}).
		Params()
	return nil
}

func TestRejectedExtensionDoesNotLeakHooks(t *testing.T) {
	fired := 0
	rt := NewRuntime()
	if err := rt.Use(hookExt{}); err != nil {
		t.Fatalf("use owner: %v", err)
	}
	if err := rt.Use(rejectedHookExt{fired: &fired}); !errors.Is(err, ErrExtensionConflict) {
		t.Fatalf("Use rejected extension error = %v, want ErrExtensionConflict", err)
	}
	in, err := rt.Instantiate(context.Background(), callsEnvF(t, rt))
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	_ = in.Close()
	if fired != 0 {
		t.Fatalf("rejected extension leaked %d active hooks", fired)
	}
}

func TestLowLevelInstanceCloseSkipsHooks(t *testing.T) {
	fired := 0
	rt := NewRuntime()
	if err := rt.Use(hookExt{afterClose: func(*InstanceContext) { fired++ }}); err != nil {
		t.Fatalf("use: %v", err)
	}
	mod := callsEnvF(t, rt)
	in, err := Instantiate(mod.Compiled(), InstantiateOptions{Imports: rt.HostImports()})
	if err != nil {
		t.Fatalf("low-level instantiate: %v", err)
	}
	_ = in.Close()
	if fired != 0 {
		t.Fatalf("low-level Close fired %d hooks, want 0", fired)
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

func TestCapabilityAccessorsRegisterHooks(t *testing.T) {
	r := &Registry{hooks: &HookRegistry{}}
	host, err := r.HostImports()
	if err != nil || host.Module("env") == nil || host.CallerResolver() == nil {
		t.Fatalf("HostImports = %#v, %v", host, err)
	}
	runtimeHooks, err := r.RuntimeLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	runtimeHooks.OnClose(func(*RuntimeContext) {})
	compileHooks, err := r.ModuleCompiler()
	if err != nil {
		t.Fatal(err)
	}
	compileHooks.Before(func(*CompileContext, []byte) ([]byte, error) { return nil, nil })
	compileHooks.After(func(*CompileContext, *Module) error { return nil })
	instanceHooks, err := r.InstanceLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	instanceHooks.BeforeInstantiate(func(*InstantiateContext) error { return nil })
	instanceHooks.AfterInstantiate(func(*InstantiateContext, *Instance) error { return nil })
	instanceHooks.OnInstantiateError(func(*InstantiateContext, error) {})
	instanceHooks.BeforeClose(func(*InstanceContext) {})
	instanceHooks.AfterClose(func(*InstanceContext) {})
	invokeHooks, err := r.InstanceInvocation()
	if err != nil {
		t.Fatal(err)
	}
	invokeHooks.Before(func(*InvokeContext) error { return nil })
	invokeHooks.After(func(*InvokeContext, []Value, error) {})

	if len(r.used) != 5 || len(r.activate) != 1 || len(r.hooks.beforeCompile) != 1 || len(r.hooks.afterCompile) != 1 ||
		len(r.hooks.beforeInstantiate) != 1 || len(r.hooks.afterInstantiate) != 1 || len(r.hooks.onInstantiateError) != 1 ||
		len(r.hooks.beforeClose) != 1 || len(r.hooks.afterClose) != 1 || len(r.hooks.beforeInvoke) != 1 || len(r.hooks.afterInvoke) != 1 ||
		len(r.hooks.onRuntimeClose) != 1 {
		t.Fatal("capability accessors did not register all callbacks")
	}
}

func TestInstantiateOptionHelpers(t *testing.T) {
	c := instantiateConfig{}
	WithPolicy(Policy{MaxTableEntries: 3})(&c)
	WithImports(Imports{"env.f": 1})(&c)
	WithImports(Imports{"env.g": 2, "env.f": 3})(&c)
	WithGC(GCConfig{TinyHeapBytes: 64})(&c)
	if c.policy.MaxTableEntries != 3 || len(c.imports) != 2 || c.imports["env.f"] != 3 || c.imports["env.g"] != 2 || !c.hasGC || c.gc.TinyHeapBytes != 64 {
		t.Fatalf("instantiate config = %#v", c)
	}
}

func TestCapabilityAccessorsEnforceGrants(t *testing.T) {
	r := &Registry{hooks: &HookRegistry{}, grants: map[PluginCapability]struct{}{}}
	if _, err := r.HostImports(); err == nil {
		t.Fatal("ungranted host imports access succeeded")
	}
	if _, err := (*Registry)(nil).InstanceInvocation(); err == nil {
		t.Fatal("nil registry access succeeded")
	}
}

func TestAfterCloseBracketsLogicalCloseBeforePhysicalRelease(t *testing.T) {
	var before, after int
	var observedLogical, observedPhysical bool
	rt := NewRuntime()
	rt.hooks.beforeClose = append(rt.hooks.beforeClose, func(ctx *InstanceContext) {
		before++
		observedLogical = ctx.Instance.isLogicallyClosed()
	})
	rt.hooks.afterClose = append(rt.hooks.afterClose, func(ctx *InstanceContext) {
		after++
		ctx.Instance.lifeMu.Lock()
		observedPhysical = ctx.Instance.resourcesClosed
		ctx.Instance.lifeMu.Unlock()
	})
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	if !in.retainResourceRoot() {
		t.Fatal("retain test resource root")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if before != 1 || after != 1 || !observedLogical || observedPhysical {
		t.Fatalf("logical hook state: before=%d after=%d logical=%v physical=%v", before, after, observedLogical, observedPhysical)
	}
	if !in.hasPhysicalResources() {
		t.Fatal("retained root did not defer physical release")
	}
	in.releaseResourceRoot()
	if in.hasPhysicalResources() {
		t.Fatal("physical release did not run after final root release")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}
