package wago

// This file is a test-only migration harness for assertions written against the
// pre-vNext plugin API. It is deliberately compiled only into src/wago tests:
// production packages expose no compatibility aliases. The harness keeps the
// old runtime/compiler/lifecycle coverage active while routing it through the
// vNext internals and opaque event implementation.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	coreplugins "github.com/wago-org/wago/src/core/plugins"
)

type PluginCapability string

const (
	PluginHostImports      PluginCapability = "host.imports"
	PluginHostEnvironment  PluginCapability = "host.environment"
	PluginCompileHooks     PluginCapability = "module.compile"
	PluginInstanceHooks    PluginCapability = "instance.lifecycle"
	PluginInvokeHooks      PluginCapability = "instance.invoke"
	PluginRuntimeHooks     PluginCapability = "runtime.lifecycle"
	PluginManagedInstances PluginCapability = "instance.manage"
	PluginCoreRuntime      PluginCapability = "core.runtime"
	ErrExtensionConflict                    = ErrPluginConflict
)

type CapabilityBudget = AuthorityScope

func validPluginCapability(cap PluginCapability) bool {
	switch cap {
	case PluginHostImports, PluginHostEnvironment, PluginCompileHooks, PluginInstanceHooks,
		PluginInvokeHooks, PluginRuntimeHooks, PluginManagedInstances, PluginCoreRuntime:
		return true
	default:
		return false
	}
}

type ExtensionInfo struct {
	ID                   string
	Name                 string
	Version              string
	Description          string
	Stability            Stability
	Compat               Compatibility
	RequiresCapabilities []PluginCapability
}

type Extension interface {
	Info() ExtensionInfo
	Register(*Registry) error
}

type ExtensionError struct {
	Extension string
	Operation string
	Err       error
}

func (e *ExtensionError) Error() string {
	return fmt.Sprintf("wago extension %s: %s: %v", e.Extension, e.Operation, e.Err)
}
func (e *ExtensionError) Unwrap() error { return e.Err }

type useConfig struct {
	strict bool
	grants map[PluginCapability]struct{}
	config json.RawMessage
}
type UseOption func(*useConfig)

func WithPluginGrants(caps ...PluginCapability) UseOption {
	return func(cfg *useConfig) {
		cfg.strict = true
		cfg.grants = map[PluginCapability]struct{}{}
		for _, cap := range caps {
			cfg.grants[cap] = struct{}{}
		}
	}
}

type RuntimeContext struct{ Runtime *Runtime }
type CompileContext struct {
	Runtime  *Runtime
	Metadata map[string]any
}
type InstantiateContext struct {
	Runtime  *Runtime
	Module   *Module
	Compiled *Compiled
	Imports  Imports
	Origin   InstantiateOrigin
	Metadata map[string]any
}
type InstanceContext struct {
	Runtime  *Runtime
	Compiled *Compiled
	Instance *Instance
	Origin   InstantiateOrigin
	Metadata map[string]any
}
type InvokeContext struct {
	Runtime  *Runtime
	Instance *Instance
	Export   string
	Args     []Value
	Start    time.Time
	Metadata map[string]any
}

type HookRegistry struct {
	onRuntimeClose     []func(*RuntimeContext)
	internalClose      []func() error
	beforeCompile      []func(*CompileContext, []byte) ([]byte, error)
	afterCompile       []func(*CompileContext, *Module) error
	beforeInstantiate  []func(*InstantiateContext) error
	afterInstantiate   []func(*InstantiateContext, *Instance) error
	onInstantiateError []func(*InstantiateContext, error)
	beforeClose        []func(*InstanceContext)
	afterClose         []func(*InstanceContext)
	beforeInvoke       []func(*InvokeContext) error
	afterInvoke        []func(*InvokeContext, []Value, error)
}

func (h *HookRegistry) OnRuntimeClose(fns ...func(*RuntimeContext)) {
	h.onRuntimeClose = append(h.onRuntimeClose, fns...)
}
func (h *HookRegistry) BeforeCompile(fns ...func(*CompileContext, []byte) ([]byte, error)) {
	h.beforeCompile = append(h.beforeCompile, fns...)
}
func (h *HookRegistry) AfterCompile(fns ...func(*CompileContext, *Module) error) {
	h.afterCompile = append(h.afterCompile, fns...)
}
func (h *HookRegistry) BeforeInstantiate(fns ...func(*InstantiateContext) error) {
	h.beforeInstantiate = append(h.beforeInstantiate, fns...)
}
func (h *HookRegistry) AfterInstantiate(fns ...func(*InstantiateContext, *Instance) error) {
	h.afterInstantiate = append(h.afterInstantiate, fns...)
}
func (h *HookRegistry) OnInstantiateError(fns ...func(*InstantiateContext, error)) {
	h.onInstantiateError = append(h.onInstantiateError, fns...)
}
func (h *HookRegistry) BeforeClose(fns ...func(*InstanceContext)) {
	h.beforeClose = append(h.beforeClose, fns...)
}
func (h *HookRegistry) AfterClose(fns ...func(*InstanceContext)) {
	h.afterClose = append(h.afterClose, fns...)
}
func (h *HookRegistry) BeforeInvoke(fns ...func(*InvokeContext) error) {
	h.beforeInvoke = append(h.beforeInvoke, fns...)
}
func (h *HookRegistry) AfterInvoke(fns ...func(*InvokeContext, []Value, error)) {
	h.afterInvoke = append(h.afterInvoke, fns...)
}

type Registry struct {
	info         ExtensionInfo
	caps         []capabilitySpec
	imports      []*registeredImport
	hooks        *HookRegistry
	managers     []*InstanceManager
	activate     []func(*Runtime)
	grants       map[PluginCapability]struct{}
	used         map[PluginCapability]struct{}
	config       json.RawMessage
	compiler     *CompilerRegistry
	customTypes  map[string]CustomType
	instructions []*registeredInstruction
}

func (r *Registry) authorize(cap PluginCapability) error {
	if r == nil {
		return fmt.Errorf("wago: nil plugin registry")
	}
	if r.used == nil {
		r.used = map[PluginCapability]struct{}{}
	}
	r.used[cap] = struct{}{}
	if r.grants != nil {
		if _, ok := r.grants[cap]; !ok {
			return fmt.Errorf("plugin capability %q was not granted: %w", cap, ErrPermissionDenied)
		}
	}
	return nil
}
func (r *Registry) Granted(cap PluginCapability) bool { _, ok := r.grants[cap]; return ok }
func (r *Registry) Capability(cap Capability, opts ...CapabilityOption) {
	spec := capabilitySpec{cap: cap}
	for _, opt := range opts {
		opt(&spec)
	}
	r.caps = append(r.caps, spec)
}
func (r *Registry) Config(dst any) error {
	b := r.config
	if len(b) == 0 {
		b = []byte("{}")
	}
	return json.Unmarshal(b, dst)
}
func (r *Registry) Hooks() *HookRegistry {
	if r.hooks == nil {
		r.hooks = &HookRegistry{}
	}
	return r.hooks
}

type legacyImportModuleBuilder struct {
	reg    *Registry
	module string
}
type legacyImportFuncBuilder struct{ imp *registeredImport }

func (r *Registry) ImportModule(name string) *legacyImportModuleBuilder {
	return &legacyImportModuleBuilder{reg: r, module: name}
}
func (m *legacyImportModuleBuilder) Func(name string, fn HostFunc) *legacyImportFuncBuilder {
	imp := &registeredImport{module: m.module, name: name, fn: fn}
	m.reg.imports = append(m.reg.imports, imp)
	return &legacyImportFuncBuilder{imp: imp}
}
func (f *legacyImportFuncBuilder) Params(types ...ValType) *legacyImportFuncBuilder {
	f.imp.params = append(f.imp.params[:0], types...)
	return f
}
func (f *legacyImportFuncBuilder) Results(types ...ValType) *legacyImportFuncBuilder {
	f.imp.results = append(f.imp.results[:0], types...)
	return f
}
func (f *legacyImportFuncBuilder) Capability(cap Capability) *legacyImportFuncBuilder {
	f.imp.cap, f.imp.hasCap = cap, true
	return f
}
func (f *legacyImportFuncBuilder) Docs(docs string) *legacyImportFuncBuilder {
	f.imp.docs = docs
	return f
}

type HostImportAccess struct{ reg *Registry }

func (r *Registry) HostImports() (*HostImportAccess, error) {
	if err := r.authorize(PluginHostImports); err != nil {
		return nil, err
	}
	return &HostImportAccess{reg: r}, nil
}
func (a *HostImportAccess) Module(name string) *legacyImportModuleBuilder {
	return a.reg.ImportModule(name)
}
func (a *HostImportAccess) CallerResolver() *CallerResolver {
	resolver := &CallerResolver{}
	a.reg.activate = append(a.reg.activate, resolver.activate)
	return resolver
}

type RuntimeHookAccess struct{ hooks *HookRegistry }

func (r *Registry) RuntimeLifecycle() (*RuntimeHookAccess, error) {
	if err := r.authorize(PluginRuntimeHooks); err != nil {
		return nil, err
	}
	return &RuntimeHookAccess{r.Hooks()}, nil
}
func (a *RuntimeHookAccess) OnClose(fns ...func(*RuntimeContext)) { a.hooks.OnRuntimeClose(fns...) }

type CompileHookAccess struct{ hooks *HookRegistry }

func (r *Registry) ModuleCompiler() (*CompileHookAccess, error) {
	if err := r.authorize(PluginCompileHooks); err != nil {
		return nil, err
	}
	return &CompileHookAccess{r.Hooks()}, nil
}
func (a *CompileHookAccess) Before(fns ...func(*CompileContext, []byte) ([]byte, error)) {
	a.hooks.BeforeCompile(fns...)
}
func (a *CompileHookAccess) After(fns ...func(*CompileContext, *Module) error) {
	a.hooks.AfterCompile(fns...)
}

type InstanceHookAccess struct{ hooks *HookRegistry }

func (r *Registry) InstanceLifecycle() (*InstanceHookAccess, error) {
	if err := r.authorize(PluginInstanceHooks); err != nil {
		return nil, err
	}
	return &InstanceHookAccess{r.Hooks()}, nil
}
func (a *InstanceHookAccess) BeforeInstantiate(fns ...func(*InstantiateContext) error) {
	a.hooks.BeforeInstantiate(fns...)
}
func (a *InstanceHookAccess) AfterInstantiate(fns ...func(*InstantiateContext, *Instance) error) {
	a.hooks.AfterInstantiate(fns...)
}
func (a *InstanceHookAccess) OnInstantiateError(fns ...func(*InstantiateContext, error)) {
	a.hooks.OnInstantiateError(fns...)
}
func (a *InstanceHookAccess) BeforeClose(fns ...func(*InstanceContext)) { a.hooks.BeforeClose(fns...) }
func (a *InstanceHookAccess) AfterClose(fns ...func(*InstanceContext))  { a.hooks.AfterClose(fns...) }

type InvokeHookAccess struct{ hooks *HookRegistry }

func (r *Registry) InstanceInvocation() (*InvokeHookAccess, error) {
	if err := r.authorize(PluginInvokeHooks); err != nil {
		return nil, err
	}
	return &InvokeHookAccess{r.Hooks()}, nil
}
func (a *InvokeHookAccess) Before(fns ...func(*InvokeContext) error) { a.hooks.BeforeInvoke(fns...) }
func (a *InvokeHookAccess) After(fns ...func(*InvokeContext, []Value, error)) {
	a.hooks.AfterInvoke(fns...)
}

func (r *Registry) ManagedInstances() (*InstanceManager, error) {
	if err := r.authorize(PluginManagedInstances); err != nil {
		return nil, err
	}
	m := newPendingInstanceManager(r.info.ID, AuthorityScope{})
	r.managers = append(r.managers, m)
	r.activate = append(r.activate, m.activate)
	r.Hooks().internalClose = append(r.Hooks().internalClose, m.close)
	return m, nil
}

func (m *InstanceManager) Caller(caller HostModule) (*Instance, error) { return m.caller(caller) }

type CompilerRegistry struct{ reg *Registry }

func (r *Registry) Compiler() *CompilerRegistry {
	if r.compiler == nil {
		r.compiler = &CompilerRegistry{reg: r}
	}
	return r.compiler
}
func (r *CompilerRegistry) Type(spec CustomTypeSpec) (CustomType, error) {
	if r == nil || r.reg == nil {
		return CustomType{}, fmt.Errorf("wago: nil compiler registry")
	}
	typ, err := coreplugins.PrepareCustomType(spec)
	if err != nil {
		return CustomType{}, err
	}
	if r.reg.customTypes == nil {
		r.reg.customTypes = map[string]CustomType{}
	}
	if previous, ok := r.reg.customTypes[typ.Name()]; ok {
		if !previous.Equal(typ) {
			return CustomType{}, fmt.Errorf("wago: custom type %q conflicts", typ.Name())
		}
		return previous, nil
	}
	r.reg.customTypes[typ.Name()] = typ
	return typ, nil
}
func (r *CompilerRegistry) Instruction(spec InstructionSpec) error {
	if r == nil || r.reg == nil {
		return fmt.Errorf("wago: nil compiler registry")
	}
	if spec.Custom != nil {
		check := func(typ CustomType) error {
			registered, ok := r.reg.customTypes[typ.Name()]
			if !typ.IsZero() && (!ok || !registered.Equal(typ)) {
				return fmt.Errorf("wago: custom type %q was not registered with this compiler registry", typ.Name())
			}
			return nil
		}
		for _, typ := range spec.Custom.Inputs {
			if err := check(typ); err != nil {
				return err
			}
		}
		if spec.Custom.Output != nil {
			if err := check(*spec.Custom.Output); err != nil {
				return err
			}
		}
	}
	definition, err := coreplugins.Prepare(spec)
	if err != nil {
		return err
	}
	r.reg.instructions = append(r.reg.instructions, &registeredInstruction{spec: definition.Spec, definition: definition})
	return nil
}

type CoreRuntimeAccess struct {
	mu sync.RWMutex
	rt *Runtime
}

func (r *Registry) CoreRuntime() (*CoreRuntimeAccess, error) {
	if err := r.authorize(PluginCoreRuntime); err != nil {
		return nil, err
	}
	a := &CoreRuntimeAccess{}
	r.activate = append(r.activate, a.activate)
	r.Hooks().internalClose = append(r.Hooks().internalClose, a.close)
	return a, nil
}
func (a *CoreRuntimeAccess) activate(rt *Runtime) { a.mu.Lock(); a.rt = rt; a.mu.Unlock() }
func (a *CoreRuntimeAccess) close() error         { a.mu.Lock(); a.rt = nil; a.mu.Unlock(); return nil }
func (a *CoreRuntimeAccess) runtime() (*Runtime, error) {
	if a == nil {
		return nil, fmt.Errorf("wago: core runtime access inactive: %w", ErrPermissionDenied)
	}
	a.mu.RLock()
	rt := a.rt
	a.mu.RUnlock()
	if rt == nil {
		return nil, fmt.Errorf("wago: core runtime access inactive: %w", ErrPermissionDenied)
	}
	return rt, nil
}
func (a *CoreRuntimeAccess) Compile(source []byte) (*Module, error) {
	rt, err := a.runtime()
	if err != nil {
		return nil, err
	}
	return rt.Compile(source)
}

var legacyExtensions = struct {
	sync.Mutex
	values map[*Runtime][]ExtensionInfo
}{values: map[*Runtime][]ExtensionInfo{}}

func (rt *Runtime) Extensions() []ExtensionInfo {
	legacyExtensions.Lock()
	defer legacyExtensions.Unlock()
	return append([]ExtensionInfo(nil), legacyExtensions.values[rt]...)
}
func (rt *Runtime) Extension(id string) (ExtensionInfo, bool) {
	if rt == nil {
		return ExtensionInfo{}, false
	}
	for _, info := range rt.Extensions() {
		if info.ID == id {
			return info, true
		}
	}
	return ExtensionInfo{}, false
}
func (rt *Runtime) HostImports() Imports {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make(Imports, len(rt.imports))
	for key, value := range rt.imports {
		out[key] = value
	}
	return out
}

func (rt *Runtime) Use(ext Extension, opts ...UseOption) error {
	if ext == nil {
		return fmt.Errorf("wago: Use: nil extension")
	}
	info := ext.Info()
	if info.ID == "" {
		return fmt.Errorf("wago: Use: extension has no ID")
	}
	if err := checkCompat(info.Compat); err != nil {
		return &ExtensionError{Extension: info.ID, Operation: "use", Err: err}
	}
	var cfg useConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.strict {
		for cap := range cfg.grants {
			if !validPluginCapability(cap) {
				return fmt.Errorf("unknown plugin capability %q", cap)
			}
		}
		for _, cap := range info.RequiresCapabilities {
			if _, ok := cfg.grants[cap]; !ok {
				return fmt.Errorf("plugin capability %q was not granted: %w", cap, ErrPermissionDenied)
			}
		}
	}
	reg := &Registry{info: info, hooks: &HookRegistry{}, grants: cfg.grants, config: cfg.config}
	if err := ext.Register(reg); err != nil {
		return &ExtensionError{Extension: info.ID, Operation: "register", Err: err}
	}
	if cfg.strict {
		for cap := range reg.used {
			if _, ok := cfg.grants[cap]; !ok {
				return fmt.Errorf("plugin exercised capability %q without a grant: %w", cap, ErrPermissionDenied)
			}
		}
	}
	if len(reg.instructions) != 0 {
		declared := false
		for _, cap := range reg.caps {
			if cap.cap == CapCompilerCodegen {
				declared = true
			}
		}
		if !declared {
			return fmt.Errorf("compiler contributions require %q: %w", CapCompilerCodegen, ErrPermissionDenied)
		}
		for _, ins := range reg.instructions {
			reg.imports = append(reg.imports, instructionImport(ins))
		}
		reg.imports = append(reg.imports, instructionABIImports()...)
	}

	rt.mu.Lock()
	if rt.state == runtimeClosing || rt.state == runtimeClosed {
		rt.mu.Unlock()
		return fmt.Errorf("wago: Use on a closed runtime")
	}
	legacyExtensions.Lock()
	for _, previous := range legacyExtensions.values[rt] {
		if previous.ID == info.ID {
			legacyExtensions.Unlock()
			rt.mu.Unlock()
			return &ExtensionError{Extension: info.ID, Operation: "use", Err: ErrExtensionConflict}
		}
	}
	legacyExtensions.Unlock()
	for _, imp := range reg.imports {
		if imp.fn == nil {
			rt.mu.Unlock()
			return fmt.Errorf("import %q has no function", imp.key())
		}
		if owner, ok := rt.moduleOwner[imp.module]; ok && owner != info.ID && rt.overridePolicy != AllowTestOverrides {
			rt.mu.Unlock()
			return &ExtensionError{Extension: info.ID, Operation: "register", Err: fmt.Errorf("module conflict: %w", ErrExtensionConflict)}
		}
	}
	for _, ins := range reg.instructions {
		rt.instructions[ins.spec.Module+"."+ins.spec.Name] = ins
	}
	for _, imp := range reg.imports {
		rt.imports[imp.key()] = imp.fn
		rt.importMeta[imp.key()] = imp
		rt.importOwner[imp.key()] = info.ID
		rt.moduleOwner[imp.module] = info.ID
	}
	for _, cap := range reg.caps {
		if _, ok := rt.caps[cap.cap]; !ok {
			rt.capOrder = append(rt.capOrder, cap.cap)
		}
		rt.caps[cap.cap] = info.ID
	}
	rt.mu.Unlock()

	commitLegacyHooks(rt, reg.hooks)
	for _, activate := range reg.activate {
		activate(rt)
	}
	legacyExtensions.Lock()
	legacyExtensions.values[rt] = append(legacyExtensions.values[rt], info)
	legacyExtensions.Unlock()
	return nil
}

func commitLegacyHooks(rt *Runtime, legacy *HookRegistry) {
	if legacy == nil {
		return
	}
	var mu sync.Mutex
	compileQueue := []*CompileContext{}
	instantiateQueue := map[*Compiled][]*InstantiateContext{}
	closeContexts := map[*Instance]*InstanceContext{}
	invokeContexts := map[OperationIdentity]*InvokeContext{}
	if len(legacy.beforeCompile) != 0 {
		rt.hooks.beforeCompile = append(rt.hooks.beforeCompile, func(_ ModuleSourceContext, source []byte) ([]byte, error) {
			ctx := &CompileContext{Runtime: rt, Metadata: map[string]any{}}
			current := source
			for _, fn := range legacy.beforeCompile {
				next, err := fn(ctx, current)
				if err != nil {
					return nil, err
				}
				if next != nil {
					current = next
				}
			}
			mu.Lock()
			compileQueue = append(compileQueue, ctx)
			mu.Unlock()
			return current, nil
		})
	}
	if len(legacy.afterCompile) != 0 {
		rt.hooks.afterCompile = append(rt.hooks.afterCompile, func(event ModuleCompiledEvent) {
			mu.Lock()
			var ctx *CompileContext
			if len(compileQueue) != 0 {
				ctx = compileQueue[0]
				compileQueue = compileQueue[1:]
			}
			mu.Unlock()
			if ctx == nil {
				ctx = &CompileContext{Runtime: rt, Metadata: map[string]any{}}
			}
			mod := &Module{rt: rt, c: event.Module.compiled, imports: event.Module.Imports()}
			for _, fn := range legacy.afterCompile {
				_ = fn(ctx, mod)
			}
		})
	}
	if len(legacy.beforeInstantiate) != 0 || len(legacy.afterInstantiate) != 0 || len(legacy.onInstantiateError) != 0 {
		rt.hooks.beforeInstantiate = append(rt.hooks.beforeInstantiate, func(event InstantiationRequest) error {
			mod := &Module{rt: rt, c: event.Module.compiled, imports: event.Module.Imports()}
			ctx := &InstantiateContext{Runtime: rt, Module: mod, Compiled: mod.c, Imports: rt.HostImports(), Origin: event.Origin, Metadata: map[string]any{}}
			for _, fn := range legacy.beforeInstantiate {
				if err := fn(ctx); err != nil {
					return err
				}
			}
			mu.Lock()
			instantiateQueue[mod.c] = append(instantiateQueue[mod.c], ctx)
			mu.Unlock()
			return nil
		})
		pop := func(compiled *Compiled) *InstantiateContext {
			mu.Lock()
			defer mu.Unlock()
			queue := instantiateQueue[compiled]
			if len(queue) == 0 {
				return &InstantiateContext{Runtime: rt, Compiled: compiled, Metadata: map[string]any{}}
			}
			ctx := queue[0]
			instantiateQueue[compiled] = queue[1:]
			return ctx
		}
		rt.hooks.afterInstantiate = append(rt.hooks.afterInstantiate, func(event InstantiationEvent) {
			ctx := pop(event.Module.compiled)
			ctx.Origin = event.Origin
			var errs []error
			for _, fn := range legacy.afterInstantiate {
				callback := fn
				var callbackErr error
				panicErr := callSafely("legacy AfterInstantiate", func() { callbackErr = callback(ctx, event.Instance.value) })
				if err := joinPrimary(callbackErr, panicErr); err != nil {
					errs = append(errs, err)
				}
			}
			if err := errors.Join(errs...); err != nil {
				panic(err)
			}
		})
		rt.hooks.onInstantiateError = append(rt.hooks.onInstantiateError, func(event InstantiationErrorEvent) {
			ctx := pop(event.Module.compiled)
			ctx.Origin = event.Origin
			for _, fn := range legacy.onInstantiateError {
				fn(ctx, event.Err)
			}
		})
	}
	if len(legacy.beforeClose) != 0 {
		rt.hooks.beforeClose = append(rt.hooks.beforeClose, func(event InstanceCloseEvent) {
			ctx := &InstanceContext{Runtime: rt, Compiled: event.Module.compiled, Instance: event.Instance.value, Origin: event.Origin, Metadata: map[string]any{}}
			mu.Lock()
			closeContexts[event.Instance.value] = ctx
			mu.Unlock()
			var errs []error
			for i := len(legacy.beforeClose) - 1; i >= 0; i-- {
				callback := legacy.beforeClose[i]
				if err := callSafely("legacy BeforeClose", func() { callback(ctx) }); err != nil {
					errs = append(errs, err)
				}
			}
			if err := errors.Join(errs...); err != nil {
				panic(err)
			}
		})
	}
	if len(legacy.afterClose) != 0 {
		rt.hooks.afterClose = append(rt.hooks.afterClose, func(event InstanceCloseEvent) {
			mu.Lock()
			ctx := closeContexts[event.Instance.value]
			delete(closeContexts, event.Instance.value)
			mu.Unlock()
			if ctx == nil {
				ctx = &InstanceContext{Runtime: rt, Compiled: event.Module.compiled, Instance: event.Instance.value, Origin: event.Origin, Metadata: map[string]any{}}
			}
			var errs []error
			for i := len(legacy.afterClose) - 1; i >= 0; i-- {
				callback := legacy.afterClose[i]
				if err := callSafely("legacy AfterClose", func() { callback(ctx) }); err != nil {
					errs = append(errs, err)
				}
			}
			if err := errors.Join(errs...); err != nil {
				panic(err)
			}
		})
	}
	if len(legacy.beforeInvoke) != 0 {
		rt.hooks.beforeInvoke = append(rt.hooks.beforeInvoke, func(event InvocationRequest) error {
			ctx := &InvokeContext{Runtime: rt, Instance: event.Instance.value, Export: event.Export, Args: append([]Value(nil), event.Args...), Start: event.Start, Metadata: map[string]any{}}
			mu.Lock()
			invokeContexts[event.Operation] = ctx
			mu.Unlock()
			for _, fn := range legacy.beforeInvoke {
				if err := fn(ctx); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if len(legacy.afterInvoke) != 0 {
		rt.hooks.afterInvoke = append(rt.hooks.afterInvoke, func(event InvocationEvent) {
			mu.Lock()
			ctx := invokeContexts[event.Operation]
			delete(invokeContexts, event.Operation)
			mu.Unlock()
			if ctx == nil {
				ctx = &InvokeContext{Runtime: rt, Instance: event.Instance.value, Export: event.Export, Start: event.Start, Metadata: map[string]any{}}
			}
			for _, fn := range legacy.afterInvoke {
				fn(ctx, append([]Value(nil), event.Results...), event.Err)
			}
		})
	}
	for _, fn := range legacy.onRuntimeClose {
		callback := fn
		rt.hooks.onRuntimeClose = append(rt.hooks.onRuntimeClose, func(RuntimeCloseEvent) { callback(&RuntimeContext{Runtime: rt}) })
	}
	rt.hooks.internalClose = append(rt.hooks.internalClose, legacy.internalClose...)
}
