package wago

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

type hookRegistry struct {
	operationGates      []*pluginCallGate
	onRuntimeClose      []func(RuntimeCloseEvent)
	internalClose       []func() error
	internalBeforeClose []func(*Instance)
	beforeCompile       []func(ModuleSourceContext, []byte) ([]byte, error)
	afterCompile        []func(ModuleCompiledEvent)
	onCompileError      []func(ModuleCompileErrorEvent)
	onModuleClose       []func(ModuleCloseEvent)
	beforeInstantiate   []func(InstantiationRequest) error
	afterCreate         []func(InstantiationEvent) error
	afterInstantiate    []func(InstantiationEvent)
	onInstantiateError  []func(InstantiationErrorEvent)
	beforeClose         []func(InstanceCloseEvent)
	afterClose          []func(InstanceCloseEvent)
	beforeInvoke        []func(InvocationRequest) error
	afterInvoke         []func(InvocationEvent)
}

// InstanceIdentity is an opaque comparable identity. It conveys no invocation,
// close, memory, export, or management authority.
type InstanceIdentity struct{ value *Instance }

func (id InstanceIdentity) IsZero() bool                  { return id.value == nil }
func sameInstance(id InstanceIdentity, in *Instance) bool { return id.value == in }

type compilationIdentityToken struct{ _ byte }

// CompilationIdentity correlates one Runtime.Compile or Runtime.Module
// operation without exposing source, compiled code, or Runtime authority.
type CompilationIdentity struct{ value *compilationIdentityToken }

func (id CompilationIdentity) IsZero() bool { return id.value == nil }

type moduleIdentityToken struct{ _ byte }

// ModuleIdentity is the opaque comparable identity of one runtime-bound
// module. It grants no access to code, imports, instantiation, or close
// operations and does not retain the underlying Compiled artifact.
type ModuleIdentity struct{ value *moduleIdentityToken }

func (id ModuleIdentity) IsZero() bool { return id.value == nil }

// ModuleSourceDigest is an opaque comparable digest of exact Wasm source
// bytes. Its representation is deliberately unavailable to plugins: source
// transformers can compare values without a compile observer learning source.
// The zero value means that no source bytes were available, as for
// Runtime.Module.
type ModuleSourceDigest struct {
	value     [sha256.Size]byte
	available bool
}

// DigestModuleSource returns the opaque digest of source the caller already
// holds. ModuleSourceDigest values are intended to be compared with == or !=.
func DigestModuleSource(source []byte) ModuleSourceDigest {
	return ModuleSourceDigest{value: sha256.Sum256(source), available: true}
}

func (d ModuleSourceDigest) IsZero() bool { return !d.available }

type ModuleView struct {
	compiled *Compiled
	identity ModuleIdentity
	imports  []ImportSpec
}

func (v ModuleView) Identity() ModuleIdentity { return v.identity }

func (v ModuleView) Imports() []ImportSpec {
	return cloneImportSpecs(v.imports)
}

func moduleView(mod *Module) ModuleView {
	if mod == nil {
		return ModuleView{}
	}
	return ModuleView{compiled: mod.c, identity: mod.moduleIdentity(), imports: mod.imports}
}

func cloneImportSpecs(in []ImportSpec) []ImportSpec {
	out := append([]ImportSpec(nil), in...)
	for i := range out {
		out[i].Params = append([]ValType(nil), in[i].Params...)
		out[i].Results = append([]ValType(nil), in[i].Results...)
		out[i].ParamTypes = append([]ValueTypeDescriptor(nil), in[i].ParamTypes...)
		out[i].ResultTypes = append([]ValueTypeDescriptor(nil), in[i].ResultTypes...)
	}
	return out
}

type RuntimeCloseEvent struct{}
type ModuleSourceContext struct{ Compilation CompilationIdentity }
type ModuleCompiledEvent struct {
	Compilation CompilationIdentity
	Module      ModuleView
	// SourceDigest identifies the final bytes passed to the compiler after every
	// source transformer. It is zero for Runtime.Module's precompiled path.
	SourceDigest ModuleSourceDigest
}
type ModuleCompileErrorEvent struct {
	Compilation CompilationIdentity
	Err         error
}
type ModuleCloseEvent struct{ Module ModuleView }
type InstantiationRequest struct {
	Module      ModuleView
	Origin      InstantiateOrigin
	reservation *pluginOperationReservation
}
type InstantiationEvent struct {
	Module      ModuleView
	Instance    InstanceIdentity
	Origin      InstantiateOrigin
	reservation *pluginOperationReservation
}
type InstantiationErrorEvent struct {
	Module      ModuleView
	Origin      InstantiateOrigin
	Err         error
	reservation *pluginOperationReservation
}
type InstanceCloseEvent struct {
	Module   ModuleView
	Instance InstanceIdentity
	Origin   InstantiateOrigin
}
type operationIdentityToken struct{ _ byte }
type OperationIdentity struct{ value *operationIdentityToken }

type InvocationRequest struct {
	Operation   OperationIdentity
	Instance    InstanceIdentity
	Export      string
	Args        []Value
	Start       time.Time
	reservation *pluginOperationReservation
}
type InvocationEvent struct {
	Operation   OperationIdentity
	Instance    InstanceIdentity
	Export      string
	Results     []Value
	Err         error
	Start       time.Time
	reservation *pluginOperationReservation
}

type InstantiateOrigin uint8

const (
	InstantiateDirect InstantiateOrigin = iota
	InstantiateManaged
)

func (h *hookRegistry) clone() *hookRegistry {
	if h == nil {
		return &hookRegistry{}
	}
	return &hookRegistry{
		operationGates:      append([]*pluginCallGate(nil), h.operationGates...),
		onRuntimeClose:      append([]func(RuntimeCloseEvent){}, h.onRuntimeClose...),
		internalClose:       append([]func() error{}, h.internalClose...),
		internalBeforeClose: append([]func(*Instance){}, h.internalBeforeClose...),
		beforeCompile:       append([]func(ModuleSourceContext, []byte) ([]byte, error){}, h.beforeCompile...),
		afterCompile:        append([]func(ModuleCompiledEvent){}, h.afterCompile...),
		onCompileError:      append([]func(ModuleCompileErrorEvent){}, h.onCompileError...),
		onModuleClose:       append([]func(ModuleCloseEvent){}, h.onModuleClose...),
		beforeInstantiate:   append([]func(InstantiationRequest) error{}, h.beforeInstantiate...),
		afterCreate:         append([]func(InstantiationEvent) error{}, h.afterCreate...),
		afterInstantiate:    append([]func(InstantiationEvent){}, h.afterInstantiate...),
		onInstantiateError:  append([]func(InstantiationErrorEvent){}, h.onInstantiateError...),
		beforeClose:         append([]func(InstanceCloseEvent){}, h.beforeClose...),
		afterClose:          append([]func(InstanceCloseEvent){}, h.afterClose...),
		beforeInvoke:        append([]func(InvocationRequest) error{}, h.beforeInvoke...),
		afterInvoke:         append([]func(InvocationEvent){}, h.afterInvoke...),
	}
}

func (h *hookRegistry) appendGated(src *hookRegistry, gate *pluginCallGate) {
	if src == nil {
		return
	}
	h.operationGates = append(h.operationGates, gate)
	for _, fn := range src.onRuntimeClose {
		fn := fn
		h.onRuntimeClose = append(h.onRuntimeClose, func(event RuntimeCloseEvent) { withPluginHook(gate, func() { fn(event) }) })
	}
	for _, fn := range src.beforeCompile {
		fn := fn
		h.beforeCompile = append(h.beforeCompile, func(ctx ModuleSourceContext, source []byte) (out []byte, err error) {
			err = withPluginHookError(gate, func() error { out, err = fn(ctx, source); return err })
			return out, err
		})
	}
	for _, fn := range src.afterCompile {
		fn := fn
		h.afterCompile = append(h.afterCompile, func(event ModuleCompiledEvent) { withPluginHook(gate, func() { fn(event) }) })
	}
	for _, fn := range src.onCompileError {
		fn := fn
		h.onCompileError = append(h.onCompileError, func(event ModuleCompileErrorEvent) { withPluginHook(gate, func() { fn(event) }) })
	}
	for _, fn := range src.onModuleClose {
		fn := fn
		h.onModuleClose = append(h.onModuleClose, func(event ModuleCloseEvent) { withPluginHook(gate, func() { fn(event) }) })
	}
	for _, fn := range src.beforeInstantiate {
		fn := fn
		h.beforeInstantiate = append(h.beforeInstantiate, func(request InstantiationRequest) error {
			return withReservedPluginHookError(gate, request.reservation, func() error { return fn(request) })
		})
	}
	for _, fn := range src.afterCreate {
		fn := fn
		h.afterCreate = append(h.afterCreate, func(event InstantiationEvent) error {
			return withReservedPluginHookError(gate, event.reservation, func() error { return fn(event) })
		})
	}
	for _, fn := range src.afterInstantiate {
		fn := fn
		h.afterInstantiate = append(h.afterInstantiate, func(event InstantiationEvent) { withReservedPluginHook(gate, event.reservation, func() { fn(event) }) })
	}
	for _, fn := range src.onInstantiateError {
		fn := fn
		h.onInstantiateError = append(h.onInstantiateError, func(event InstantiationErrorEvent) {
			withReservedPluginHook(gate, event.reservation, func() { fn(event) })
		})
	}
	for _, fn := range src.beforeClose {
		fn := fn
		h.beforeClose = append(h.beforeClose, func(event InstanceCloseEvent) { withPluginHook(gate, func() { fn(event) }) })
	}
	for _, fn := range src.afterClose {
		fn := fn
		h.afterClose = append(h.afterClose, func(event InstanceCloseEvent) { withPluginHook(gate, func() { fn(event) }) })
	}
	for _, fn := range src.beforeInvoke {
		fn := fn
		h.beforeInvoke = append(h.beforeInvoke, func(request InvocationRequest) error {
			return withReservedPluginHookError(gate, request.reservation, func() error { return fn(request) })
		})
	}
	for _, fn := range src.afterInvoke {
		fn := fn
		h.afterInvoke = append(h.afterInvoke, func(event InvocationEvent) { withReservedPluginHook(gate, event.reservation, func() { fn(event) }) })
	}
}

func withPluginHook(gate *pluginCallGate, fn func()) {
	if err := gate.enter(); err != nil {
		return
	}
	defer gate.release()
	fn()
}

func withPluginHookError(gate *pluginCallGate, fn func() error) error {
	if err := gate.enter(); err != nil {
		return err
	}
	defer gate.release()
	return fn()
}

func withReservedPluginHook(gate *pluginCallGate, reservation *pluginOperationReservation, fn func()) {
	if reservation != nil && reservation.allows(gate) {
		fn()
		return
	}
	withPluginHook(gate, fn)
}

func withReservedPluginHookError(gate *pluginCallGate, reservation *pluginOperationReservation, fn func() error) error {
	if reservation != nil && reservation.allows(gate) {
		return fn()
	}
	return withPluginHookError(gate, fn)
}

func callHookSafely(phase string, fn func()) (err error) {
	return callSafely(phase+" hook", fn)
}

func callShutdownSafely(label string, fn func()) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("wago: %s: %w", label, ErrCallbackPanic)
		}
	}()
	fn()
	return nil
}

func callSafely(label string, fn func()) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if panicErr, ok := recovered.(error); ok {
				err = fmt.Errorf("wago: %s panicked: %w", label, panicErr)
			} else {
				err = fmt.Errorf("wago: %s panicked: %v", label, recovered)
			}
		}
	}()
	fn()
	return nil
}
func joinPrimary(primary error, additional ...error) error {
	joined := make([]error, 0, len(additional)+1)
	if primary != nil {
		joined = append(joined, primary)
	}
	for _, err := range additional {
		if err != nil {
			joined = append(joined, err)
		}
	}
	switch len(joined) {
	case 0:
		return nil
	case 1:
		return joined[0]
	default:
		return errors.Join(joined...)
	}
}
