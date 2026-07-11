package wago

import "fmt"

func (r *Registry) authorize(cap PluginCapability) error {
	if r == nil {
		return fmt.Errorf("wago: nil plugin registry")
	}
	if r.used == nil {
		r.used = map[PluginCapability]struct{}{}
	}
	r.used[cap] = struct{}{}
	if r.grants != nil && !r.Granted(cap) {
		return fmt.Errorf("plugin capability %q was not granted: %w", cap, ErrPermissionDenied)
	}
	return nil
}

type HostImportAccess struct{ reg *Registry }

func (r *Registry) HostImports() (*HostImportAccess, error) {
	if err := r.authorize(PluginHostImports); err != nil {
		return nil, err
	}
	return &HostImportAccess{reg: r}, nil
}

func (a *HostImportAccess) Module(name string) *ImportModuleBuilder {
	return a.reg.ImportModule(name)
}

type RuntimeHookAccess struct{ hooks *HookRegistry }

func (r *Registry) RuntimeLifecycle() (*RuntimeHookAccess, error) {
	if err := r.authorize(PluginRuntimeHooks); err != nil {
		return nil, err
	}
	return &RuntimeHookAccess{r.hooks}, nil
}

func (a *RuntimeHookAccess) OnClose(fns ...func(*RuntimeContext)) {
	a.hooks.OnRuntimeClose(fns...)
}

type CompileHookAccess struct{ hooks *HookRegistry }

func (r *Registry) ModuleCompiler() (*CompileHookAccess, error) {
	if err := r.authorize(PluginCompileHooks); err != nil {
		return nil, err
	}
	return &CompileHookAccess{r.hooks}, nil
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
	return &InstanceHookAccess{r.hooks}, nil
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
func (a *InstanceHookAccess) BeforeClose(fns ...func(*InstanceContext)) {
	a.hooks.BeforeClose(fns...)
}
func (a *InstanceHookAccess) AfterClose(fns ...func(*InstanceContext)) {
	a.hooks.AfterClose(fns...)
}

type InvokeHookAccess struct{ hooks *HookRegistry }

func (r *Registry) InstanceInvocation() (*InvokeHookAccess, error) {
	if err := r.authorize(PluginInvokeHooks); err != nil {
		return nil, err
	}
	return &InvokeHookAccess{r.hooks}, nil
}

func (a *InvokeHookAccess) Before(fns ...func(*InvokeContext) error) {
	a.hooks.BeforeInvoke(fns...)
}
func (a *InvokeHookAccess) After(fns ...func(*InvokeContext, []Value, error)) {
	a.hooks.AfterInvoke(fns...)
}
