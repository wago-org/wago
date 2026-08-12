package wago

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestLoadPluginsIsOneShotBeforeRuntimeUse(t *testing.T) {
	def := testDefinition("example.com/load/once")
	provider := PluginProvider{Definition: def, New: func() Plugin { return pluginFunc(nil) }}
	set := testSet(t, provider)

	used := NewRuntime()
	if _, err := used.Compile(wasmtest.Module()); err != nil {
		t.Fatal(err)
	}
	if err := used.LoadPlugins(context.Background(), set); err == nil || !strings.Contains(err.Error(), "before the first runtime operation") {
		t.Fatalf("late LoadPlugins = %v", err)
	}

	failed := NewRuntime()
	broken := set
	broken.Selections = append([]PluginSelection(nil), set.Selections...)
	broken.Selections[0].DefinitionDigest = "sha256:wrong"
	if err := failed.LoadPlugins(context.Background(), broken); err == nil {
		t.Fatal("invalid first load succeeded")
	}
	if err := failed.LoadPlugins(context.Background(), set); err == nil || !strings.Contains(err.Error(), "at most once") {
		t.Fatalf("second load attempt = %v", err)
	}
}

func TestLoadPluginsExclusiveAndCloseWaitsForStart(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	def := testDefinition("example.com/load/exclusive")
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			return r.Lifecycle(PluginLifecycle{Start: func(context.Context) error {
				close(entered)
				<-release
				return nil
			}})
		})
	}}
	rt := NewRuntime()
	loadDone := make(chan error, 1)
	go func() { loadDone <- rt.LoadPlugins(context.Background(), testSet(t, provider)) }()
	<-entered
	if _, err := rt.Compile(wasmtest.Module()); err == nil || !strings.Contains(err.Error(), "plugins are loading") {
		t.Fatalf("Compile during Start = %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if rt.Closed() == nil {
		t.Fatal("Close did not publish shutdown while Start was active")
	}
	close(release)
	if err := <-loadDone; err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPluginStartCanUseGrantedCoreHandlesAndDependencies(t *testing.T) {
	spec := ContractSpec{ID: "example.com/start/dependency", Major: 1}
	providerDef := testDefinition("example.com/start/provider")
	providerDef.Provides = []ContractSpec{spec}
	consumerDef := testDefinition("example.com/start/consumer")
	consumerDef.Requires = []PluginRequirement{{ID: providerDef.ID, Version: "^1.0.0"}}
	consumerDef.Consumes = []ContractRequirement{{ID: spec.ID, Major: spec.Major, Mode: ContractRequired}}
	consumerDef.Authorities = []AuthorityRequest{
		{Name: AuthorityCoreModuleCompile, Mode: AuthorityRequired, Reason: "compile during startup"},
		{Name: AuthorityCoreInstanceInstantiate, Mode: AuthorityRequired, Reason: "instantiate during startup", Scope: AuthorityScope{MaxInstances: 1, MaxMemoryBytes: 65536}},
	}
	var providerStarted atomic.Bool
	provider := PluginProvider{Definition: providerDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			if err := ProvideContract(r, spec, "ready"); err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Start: func(context.Context) error {
				providerStarted.Store(true)
				return nil
			}})
		})
	}}
	var dependencyValue string
	consumer := PluginProvider{Definition: consumerDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			dependency, err := RequireContract(r, spec, ContractRequired, (*string)(nil))
			if err != nil {
				return err
			}
			compiler, err := r.CoreModuleCompiler()
			if err != nil {
				return err
			}
			instantiator, err := r.CoreInstanceInstantiator()
			if err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Start: func(ctx context.Context) error {
				if !providerStarted.Load() {
					return errors.New("dependency started out of order")
				}
				if err := dependency.Call(func(value any) error {
					dependencyValue = value.(string)
					return nil
				}); err != nil {
					return err
				}
				mod, err := compiler.Compile(wasmtest.Module())
				if err != nil {
					return err
				}
				compiled := mod.Compiled()
				owned, instantiateErr := instantiator.Instantiate(ctx, mod)
				var closeErr error
				if owned != nil {
					closeErr = owned.Close()
				}
				return errors.Join(instantiateErr, closeErr, mod.Close(), compiled.Close())
			}})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider, consumer)); err != nil {
		t.Fatal(err)
	}
	if dependencyValue != "ready" {
		t.Fatalf("dependency value during Start = %q", dependencyValue)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConsumerStopCanInvokeProviderManagedInstance(t *testing.T) {
	type service func(context.Context) (int32, error)
	spec := ContractSpec{ID: "example.com/managed/service", Major: 1}
	providerDef := testDefinition("example.com/managed/provider")
	providerDef.Provides = []ContractSpec{spec}
	providerDef.Authorities = []AuthorityRequest{
		{Name: AuthorityCoreModuleCompile, Mode: AuthorityRequired, Reason: "compile the provider worker"},
		{Name: AuthorityInstanceManage, Mode: AuthorityRequired, Reason: "own the provider worker", Scope: AuthorityScope{MaxInstances: 1, MaxMemoryBytes: 64 << 10}},
	}
	consumerDef := testDefinition("example.com/managed/consumer")
	consumerDef.Requires = []PluginRequirement{{ID: providerDef.ID, Version: "^1.0.0"}}
	consumerDef.Consumes = []ContractRequirement{{ID: spec.ID, Major: spec.Major, Mode: ContractRequired}}

	var managed *ManagedInstance
	provider := PluginProvider{Definition: providerDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			compiler, err := r.CoreModuleCompiler()
			if err != nil {
				return err
			}
			manager, err := r.ManagedInstances()
			if err != nil {
				return err
			}
			call := service(func(ctx context.Context) (int32, error) {
				in := managed.Instance()
				if in == nil {
					return 0, errors.New("provider worker closed before consumer Stop")
				}
				values, err := in.Call(ctx, "value")
				if err != nil {
					return 0, err
				}
				return values[0].I32(), nil
			})
			if err := ProvideContract(r, spec, call); err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Start: func(ctx context.Context) error {
				mod, err := compiler.Compile(wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
					wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("value", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0x0b}))),
				))
				if err != nil {
					return err
				}
				defer mod.Close()
				managed, err = manager.Instantiate(ctx, mod)
				return err
			}})
		})
	}}

	var ref *ContractRef
	var stoppedValue int32
	consumer := PluginProvider{Definition: consumerDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			var err error
			ref, err = RequireContract(r, spec, ContractRequired, (*service)(nil))
			if err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Stop: func(ctx context.Context) error {
				return ref.Call(func(value any) error {
					got, err := value.(service)(ctx)
					stoppedValue = got
					return err
				})
			}})
		})
	}}

	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider, consumer)); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stoppedValue != 7 {
		t.Fatalf("consumer Stop value = %d, want 7", stoppedValue)
	}
	if managed.Instance() != nil {
		t.Fatal("provider manager retained its worker after provider Stop")
	}
}

func TestStartFailureNeverPublishesCommittedPlan(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	def := testDefinition("example.com/start/fails-atomically")
	def.Authorities = []AuthorityRequest{{Name: AuthorityModuleCompileObserve, Mode: AuthorityRequired, Reason: "detect leaked plan"}}
	var observed atomic.Int32
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			observer, err := r.ModuleCompileObserver()
			if err != nil {
				return err
			}
			if err := observer.Observe(func(ModuleCompiledEvent) { observed.Add(1) }); err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Start: func(context.Context) error {
				close(entered)
				<-release
				return errors.New("start failed")
			}})
		})
	}}
	rt := NewRuntime()
	loadDone := make(chan error, 1)
	go func() { loadDone <- rt.LoadPlugins(context.Background(), testSet(t, provider)) }()
	<-entered
	compileDone := make(chan error, 1)
	go func() { _, err := rt.Compile(wasmtest.Module()); compileDone <- err }()
	close(release)
	if err := <-loadDone; err == nil || !strings.Contains(err.Error(), "start failed") {
		t.Fatalf("LoadPlugins = %v", err)
	}
	if err := <-compileDone; err == nil || !strings.Contains(err.Error(), "plugins are loading") && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("waiting Compile observed failed plan: %v", err)
	}
	if observed.Load() != 0 {
		t.Fatalf("failed plan observed %d public compiles", observed.Load())
	}
}

func TestRuntimeCloseCompletionPublishesSameError(t *testing.T) {
	stopErr := errors.New("same close result")
	entered, release := make(chan struct{}), make(chan struct{})
	def := testDefinition("example.com/close/join")
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				close(entered)
				<-release
				return stopErr
			}})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { first <- rt.Close() }()
	<-entered
	if err := rt.Close(); err != nil {
		t.Fatalf("concurrent Close = %v", err)
	}
	done := rt.Closed()
	if done == nil {
		t.Fatal("concurrent Close did not expose completion")
	}
	select {
	case <-done:
		t.Fatal("shutdown completed before Stop returned")
	default:
	}
	close(release)
	err1 := <-first
	err2 := rt.WaitClosed(context.Background())
	if err1 != nil || !errors.Is(err2, stopErr) {
		t.Fatalf("Close/WaitClosed results = %v / %v", err1, err2)
	}
}

func TestRuntimeCloseObserverAndInstancesPrecedePluginStop(t *testing.T) {
	var mu sync.Mutex
	var events []string
	def := testDefinition("example.com/close/order")
	def.Authorities = []AuthorityRequest{
		{Name: AuthorityRuntimeCloseObserve, Mode: AuthorityRequired, Reason: "observe shutdown"},
		{Name: AuthorityInstanceCloseObserve, Mode: AuthorityRequired, Reason: "observe instance close"},
	}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			shutdown, _ := r.RuntimeCloseObserver()
			if err := shutdown.Observe(func(RuntimeCloseEvent) { mu.Lock(); events = append(events, "runtime"); mu.Unlock() }); err != nil {
				return err
			}
			closed, _ := r.InstanceCloseObserver()
			if err := closed.After(func(InstanceCloseEvent) { mu.Lock(); events = append(events, "instance"); mu.Unlock() }); err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				mu.Lock()
				events = append(events, "stop")
				mu.Unlock()
				return nil
			}})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"runtime", "instance", "stop"}) {
		t.Fatalf("events = %v", events)
	}
	if err := in.Close(); err != nil {
		t.Fatalf("caller's idempotent Close = %v", err)
	}
	if _, err := in.Invoke("missing"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("runtime-closed instance invoke = %v", err)
	}
}

func TestRuntimeCloseClosesInstancesInReverseCreationOrder(t *testing.T) {
	var mu sync.Mutex
	var closed []InstanceIdentity
	def := testDefinition("example.com/close/instance-order")
	def.Authorities = []AuthorityRequest{{Name: AuthorityInstanceCloseObserve, Mode: AuthorityRequired, Reason: "record order"}}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			observer, err := r.InstanceCloseObserver()
			if err != nil {
				return err
			}
			return observer.Before(func(event InstanceCloseEvent) {
				mu.Lock()
				closed = append(closed, event.Instance)
				mu.Unlock()
			})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	var created []InstanceIdentity
	for i := 0; i < 3; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, InstanceIdentity{value: in})
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []InstanceIdentity{created[2], created[1], created[0]}
	if !reflect.DeepEqual(closed, want) {
		t.Fatalf("instance close order = %v, want reverse creation order", closed)
	}
}

func TestRuntimeCloseStopsAdmissionBeforeReleasingBlockingHostCall(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	stopped := make(chan struct{})
	def := testDefinition("example.com/close/host")
	def.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "blocking host call", Scope: AuthorityScope{Modules: []string{"env"}}}}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			hosts, _ := r.HostImports()
			mod, _ := hosts.Module("env")
			mod.Func("f", func(HostModule, []uint64, []uint64) { close(entered); <-release })
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error { close(stopped); return nil }})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(voidImportCallModule())
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	callDone := make(chan error, 1)
	go func() { _, err := in.Invoke("call"); callDone <- err }()
	<-entered
	closeDone := make(chan error, 1)
	go func() { closeDone <- rt.Close() }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("plugin Stop did not run while host call was active")
	}
	if _, err := in.Invoke("call"); err == nil {
		t.Fatal("new invocation entered after shutdown admission closed")
	}
	close(release)
	<-callDone // interruption or successful unwind are both valid
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeStopCanReleaseBlockedPublicInvoke(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	def := testDefinition("example.com/close/stop-releases-invoke")
	def.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "block until Stop", Scope: AuthorityScope{Modules: []string{"env"}}}}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			hosts, err := r.HostImports()
			if err != nil {
				return err
			}
			module, err := hosts.Module("env")
			if err != nil {
				return err
			}
			module.Func("f", func(HostModule, []uint64, []uint64) { close(entered); <-release })
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				close(release)
				return nil
			}})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(voidImportCallModule())
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	invokeDone := make(chan error, 1)
	go func() { _, err := in.Invoke("call"); invokeDone <- err }()
	<-entered
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-release:
	case <-time.After(time.Second):
		t.Fatal("Runtime.Close did not run Stop to release a blocked public Invoke")
	}
	<-invokeDone // interruption or successful unwind are both valid
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeStopCanReleaseBlockedImportedStart(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var stopped atomic.Bool
	var afterInstantiate atomic.Int32
	def := testDefinition("example.com/close/stop-releases-start")
	def.Authorities = []AuthorityRequest{
		{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "block imported start", Scope: AuthorityScope{Modules: []string{"env"}}},
		{Name: AuthorityInstanceInstantiateObserve, Mode: AuthorityRequired, Reason: "prove callbacks cannot run after Stop"},
	}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			hosts, err := r.HostImports()
			if err != nil {
				return err
			}
			module, err := hosts.Module("env")
			if err != nil {
				return err
			}
			module.Func("f", func(HostModule, []uint64, []uint64) { close(entered); <-release })
			observer, err := r.InstanceInstantiateObserver()
			if err != nil {
				return err
			}
			if err := observer.After(func(InstantiationEvent) {
				if stopped.Load() {
					afterInstantiate.Add(1)
				}
			}); err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				stopped.Store(true)
				close(release)
				return nil
			}})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(blockingImportedStartModule())
	if err != nil {
		t.Fatal(err)
	}
	instantiateDone := make(chan error, 1)
	go func() { _, err := rt.Instantiate(context.Background(), mod); instantiateDone <- err }()
	<-entered
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-release:
	case <-time.After(time.Second):
		t.Fatal("Runtime.Close did not run Stop to release a blocked imported start")
	}
	if err := <-instantiateDone; err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Instantiate racing Runtime.Close = %v, want closed failure", err)
	}
	if got := afterInstantiate.Load(); got != 1 {
		t.Fatalf("admitted AfterInstantiate ran %d time(s), want 1", got)
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCloseDrainsManagedForkBeforeManagerTeardown(t *testing.T) {
	def := testDefinition("example.com/close/managed-fork")
	def.Authorities = []AuthorityRequest{
		{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "fork caller", Scope: AuthorityScope{Modules: []string{"env"}}},
		{Name: AuthorityInstanceManage, Mode: AuthorityRequired, Reason: "own fork", Scope: AuthorityScope{MaxInstances: 1, MaxMemoryBytes: 64 << 10}},
		{Name: AuthorityInstanceInstantiateIntercept, Mode: AuthorityRequired, Reason: "hold admitted fork"},
	}
	entered, release := make(chan struct{}), make(chan struct{})
	forkResult := make(chan error, 1)
	var manager *InstanceManager
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			var err error
			manager, err = reg.ManagedInstances()
			if err != nil {
				return err
			}
			hosts, err := reg.HostImports()
			if err != nil {
				return err
			}
			module, err := hosts.Module("env")
			if err != nil {
				return err
			}
			module.Func("f", func(caller HostModule, _, _ []uint64) {
				child, err := manager.Fork(context.Background(), caller)
				if child != nil {
					err = errors.Join(err, child.Close())
				}
				forkResult <- err
			})
			interceptor, err := reg.InstanceInstantiateInterceptor()
			if err != nil {
				return err
			}
			return interceptor.Before(func(request InstantiationRequest) error {
				if request.Origin == InstantiateManaged {
					close(entered)
					<-release
				}
				return nil
			})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(voidImportCallModule())
	if err != nil {
		t.Fatal(err)
	}
	parent, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	callDone := make(chan error, 1)
	go func() { _, err := parent.Call(context.Background(), "call"); callDone <- err }()
	<-entered
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rt.Closed():
		t.Fatal("runtime teardown completed while managed fork was admitted")
	default:
	}
	close(release)
	if err := <-forkResult; err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("managed fork racing close = %v, want closed failure", err)
	}
	<-callDone // interrupted or successful host-call unwind are both valid.
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAdmittedImportedStartKeepsPluginGenerationUntilTerminalObserver(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	afterReturned, stopped := make(chan struct{}), make(chan struct{})
	def := testDefinition("example.com/close/reserved-start")
	def.Authorities = []AuthorityRequest{
		{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "block imported start", Scope: AuthorityScope{Modules: []string{"env"}}},
		{Name: AuthorityInstanceInstantiateObserve, Mode: AuthorityRequired, Reason: "prove terminal generation retention"},
	}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			hosts, _ := r.HostImports()
			module, _ := hosts.Module("env")
			module.Func("f", func(HostModule, []uint64, []uint64) { close(entered); <-release })
			observer, _ := r.InstanceInstantiateObserver()
			if err := observer.After(func(InstantiationEvent) { close(afterReturned) }); err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error { close(stopped); return nil }})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(blockingImportedStartModule())
	if err != nil {
		t.Fatal(err)
	}
	instantiateDone := make(chan error, 1)
	go func() { _, err := rt.Instantiate(context.Background(), mod); instantiateDone <- err }()
	<-entered
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("plugin Stop did not run")
	}
	close(release)
	if err := <-instantiateDone; err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Instantiate racing close = %v, want closed failure", err)
	}
	select {
	case <-afterReturned:
	case <-time.After(time.Second):
		t.Fatal("admitted terminal observer did not run after Stop")
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func blockingImportedStartModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
	)
}

func TestRetainedCrossInstanceCallCannotEnterPluginAfterStop(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var stopped, calledAfterStop atomic.Bool
	def := testDefinition("example.com/close/retained-call")
	def.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "retained callback", Scope: AuthorityScope{Modules: []string{"env"}}}}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			hosts, err := r.HostImports()
			if err != nil {
				return err
			}
			mod, err := hosts.Module("env")
			if err != nil {
				return err
			}
			mod.Func("f", func(HostModule, []uint64, []uint64) {
				if stopped.Load() {
					calledAfterStop.Store(true)
				}
				calls.Add(1)
				close(entered)
				<-release
			})
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				stopped.Store(true)
				return nil
			}})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	producerModule, err := rt.Compile(voidImportCallModule())
	if err != nil {
		t.Fatal(err)
	}
	producerCompiled := producerModule.Compiled()
	producer, err := rt.Instantiate(context.Background(), producerModule)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := producer.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	consumerCompiled := MustCompile(voidImportCallModule())
	consumer, err := Instantiate(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": exported}})
	if err != nil {
		t.Fatal(err)
	}
	callDone := make(chan error, 1)
	go func() { _, err := consumer.Invoke("call"); callDone <- err }()
	<-entered
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rt.Closed():
		t.Fatal("Runtime teardown completed while a retained plugin callback was active")
	default:
	}
	close(release)
	<-callDone // runtime interruption or normal unwind are both valid
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !stopped.Load() {
		t.Fatal("plugin Stop did not run")
	}
	if _, err := consumer.Invoke("call"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("retained call after plugin Stop = %v, want permission denied", err)
	}
	if calls.Load() != 1 || calledAfterStop.Load() {
		t.Fatalf("plugin calls = %d, calledAfterStop = %v", calls.Load(), calledAfterStop.Load())
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := consumerCompiled.Close(); err != nil {
		t.Fatal(err)
	}
	if err := producerModule.Close(); err != nil {
		t.Fatal(err)
	}
	if err := producerCompiled.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManyContractRequiresEverySelectedProviderAndExactRegistration(t *testing.T) {
	spec := ContractSpec{ID: "example.com/contracts/all", Major: 1}
	p1, p2 := testDefinition("example.com/contracts/all/p1"), testDefinition("example.com/contracts/all/p2")
	p1.Provides, p2.Provides = []ContractSpec{spec}, []ContractSpec{spec}
	consumer := testDefinition("example.com/contracts/all/consumer")
	consumer.Consumes = []ContractRequirement{{ID: spec.ID, Major: 1, Mode: ContractMany}}
	provide := func(def PluginDefinition) PluginProvider {
		return PluginProvider{Definition: def, New: func() Plugin { return pluginFunc(func(r *Registrar) error { return ProvideContract(r, spec, def.ID) }) }}
	}
	consume := PluginProvider{Definition: consumer, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error { _, err := RequireContract(r, spec, ContractMany, (*string)(nil)); return err })
	}}
	set := testSet(t, provide(p1), provide(p2), consume)
	set.Selections[2].Contracts[0].Providers = []string{p1.ID}
	if _, err := InspectPluginPlan(set); err == nil || !strings.Contains(err.Error(), "every selected provider") {
		t.Fatalf("partial many binding = %v", err)
	}

	dupProvider := PluginProvider{Definition: consumer, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			if _, err := RequireContract(r, spec, ContractMany, (*string)(nil)); err != nil {
				return err
			}
			_, err := RequireContract(r, spec, ContractMany, (*string)(nil))
			return err
		})
	}}
	if err := ValidatePluginSet(testSet(t, provide(p1), dupProvider)); err == nil || !strings.Contains(err.Error(), "consumed more than once") {
		t.Fatalf("duplicate consume registration = %v", err)
	}
}

func TestValidatePluginSetPreflightsCommitConflicts(t *testing.T) {
	mk := func(id string) PluginProvider {
		def := testDefinition(id)
		def.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "conflict", Scope: AuthorityScope{Modules: []string{"env"}}}}
		return PluginProvider{Definition: def, New: func() Plugin {
			return pluginFunc(func(r *Registrar) error {
				hosts, _ := r.HostImports()
				mod, _ := hosts.Module("env")
				mod.Func(id, func(HostModule, []uint64, []uint64) {})
				return nil
			})
		}}
	}
	if err := ValidatePluginSet(testSet(t, mk("example.com/conflict/a"), mk("example.com/conflict/b"))); !errors.Is(err, ErrPluginConflict) {
		t.Fatalf("ValidatePluginSet conflict = %v", err)
	}
}

func TestInstructionPluginsShareCoreABIWithoutNamespaceConflict(t *testing.T) {
	mk := func(id, module string) PluginProvider {
		def := testDefinition(id)
		def.Authorities = []AuthorityRequest{{Name: AuthorityCompilerInstructionDefine, Mode: AuthorityRequired, Reason: "define instruction", Scope: AuthorityScope{Modules: []string{module}}}}
		return PluginProvider{Definition: def, New: func() Plugin {
			return pluginFunc(func(r *Registrar) error {
				instructions, err := r.CompilerInstructions()
				if err != nil {
					return err
				}
				return instructions.Define(InstructionSpec{
					Module: module, Name: "identity", Input: []int32{32}, Output: []int32{32},
					Handler: func(_ InstructionContext, args []Bits) ([]Bits, error) { return args, nil },
				})
			})
		}}
	}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t,
		mk("example.com/instruction/a", "example:instruction/a"),
		mk("example.com/instruction/b", "example:instruction/b"),
	)); err != nil {
		t.Fatal(err)
	}
	imports := rt.ProvidedImports()
	var abi int
	for _, imp := range imports {
		if imp.Module == instructionABIModule {
			abi++
		}
	}
	if abi != len(instructionABIImports()) {
		t.Fatalf("instruction ABI imports = %d, want one core set of %d", abi, len(instructionABIImports()))
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedReservationHeldUntilPhysicalRelease(t *testing.T) {
	const pageBytes = uint64(65536)
	rt := NewRuntime()
	manager := newPendingInstanceManager("example.com/quota/physical", AuthorityScope{MaxInstances: 1, MaxMemoryBytes: pageBytes})
	manager.activate(rt)
	mod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	owned, err := manager.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := owned.Instance().ExportedFunc("f")
	if err != nil {
		t.Fatal(err)
	}
	consumerCompiled := MustCompile(voidImportCallModule())
	consumer, err := Instantiate(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": exported}})
	if err != nil {
		t.Fatal(err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if manager.live != 1 || manager.memoryBytes != pageBytes {
		t.Fatalf("quota after logical close = %d/%d, want 1/%d", manager.live, manager.memoryBytes, pageBytes)
	}
	if _, err := manager.Instantiate(context.Background(), mod); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("quota reused before physical release: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := consumerCompiled.Close(); err != nil {
		t.Fatal(err)
	}
	if manager.live != 0 || manager.memoryBytes != 0 {
		t.Fatalf("quota after physical release = %d/%d", manager.live, manager.memoryBytes)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClosedInstanceTrackingDoesNotGrow(t *testing.T) {
	rt := NewRuntime()
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 128; i++ {
		in, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
	rt.mu.Lock()
	tracked := len(rt.instances)
	rt.mu.Unlock()
	if tracked != 0 {
		t.Fatalf("Runtime retained %d physically closed instances", tracked)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkPluginCallGate(b *testing.B) {
	fn := HostFunc(func(HostModule, []uint64, []uint64) {})
	b.Run("plain", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fn(nil, nil, nil)
		}
	})
	b.Run("gated", func(b *testing.B) {
		gated := newPluginCallGate("benchmark").wrap(fn)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			gated(nil, nil, nil)
		}
	})
}
