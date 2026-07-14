package wago

import "testing"

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
