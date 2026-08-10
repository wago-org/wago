// Package engine adapts Wago's core WebAssembly runtime to the small runtime
// surface needed by the Component Model linker. Keeping this boundary private
// prevents component code from bypassing Runtime policy and lifecycle checks.
package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/wago-org/wago/src/component/internal/engine/expctxkeys"
	core "github.com/wago-org/wago/src/wago"
)

type ValueType = byte

// activeCallerKey carries the exact core module parked in a component host
// callback. Canonical lowering can synchronously call a guest realloc function;
// retaining this identity lets that call use Wago's isolated re-entry path when
// realloc belongs to a module already active in the same component call.
type activeCallerKey struct{}

const (
	ValueTypeI32 ValueType = 0x7f
	ValueTypeI64 ValueType = 0x7e
	ValueTypeF32 ValueType = 0x7d
	ValueTypeF64 ValueType = 0x7c
)

func EncodeU32(v uint32) uint64 { return uint64(v) }
func DecodeU32(v uint64) uint32 { return uint32(v) }

func ExternTypeName(v byte) string {
	switch v {
	case 0:
		return "func"
	case 1:
		return "table"
	case 2:
		return "memory"
	case 3:
		return "global"
	default:
		return fmt.Sprintf("%#x", v)
	}
}

type FunctionDefinition interface {
	ParamTypes() []ValueType
	ResultTypes() []ValueType
}

type functionDefinition struct{ params, results []ValueType }

func (d functionDefinition) ParamTypes() []ValueType  { return append([]ValueType(nil), d.params...) }
func (d functionDefinition) ResultTypes() []ValueType { return append([]ValueType(nil), d.results...) }

type Function interface {
	Definition() FunctionDefinition
	Call(context.Context, ...uint64) ([]uint64, error)
	CallWithStack(context.Context, []uint64) error
}

type Memory interface {
	Size() uint32
	Read(uint32, uint32) ([]byte, bool)
}

type Global interface {
	Type() ValueType
	Get() uint64
}

type MutableGlobal interface {
	Global
	Set(uint64)
}

type Module interface {
	Name() string
	Memory() Memory
	ExportedFunction(string) Function
	ExportedFunctionDefinitions() map[string]FunctionDefinition
	ExportedMemory(string) Memory
	ExportedGlobal(string) Global
	Close(context.Context) error
}

type GoModuleFunction interface {
	Call(context.Context, Module, []uint64)
}

type GoModuleFunc func(context.Context, Module, []uint64)

func (f GoModuleFunc) Call(ctx context.Context, m Module, stack []uint64) { f(ctx, m, stack) }

type ImportResolver func(name string) Module

func WithImportResolver(ctx context.Context, resolver ImportResolver) context.Context {
	return context.WithValue(ctx, expctxkeys.ImportResolverKey{}, resolver)
}
func resolverFromContext(ctx context.Context) ImportResolver {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(expctxkeys.ImportResolverKey{}).(ImportResolver)
	return r
}

type ModuleConfig struct{ name string }

func NewModuleConfig() ModuleConfig                              { return ModuleConfig{} }
func (c ModuleConfig) WithName(name string) ModuleConfig         { c.name = name; return c }
func (c ModuleConfig) WithStartFunctions(...string) ModuleConfig { return c }

type CompiledModule interface{ Close(context.Context) error }

type Runtime interface {
	InstantiateWithConfig(context.Context, []byte, ModuleConfig) (Module, error)
	CompileModule(context.Context, []byte) (CompiledModule, error)
	InstantiateModule(context.Context, CompiledModule, ModuleConfig) (Module, error)
	NewHostModuleBuilder(string) HostModuleBuilder
	InspectModuleImports(context.Context, []byte) ([]CoreFuncImport, error)
}

type CoreFuncImport struct {
	Module, Name    string
	Params, Results []ValueType
}

type runtimeAdapter struct{ rt core.CoreEngine }

func Wrap(rt core.CoreEngine) Runtime { return &runtimeAdapter{rt: rt} }

type compiledModule struct{ mod *core.Module }

func (c *compiledModule) Close(context.Context) error { return c.mod.Close() }

func (r *runtimeAdapter) CompileModule(ctx context.Context, source []byte) (CompiledModule, error) {
	if r == nil || r.rt == nil {
		return nil, fmt.Errorf("component: nil Wago runtime")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	m, err := r.rt.Compile(append([]byte(nil), source...))
	if err != nil {
		return nil, err
	}
	return &compiledModule{mod: m}, nil
}

func (r *runtimeAdapter) InspectModuleImports(ctx context.Context, source []byte) ([]CoreFuncImport, error) {
	c, err := r.CompileModule(ctx, source)
	if err != nil {
		return nil, err
	}
	cm := c.(*compiledModule)
	imports := cm.mod.Imports()
	out := make([]CoreFuncImport, 0, len(imports))
	for _, f := range imports {
		if f.Kind != core.ImportFunc {
			continue
		}
		out = append(out, CoreFuncImport{Module: f.Module, Name: f.Name, Params: valTypes(f.Params), Results: valTypes(f.Results)})
	}
	return out, nil
}

func (r *runtimeAdapter) InstantiateWithConfig(ctx context.Context, source []byte, cfg ModuleConfig) (Module, error) {
	c, err := r.CompileModule(ctx, source)
	if err != nil {
		return nil, err
	}
	return r.InstantiateModule(ctx, c, cfg)
}

func (r *runtimeAdapter) InstantiateModule(ctx context.Context, c CompiledModule, cfg ModuleConfig) (Module, error) {
	cm, ok := c.(*compiledModule)
	if !ok || cm == nil || cm.mod == nil {
		return nil, fmt.Errorf("component: compiled module belongs to another runtime")
	}
	imports := core.Imports{}
	resolvedFuncs := map[string]Function{}
	hostRefs := make([]*core.HostFuncRef, 0)
	closeHostRefs := func() {
		for i := len(hostRefs) - 1; i >= 0; i-- {
			_ = hostRefs[i].Close()
		}
	}
	resolver := resolverFromContext(ctx)
	for _, spec := range cm.mod.Imports() {
		if resolver == nil {
			continue
		}
		provider := resolver(spec.Module)
		if provider == nil {
			continue
		}
		switch spec.Kind {
		case core.ImportFunc:
			fn := provider.ExportedFunction(spec.Name)
			if fn == nil {
				continue
			}
			resolvedFuncs[spec.Key()] = fn
			var host core.HostFunc
			if hf, ok := fn.(*hostFunction); ok {
				host = core.HostFunc(func(caller core.HostModule, params, results []uint64) {
					stack := make([]uint64, max(len(params), len(results)))
					copy(stack, params)
					callCtx := context.WithValue(ctx, activeCallerKey{}, caller)
					hf.fn.Call(callCtx, callerModule{caller: caller}, stack)
					copy(results, stack)
				})
			} else if wf, ok := fn.(*wasmFunction); ok {
				host = core.HostFunc(func(caller core.HostModule, params, results []uint64) {
					out, callErr := wf.mod.in.InvokeFromHost(ctx, caller, wf.name, params...)
					if callErr != nil {
						panic(core.HostTrap{Err: callErr})
					}
					copy(results, out)
				})
			}
			if host != nil {
				owner, err := r.rt.NewHostFuncRef(host, core.FuncSig{
					Params:  append([]core.ValType(nil), spec.Params...),
					Results: append([]core.ValType(nil), spec.Results...),
				})
				if err != nil {
					closeHostRefs()
					return nil, fmt.Errorf("component: own core host import %q: %w", spec.Key(), err)
				}
				hostRefs = append(hostRefs, owner)
				imports[spec.Key()] = owner
			}
		case core.ImportMemory:
			if wm, ok := provider.ExportedMemory(spec.Name).(*memory); ok {
				imports[spec.Key()] = wm.mem
			}
		case core.ImportGlobal:
			if wg, ok := provider.ExportedGlobal(spec.Name).(*global); ok {
				imports[spec.Key()] = wg.g
			}
		case core.ImportTable:
			if wm, ok := provider.(*module); ok {
				t, err := wm.in.ExportedTable(spec.Name)
				if err == nil {
					imports[spec.Key()] = t
				}
			}
		}
	}
	in, err := r.rt.Instantiate(ctx, cm.mod, core.WithImports(imports), core.WithSynchronousHostCalls())
	if err != nil {
		closeHostRefs()
		return nil, err
	}
	return newModule(cfg.name, in, cm.mod, resolvedFuncs, hostRefs), nil
}

type module struct {
	name      string
	in        *core.Instance
	defs      map[string]FunctionDefinition
	forwarded map[string]Function
	hostRefs  []*core.HostFuncRef
}

func newModule(name string, in *core.Instance, compiled *core.Module, resolved map[string]Function, hostRefs []*core.HostFuncRef) *module {
	m := &module{name: name, in: in, defs: map[string]FunctionDefinition{}, forwarded: map[string]Function{}, hostRefs: hostRefs}
	for _, f := range compiled.Metadata().Functions {
		for _, export := range f.Exports {
			m.defs[export] = functionDefinition{params: valTypes(f.Params), results: valTypes(f.Results)}
		}
	}
	c := compiled.Compiled()
	for export, index := range c.Exports {
		if index >= 0 && index < c.NumImports && index < len(c.Imports) {
			if fn := resolved[c.Imports[index]]; fn != nil {
				m.forwarded[export] = fn
			}
		}
	}
	return m
}
func valTypes(v []core.ValType) []ValueType {
	out := make([]ValueType, len(v))
	for i := range v {
		switch v[i] {
		case core.ValI32:
			out[i] = ValueTypeI32
		case core.ValI64:
			out[i] = ValueTypeI64
		case core.ValF32:
			out[i] = ValueTypeF32
		case core.ValF64:
			out[i] = ValueTypeF64
		default:
			out[i] = byte(v[i])
		}
	}
	return out
}
func (m *module) Name() string { return m.name }
func (m *module) Memory() Memory {
	if x := m.in.Memory(); x != nil {
		return &memory{mem: x}
	}
	return nil
}
func (m *module) ExportedMemory(name string) Memory {
	x, err := m.in.ExportedMemory(name)
	if err != nil {
		return nil
	}
	return &memory{mem: x}
}
func (m *module) ExportedFunction(name string) Function {
	if fn := m.forwarded[name]; fn != nil {
		return fn
	}
	d, ok := m.defs[name]
	if !ok {
		return nil
	}
	return &wasmFunction{mod: m, name: name, def: d}
}
func (m *module) ExportedFunctionDefinitions() map[string]FunctionDefinition {
	out := make(map[string]FunctionDefinition, len(m.defs))
	for k, v := range m.defs {
		out[k] = v
	}
	return out
}
func (m *module) ExportedGlobal(name string) Global {
	g, err := m.in.ExportedGlobalObject(name)
	if err != nil {
		return nil
	}
	return &global{g: g}
}
func (m *module) Close(context.Context) error {
	err := m.in.Close()
	for i := len(m.hostRefs) - 1; i >= 0; i-- {
		err = errors.Join(err, m.hostRefs[i].Close())
	}
	m.hostRefs = nil
	return err
}

type callerModule struct{ caller core.HostModule }

func (m callerModule) Name() string { return "" }
func (m callerModule) Memory() Memory {
	if m.caller == nil {
		return nil
	}
	return byteMemory(m.caller.Memory())
}
func (callerModule) ExportedFunction(string) Function                           { return nil }
func (callerModule) ExportedFunctionDefinitions() map[string]FunctionDefinition { return nil }
func (callerModule) ExportedMemory(string) Memory                               { return nil }
func (callerModule) ExportedGlobal(string) Global                               { return nil }
func (callerModule) Close(context.Context) error                                { return nil }

type wasmFunction struct {
	mod  *module
	name string
	def  FunctionDefinition
}

func (f *wasmFunction) Definition() FunctionDefinition { return f.def }
func (f *wasmFunction) Call(ctx context.Context, p ...uint64) ([]uint64, error) {
	if caller, ok := ctx.Value(activeCallerKey{}).(core.HostModule); ok && caller != nil {
		return f.mod.in.InvokeFromHost(ctx, caller, f.name, p...)
	}
	return f.mod.in.InvokeContext(ctx, f.name, p...)
}
func (f *wasmFunction) CallWithStack(ctx context.Context, stack []uint64) error {
	out, err := f.Call(ctx, stack[:len(f.def.ParamTypes())]...)
	if err == nil {
		copy(stack, out)
	}
	return err
}

type memory struct{ mem *core.Memory }

func (m *memory) Size() uint32 {
	if m == nil || m.mem == nil {
		return 0
	}
	n := len(m.mem.Bytes())
	if uint64(n) > math.MaxUint32 {
		return 0
	}
	return uint32(n)
}
func (m *memory) Read(off, n uint32) ([]byte, bool) {
	if m == nil || m.mem == nil {
		return nil, false
	}
	b := m.mem.Bytes()
	end := uint64(off) + uint64(n)
	if end > uint64(len(b)) {
		return nil, false
	}
	return b[off:end], true
}

type byteMemory []byte

func (m byteMemory) Size() uint32 {
	if uint64(len(m)) > math.MaxUint32 {
		return 0
	}
	return uint32(len(m))
}
func (m byteMemory) Read(off, n uint32) ([]byte, bool) {
	end := uint64(off) + uint64(n)
	if end > uint64(len(m)) {
		return nil, false
	}
	return m[off:end], true
}

type global struct{ g *core.Global }

func (g *global) Type() ValueType { return valTypes([]core.ValType{g.g.Type})[0] }
func (g *global) Get() uint64     { return g.g.Get() }
func (g *global) Set(v uint64)    { _ = g.g.Set(v) }

var _ = binary.LittleEndian

type HostModuleBuilder struct {
	rt      *runtimeAdapter
	name    string
	funcs   map[string]*hostFunction
	pending *hostFunction
}
type HostFunctionBuilder struct{ parent HostModuleBuilder }

func (r *runtimeAdapter) NewHostModuleBuilder(name string) HostModuleBuilder {
	return HostModuleBuilder{rt: r, name: name, funcs: map[string]*hostFunction{}}
}
func (b HostModuleBuilder) NewFunctionBuilder() HostFunctionBuilder {
	return HostFunctionBuilder{parent: b}
}
func (b HostFunctionBuilder) WithGoModuleFunction(fn GoModuleFunction, params, results []ValueType) HostFunctionBuilder {
	b.parent.pending = &hostFunction{fn: fn, def: functionDefinition{params: append([]ValueType(nil), params...), results: append([]ValueType(nil), results...)}}
	return b
}
func (b HostFunctionBuilder) Export(name string) HostModuleBuilder {
	if b.parent.pending != nil {
		b.parent.funcs[name] = b.parent.pending
		b.parent.pending = nil
	}
	return b.parent
}
func (b HostModuleBuilder) Instantiate(context.Context) (Module, error) {
	if b.name == "" {
		return nil, fmt.Errorf("component: host module name is empty")
	}
	return &hostModule{name: b.name, funcs: b.funcs}, nil
}

type hostModule struct {
	name  string
	funcs map[string]*hostFunction
}

func (m *hostModule) Name() string                       { return m.name }
func (m *hostModule) Memory() Memory                     { return nil }
func (m *hostModule) ExportedFunction(n string) Function { return m.funcs[n] }
func (m *hostModule) ExportedFunctionDefinitions() map[string]FunctionDefinition {
	out := map[string]FunctionDefinition{}
	for k, v := range m.funcs {
		out[k] = v.def
	}
	return out
}
func (m *hostModule) ExportedMemory(string) Memory { return nil }
func (m *hostModule) ExportedGlobal(string) Global { return nil }
func (m *hostModule) Close(context.Context) error  { return nil }

type hostFunction struct {
	fn  GoModuleFunction
	def FunctionDefinition
}

func (f *hostFunction) Definition() FunctionDefinition { return f.def }
func (f *hostFunction) Call(context.Context, ...uint64) ([]uint64, error) {
	return nil, fmt.Errorf("component: host function cannot be called directly")
}
func (f *hostFunction) CallWithStack(context.Context, []uint64) error {
	return fmt.Errorf("component: host function cannot be called directly")
}
