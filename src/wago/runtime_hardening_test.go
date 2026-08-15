package wago

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestRuntimeConfigOwnsConstructionSnapshot(t *testing.T) {
	base := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	// Default optimization maps are immutable process snapshots on current main.
	// Detach this same-package adversarial mutation from that shared cache before
	// verifying Runtime ingestion ownership.
	base.optimizations = base.optimizationValues()
	rt := NewRuntime(WithRuntimeConfig(base))
	base.optimizations["mutated-after-construction"] = true
	base.functionWorkers = -1
	cfg := rt.Config()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("runtime config aliased caller mutation: %v", err)
	}
	if _, ok := cfg.optimizations["mutated-after-construction"]; ok || cfg.FunctionWorkers() < 0 {
		t.Fatal("runtime config retained caller-owned state")
	}
	cfg.optimizations["mutated-returned-snapshot"] = true
	if _, ok := rt.Config().optimizations["mutated-returned-snapshot"]; ok {
		t.Fatal("Config returned mutable runtime state")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedCompilePreservesGenerationAndWarmAdoption(t *testing.T) {
	def := testDefinition("example.com/compile/prepared")
	def.Authorities = []AuthorityRequest{
		{Name: AuthorityModuleSourceTransform, Mode: AuthorityRequired, Reason: "transform source"},
		{Name: AuthorityModuleCompileObserve, Mode: AuthorityRequired, Reason: "observe compilation"},
	}
	original := wasmtest.Module()
	transformed := wasmtest.Module(wasmtest.Section(0, append(wasmtest.Name("prepared"), 1)))
	var sourceID, compiledID CompilationIdentity
	compileCalls := 0
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			transformer, err := reg.ModuleSourceTransformer()
			if err != nil {
				return err
			}
			if err := transformer.Transform(func(ctx ModuleSourceContext, source []byte) ([]byte, error) {
				compileCalls++
				sourceID = ctx.Compilation
				return append([]byte(nil), transformed...), nil
			}); err != nil {
				return err
			}
			observer, err := reg.ModuleCompileObserver()
			if err != nil {
				return err
			}
			return observer.Observe(func(event ModuleCompiledEvent) { compiledID = event.Compilation })
		})
	}}
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	prepared, err := rt.PrepareCompile(original)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Cacheable() {
		t.Fatal("source-transform generation reported cacheable")
	}
	if !reflect.DeepEqual(prepared.Source(), transformed) || compileCalls != 1 {
		t.Fatalf("prepared source/calls = %x/%d", prepared.Source(), compileCalls)
	}
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), transformed)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := prepared.Adopt(compiled)
	if err != nil {
		t.Fatal(err)
	}
	if sourceID.IsZero() || compiledID != sourceID || compileCalls != 1 {
		t.Fatalf("compile identity/calls = %v/%v/%d", sourceID, compiledID, compileCalls)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Compile(); err == nil || !strings.Contains(err.Error(), "already consumed") {
		t.Fatalf("second prepared consume = %v", err)
	}
}

func TestPreparedCompileOwnsTransformedSourceSnapshot(t *testing.T) {
	def := testDefinition("example.com/compile/source-snapshot")
	def.Authorities = []AuthorityRequest{{Name: AuthorityModuleSourceTransform, Mode: AuthorityRequired, Reason: "return plugin-owned source"}}
	transformed := wasmtest.Module(wasmtest.Section(0, wasmtest.Name("snapshot")))
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			transformer, err := reg.ModuleSourceTransformer()
			if err != nil {
				return err
			}
			return transformer.Transform(func(ModuleSourceContext, []byte) ([]byte, error) { return transformed, nil })
		})
	}}
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	prepared, err := rt.PrepareCompile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), prepared.Source()...)
	for i := range transformed {
		transformed[i] ^= 0xff
	}
	if !reflect.DeepEqual(prepared.Source(), want) {
		t.Fatal("prepared source aliased transformer-owned storage")
	}
	mod, err := prepared.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedCompileTerminalOperationsRaceExactlyOnce(t *testing.T) {
	rt := NewRuntime()
	prepared, err := rt.PrepareCompile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	type terminalResult struct {
		operation string
		err       error
	}
	start := make(chan struct{})
	results := make(chan terminalResult, 3)
	go func() {
		<-start
		mod, err := prepared.Compile()
		if mod != nil {
			err = errors.Join(err, mod.Close())
		}
		results <- terminalResult{operation: "Compile", err: err}
	}()
	go func() {
		<-start
		mod, err := prepared.Adopt(compiled)
		if mod != nil {
			err = errors.Join(err, mod.Close())
		}
		results <- terminalResult{operation: "Adopt", err: err}
	}()
	go func() {
		<-start
		results <- terminalResult{operation: "Close", err: prepared.Close()}
	}()
	close(start)
	consumeSuccesses := 0
	for range 3 {
		result := <-results
		if result.err == nil {
			if result.operation != "Close" {
				consumeSuccesses++
			}
			continue
		}
		if result.operation == "Close" || !strings.Contains(result.err.Error(), "already consumed") {
			t.Fatalf("%s terminal race error = %v", result.operation, result.err)
		}
	}
	if consumeSuccesses > 1 {
		t.Fatalf("terminal race successful consumes = %d, want at most one", consumeSuccesses)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedCompileAdoptNilConsumesPreparation(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	prepared, err := rt.PrepareCompile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Adopt(nil); err == nil || !strings.Contains(err.Error(), "nil compiled artifact") {
		t.Fatalf("Adopt(nil) = %v", err)
	}
	if _, err := prepared.Compile(); err == nil || !strings.Contains(err.Error(), "already consumed") {
		t.Fatalf("Compile after Adopt(nil) = %v", err)
	}
}

func TestPreparedCompileCloseReleasesShutdownAdmission(t *testing.T) {
	rt := NewRuntime()
	prepared, err := rt.PrepareCompile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	done := rt.Closed()
	select {
	case <-done:
		t.Fatal("shutdown completed while prepared compile retained its generation")
	default:
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCloseFromImportedStartIsReentrant(t *testing.T) {
	rt := NewRuntime()
	mod, err := rt.Compile(importedStartModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	callbackReturned := make(chan struct{})
	instantiateDone := make(chan error, 1)
	go func() {
		in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{
			"env.start": HostFunc(func(HostModule, []uint64, []uint64) {
				if err := rt.Close(); err != nil {
					t.Errorf("reentrant Close: %v", err)
				}
				close(callbackReturned)
			}),
		}))
		if in != nil {
			_ = in.Close()
		}
		instantiateDone <- err
	}()
	select {
	case <-callbackReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("reentrant Close deadlocked")
	}
	if err := <-instantiateDone; err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Instantiate racing reentrant Close = %v", err)
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCloseCallbackFreeFastPathCompletesInline(t *testing.T) {
	rt := NewRuntime()
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rt.Closed():
	default:
		t.Fatal("callback-free runtime shutdown did not complete inline")
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCloseReferenceStoreWorkIsNotInline(t *testing.T) {
	rt := NewRuntime()
	if _, err := rt.NewExternRef("retained"); err != nil {
		t.Fatal(err)
	}
	if rt.refStore.emptyForInlineClose() {
		t.Fatal("reference store with an externref reported constant close work")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCloseContextPreCanceledHasNoSideEffects(t *testing.T) {
	rt := NewRuntime()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rt.CloseContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("CloseContext = %v, want context.Canceled", err)
	}
	if rt.Closed() != nil {
		t.Fatal("pre-canceled CloseContext published shutdown")
	}
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatalf("runtime unusable after pre-canceled close: %v", err)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeCloseContextTimeoutStillPublishesFinalCompletion(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	def := testDefinition("example.com/close/context-timeout")
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			return reg.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				close(entered)
				<-release
				return nil
			}})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- rt.CloseContext(ctx) }()
	<-entered
	if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext = %v, want deadline", err)
	}
	close(release)
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeClosedAndWaitClosedObserveFinalError(t *testing.T) {
	stopErr := errors.New("stop failed")
	entered, release := make(chan struct{}), make(chan struct{})
	def := testDefinition("example.com/close/final-error")
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			return reg.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
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
	owner := make(chan error, 1)
	go func() { owner <- rt.Close() }()
	<-entered
	done := rt.Closed()
	if done == nil {
		t.Fatal("Closed returned nil after shutdown started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rt.WaitClosed(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled WaitClosed = %v", err)
	}
	close(release)
	if err := <-owner; err != nil {
		t.Fatalf("owner Close = %v", err)
	}
	if err := rt.WaitClosed(context.Background()); !errors.Is(err, stopErr) {
		t.Fatalf("final WaitClosed = %v", err)
	}
	select {
	case <-done:
	default:
		t.Fatal("Closed did not complete")
	}
}

func TestRuntimeCompileOwnsAndModuleBorrowsCompiled(t *testing.T) {
	rt := NewRuntime()
	owned, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	ownedCompiled := owned.Compiled()
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Instantiate(ownedCompiled); err == nil || !strings.Contains(err.Error(), "compiled module is closed") {
		t.Fatalf("Runtime.Compile module did not close owned code: %v", err)
	}

	borrowedCompiled, err := Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := rt.Module(borrowedCompiled)
	if err != nil {
		t.Fatal(err)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(borrowedCompiled)
	if err != nil {
		t.Fatalf("Runtime.Module closed borrowed code: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := borrowedCompiled.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRejectsForeignModuleBeforeDestinationClosedState(t *testing.T) {
	a := NewRuntime()
	mod, err := a.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	b := NewRuntime()
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Instantiate(context.Background(), mod); !errors.Is(err, ErrForeignModule) {
		t.Fatalf("foreign module error = %v", err)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeBindingReappliesDestinationExecutionPolicy(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithIndependentInstanceExecution(true), wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	serial := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithIndependentInstanceExecution(false)))
	mod, err := serial.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	in, err := serial.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	if in.usesIndependentExecution() {
		t.Fatal("destination runtime serial policy was not applied")
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
	if err := serial.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRegisteredImportMetadataIsOwned(t *testing.T) {
	def := testDefinition("example.com/import/snapshot")
	def.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "define env", Scope: AuthorityScope{Modules: []string{"env"}}}}
	var builder *ImportFuncBuilder
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			hosts, err := reg.HostImports()
			if err != nil {
				return err
			}
			module, err := hosts.Module("env")
			if err != nil {
				return err
			}
			builder = module.Func("f", func(HostModule, []uint64, []uint64) {}).Params(ValI32).Results(ValI64).Docs("original")
			return nil
		})
	}}
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	builder.Params(ValV128).Results(ValV128).Capability("mutated").Docs("mutated")
	imports := rt.ProvidedImports()
	if len(imports) != 1 || !reflect.DeepEqual(imports[0].Params, []ValType{ValI32}) || !reflect.DeepEqual(imports[0].Results, []ValType{ValI64}) || imports[0].HasCapability || imports[0].Docs != "original" {
		t.Fatalf("registered import metadata aliased plugin builder: %#v", imports)
	}
}

func TestModuleCloseObserverCanReenterClose(t *testing.T) {
	def := testDefinition("example.com/module/reentrant-close")
	def.Authorities = []AuthorityRequest{{Name: AuthorityModuleCloseObserve, Mode: AuthorityRequired, Reason: "reenter close"}}
	var mod *Module
	callbackReturned := make(chan struct{})
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			observer, err := reg.ModuleCloseObserver()
			if err != nil {
				return err
			}
			return observer.Observe(func(ModuleCloseEvent) {
				if err := mod.Close(); err != nil {
					t.Errorf("reentrant Module.Close: %v", err)
				}
				close(callbackReturned)
			})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	var err error
	mod, err = rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- mod.Close() }()
	select {
	case <-callbackReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("reentrant Module.Close deadlocked")
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestModuleCloseObserverCanCloseRuntime(t *testing.T) {
	def := testDefinition("example.com/module/close-runtime")
	def.Authorities = []AuthorityRequest{{Name: AuthorityModuleCloseObserve, Mode: AuthorityRequired, Reason: "close runtime"}}
	callbackReturned := make(chan struct{})
	var rt *Runtime
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			observer, err := reg.ModuleCloseObserver()
			if err != nil {
				return err
			}
			return observer.Observe(func(ModuleCloseEvent) {
				if err := rt.Close(); err != nil {
					t.Errorf("Runtime.Close from module observer: %v", err)
				}
				close(callbackReturned)
			})
		})
	}}
	rt = NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- mod.Close() }()
	select {
	case <-callbackReturned:
	case <-time.After(5 * time.Second):
		t.Fatal("Runtime.Close from module observer deadlocked")
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if err := rt.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeModuleImportsReturnsDeepSnapshot(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64})
	mod, err := rt.Compile(returningImportModule(sig, []byte{0x00, 0x42, 0x00, 0x0b}))
	if err != nil {
		t.Fatal(err)
	}
	first := mod.Imports()
	first[0].Params[0] = ValV128
	first[0].Results[0] = ValV128
	first[0].ParamTypes[0].Kind = ValueTypeV128
	first[0].ResultTypes[0].Kind = ValueTypeV128
	second := mod.Imports()
	if second[0].Params[0] != ValI32 || second[0].Results[0] != ValI64 || second[0].ParamTypes[0].Kind != ValueTypeI32 || second[0].ResultTypes[0].Kind != ValueTypeI64 {
		t.Fatalf("caller mutation changed module imports: %#v", second)
	}
	if err := mod.Close(); err != nil {
		t.Fatal(err)
	}
}
