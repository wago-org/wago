package wago

import (
	"context"
	"fmt"
	"sync"
)

func (r *Registrar) authorize(authority Authority) (AuthorityGrant, error) {
	if err := r.ensureOpen(); err != nil {
		return AuthorityGrant{}, err
	}
	if !validAuthority(authority) {
		return AuthorityGrant{}, fmt.Errorf("wago: unknown plugin authority %q: %w", authority, ErrPermissionDenied)
	}
	r.used[authority] = struct{}{}
	if _, declared := r.requests[authority]; !declared {
		return AuthorityGrant{}, &PluginError{Plugin: r.definition.ID, Phase: PluginPhaseAuthorize, Authority: authority, Err: fmt.Errorf("authority was not declared: %w", ErrPermissionDenied)}
	}
	grant, granted := r.grants[authority]
	if !granted {
		return AuthorityGrant{}, &PluginError{Plugin: r.definition.ID, Phase: PluginPhaseAuthorize, Authority: authority, Err: fmt.Errorf("authority was not granted: %w", ErrPermissionDenied)}
	}
	return grant, nil
}

// HostImportRegistrar declares functions only in the grant's exact module scope.
type HostImportRegistrar struct {
	reg     *Registrar
	modules map[string]struct{}
}

func (r *Registrar) HostImports() (*HostImportRegistrar, error) {
	grant, err := r.authorize(AuthorityHostImportDefine)
	if err != nil {
		return nil, err
	}
	modules := make(map[string]struct{}, len(grant.Scope.Modules))
	for _, module := range grant.Scope.Modules {
		modules[module] = struct{}{}
	}
	return &HostImportRegistrar{reg: r, modules: modules}, nil
}

// Module begins declarations in one granted exact module. Empty, parent, and
// wildcard-looking scopes are never inferred.
func (a *HostImportRegistrar) Module(name string) (*ImportModuleBuilder, error) {
	if a == nil || a.reg == nil {
		return nil, fmt.Errorf("wago: nil host-import registrar")
	}
	if err := a.reg.ensureOpen(); err != nil {
		return nil, err
	}
	if _, ok := a.modules[name]; !ok {
		return nil, &PluginError{Plugin: a.reg.definition.ID, Phase: PluginPhaseAuthorize, Authority: AuthorityHostImportDefine, Path: "scope.modules", Err: fmt.Errorf("module %q is outside the grant: %w", name, ErrPermissionDenied)}
	}
	return &ImportModuleBuilder{reg: a.reg, module: name}, nil
}

// GuestArgumentsAccess is a revocable read-only view of guest argv.
type GuestArgumentsAccess struct {
	mu     sync.RWMutex
	args   []string
	active bool
}

func (r *Registrar) GuestArguments() (*GuestArgumentsAccess, error) {
	if _, err := r.authorize(AuthorityHostArgumentsRead); err != nil {
		return nil, err
	}
	a := &GuestArgumentsAccess{}
	r.activate = append(r.activate, func(rt *Runtime) {
		a.mu.Lock()
		a.args = append([]string(nil), rt.guestArguments...)
		a.active = true
		a.mu.Unlock()
	})
	r.revoke = append(r.revoke, a.close)
	return a, nil
}

func (a *GuestArgumentsAccess) Args() ([]string, error) {
	if a == nil {
		return nil, fmt.Errorf("wago: nil guest-arguments access: %w", ErrPermissionDenied)
	}
	a.mu.RLock()
	args := append([]string(nil), a.args...)
	active := a.active
	a.mu.RUnlock()
	if !active {
		return nil, fmt.Errorf("wago: guest-arguments access is inactive: %w", ErrPermissionDenied)
	}
	if args == nil {
		args = []string{}
	}
	return args, nil
}

func (a *GuestArgumentsAccess) close() error {
	a.mu.Lock()
	a.args = nil
	a.active = false
	a.mu.Unlock()
	return nil
}

// HostCallers returns an identity-only, revocable resolver for synchronous host
// calls. It cannot create, invoke, or close instances.
func (r *Registrar) HostCallers() (*CallerResolver, error) {
	if _, err := r.authorize(AuthorityHostCallerIdentify); err != nil {
		return nil, err
	}
	a := &CallerResolver{}
	r.activate = append(r.activate, a.activate)
	r.revoke = append(r.revoke, a.close)
	return a, nil
}

type RuntimeCloseObserver struct{ reg *Registrar }

func (r *Registrar) RuntimeCloseObserver() (*RuntimeCloseObserver, error) {
	if _, err := r.authorize(AuthorityRuntimeCloseObserve); err != nil {
		return nil, err
	}
	return &RuntimeCloseObserver{reg: r}, nil
}

func (a *RuntimeCloseObserver) Observe(fns ...func(RuntimeCloseEvent)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.onRuntimeClose = append(a.reg.hooks.onRuntimeClose, fns...)
	return nil
}

type ModuleSourceTransformer struct{ reg *Registrar }

func (r *Registrar) ModuleSourceTransformer() (*ModuleSourceTransformer, error) {
	if _, err := r.authorize(AuthorityModuleSourceTransform); err != nil {
		return nil, err
	}
	return &ModuleSourceTransformer{reg: r}, nil
}

func (a *ModuleSourceTransformer) Transform(fns ...func(ModuleSourceContext, []byte) ([]byte, error)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.beforeCompile = append(a.reg.hooks.beforeCompile, fns...)
	return nil
}

type ModuleCompileObserver struct{ reg *Registrar }

func (r *Registrar) ModuleCompileObserver() (*ModuleCompileObserver, error) {
	if _, err := r.authorize(AuthorityModuleCompileObserve); err != nil {
		return nil, err
	}
	return &ModuleCompileObserver{reg: r}, nil
}

func (a *ModuleCompileObserver) Observe(fns ...func(ModuleCompiledEvent)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.afterCompile = append(a.reg.hooks.afterCompile, fns...)
	return nil
}

// OnError observes a failed source transform, compile, or successful-compile
// observer. Compilation matches the preceding ModuleSourceContext when source
// transformers ran, allowing plugins to discard correlation state.
func (a *ModuleCompileObserver) OnError(fns ...func(ModuleCompileErrorEvent)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.onCompileError = append(a.reg.hooks.onCompileError, fns...)
	return nil
}

// ModuleCloseObserver observes the logical close of runtime-built modules. The
// event is emitted once and does not transfer or close the underlying Compiled
// artifact.
type ModuleCloseObserver struct{ reg *Registrar }

func (r *Registrar) ModuleCloseObserver() (*ModuleCloseObserver, error) {
	if _, err := r.authorize(AuthorityModuleCloseObserve); err != nil {
		return nil, err
	}
	return &ModuleCloseObserver{reg: r}, nil
}

func (a *ModuleCloseObserver) Observe(fns ...func(ModuleCloseEvent)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.onModuleClose = append(a.reg.hooks.onModuleClose, fns...)
	return nil
}

type InstanceInstantiateInterceptor struct{ reg *Registrar }

func (r *Registrar) InstanceInstantiateInterceptor() (*InstanceInstantiateInterceptor, error) {
	if _, err := r.authorize(AuthorityInstanceInstantiateIntercept); err != nil {
		return nil, err
	}
	return &InstanceInstantiateInterceptor{reg: r}, nil
}

func (a *InstanceInstantiateInterceptor) Before(fns ...func(InstantiationRequest) error) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.beforeInstantiate = append(a.reg.hooks.beforeInstantiate, fns...)
	return nil
}

// After runs after the instance has been initialized and has an exact identity,
// but before its start function and successful-instantiation observers run. An
// error aborts instantiation and closes the partial instance through the normal
// lifecycle, allowing plugins to attach fallible per-instance state without
// using observer panics as control flow.
func (a *InstanceInstantiateInterceptor) After(fns ...func(InstantiationEvent) error) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.afterCreate = append(a.reg.hooks.afterCreate, fns...)
	return nil
}

type InstanceInstantiateObserver struct{ reg *Registrar }

func (r *Registrar) InstanceInstantiateObserver() (*InstanceInstantiateObserver, error) {
	if _, err := r.authorize(AuthorityInstanceInstantiateObserve); err != nil {
		return nil, err
	}
	return &InstanceInstantiateObserver{reg: r}, nil
}

func (a *InstanceInstantiateObserver) After(fns ...func(InstantiationEvent)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.afterInstantiate = append(a.reg.hooks.afterInstantiate, fns...)
	return nil
}

func (a *InstanceInstantiateObserver) OnError(fns ...func(InstantiationErrorEvent)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.onInstantiateError = append(a.reg.hooks.onInstantiateError, fns...)
	return nil
}

type InstanceCloseObserver struct{ reg *Registrar }

func (r *Registrar) InstanceCloseObserver() (*InstanceCloseObserver, error) {
	if _, err := r.authorize(AuthorityInstanceCloseObserve); err != nil {
		return nil, err
	}
	return &InstanceCloseObserver{reg: r}, nil
}

func (a *InstanceCloseObserver) Before(fns ...func(InstanceCloseEvent)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.beforeClose = append(a.reg.hooks.beforeClose, fns...)
	return nil
}
func (a *InstanceCloseObserver) After(fns ...func(InstanceCloseEvent)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.afterClose = append(a.reg.hooks.afterClose, fns...)
	return nil
}

type InstanceInvokeInterceptor struct{ reg *Registrar }

func (r *Registrar) InstanceInvokeInterceptor() (*InstanceInvokeInterceptor, error) {
	if _, err := r.authorize(AuthorityInstanceInvokeIntercept); err != nil {
		return nil, err
	}
	return &InstanceInvokeInterceptor{reg: r}, nil
}

func (a *InstanceInvokeInterceptor) Before(fns ...func(InvocationRequest) error) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.beforeInvoke = append(a.reg.hooks.beforeInvoke, fns...)
	return nil
}

type InstanceInvokeObserver struct{ reg *Registrar }

func (r *Registrar) InstanceInvokeObserver() (*InstanceInvokeObserver, error) {
	if _, err := r.authorize(AuthorityInstanceInvokeObserve); err != nil {
		return nil, err
	}
	return &InstanceInvokeObserver{reg: r}, nil
}

func (a *InstanceInvokeObserver) After(fns ...func(InvocationEvent)) error {
	if err := a.reg.ensureOpen(); err != nil {
		return err
	}
	a.reg.hooks.afterInvoke = append(a.reg.hooks.afterInvoke, fns...)
	return nil
}

type revocableRuntime struct {
	mu sync.RWMutex
	rt *Runtime
}

func (a *revocableRuntime) activate(rt *Runtime) {
	a.mu.Lock()
	a.rt = rt
	a.mu.Unlock()
}

func (a *revocableRuntime) close() error {
	a.mu.Lock()
	a.rt = nil
	a.mu.Unlock()
	return nil
}

func (a *revocableRuntime) runtime(label string) (*Runtime, error) {
	if a == nil {
		return nil, fmt.Errorf("wago: nil %s access: %w", label, ErrPermissionDenied)
	}
	a.mu.RLock()
	rt := a.rt
	a.mu.RUnlock()
	if rt == nil {
		return nil, fmt.Errorf("wago: %s access is inactive: %w", label, ErrPermissionDenied)
	}
	return rt, nil
}

type CoreModuleCompiler struct{ state revocableRuntime }

func (r *Registrar) CoreModuleCompiler() (*CoreModuleCompiler, error) {
	if _, err := r.authorize(AuthorityCoreModuleCompile); err != nil {
		return nil, err
	}
	a := &CoreModuleCompiler{}
	r.activate = append(r.activate, a.state.activate)
	r.revoke = append(r.revoke, a.state.close)
	return a, nil
}

func (a *CoreModuleCompiler) Compile(source []byte) (*Module, error) {
	rt, err := a.state.runtime("core module compile")
	if err != nil {
		return nil, err
	}
	return rt.compilePlugin(source)
}

type CoreInstanceInstantiator struct{ manager *InstanceManager }

func (r *Registrar) CoreInstanceInstantiator() (*CoreInstanceInstantiator, error) {
	grant, err := r.authorize(AuthorityCoreInstanceInstantiate)
	if err != nil {
		return nil, err
	}
	m := newPendingInstanceManager(r.definition.ID, grant.Scope)
	a := &CoreInstanceInstantiator{manager: m}
	r.activate = append(r.activate, m.activate)
	r.closeInstances = append(r.closeInstances, m.closeLogical)
	r.drainInstances = append(r.drainInstances, m.drain)
	return a, nil
}

func (a *CoreInstanceInstantiator) Instantiate(ctx context.Context, mod *Module, opts ...InstantiateOption) (*ManagedInstance, error) {
	if a == nil || a.manager == nil {
		return nil, fmt.Errorf("wago: nil core instance instantiator: %w", ErrPermissionDenied)
	}
	return a.manager.Instantiate(ctx, mod, opts...)
}

type CoreFuncRefFactory struct {
	state revocableRuntime
	gate  *pluginCallGate
}

func (r *Registrar) CoreFuncRefFactory() (*CoreFuncRefFactory, error) {
	if _, err := r.authorize(AuthorityCoreFuncRefCreate); err != nil {
		return nil, err
	}
	a := &CoreFuncRefFactory{gate: r.callGate}
	r.activate = append(r.activate, a.state.activate)
	r.revoke = append(r.revoke, a.state.close)
	return a, nil
}

func (a *CoreFuncRefFactory) New(fn HostFunc, sig FuncSig) (*HostFuncRef, error) {
	rt, err := a.state.runtime("core funcref create")
	if err != nil {
		return nil, err
	}
	return rt.newHostFuncRef(a.gate.wrap(fn), sig, false, true)
}
