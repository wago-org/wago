package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreplugins "github.com/wago-org/wago/src/core/plugins"
)

// CompilerTypeRegistrar declares custom compiler value types under the exact
// compiler.type.define authority.
type CompilerTypeRegistrar struct {
	reg        *Registrar
	namespaces map[string]struct{}
}

// CompilerInstructionRegistrar declares and lowers custom Wasm instructions in
// exact module namespaces granted to compiler.instruction.define.
type CompilerInstructionRegistrar struct {
	reg     *Registrar
	modules map[string]struct{}
}

type InstructionSpec = coreplugins.InstructionSpec
type InstructionHandler = coreplugins.InstructionHandler
type InstructionContext = coreplugins.InstructionContext
type Bits = coreplugins.Bits
type LowerValue = coreplugins.LowerValue
type InstructionLowerer = coreplugins.InstructionLowerer
type LoweringContext = coreplugins.LoweringContext
type WasmType = coreplugins.WasmType
type CustomTypeSpec = coreplugins.CustomTypeSpec
type CustomType = coreplugins.CustomType
type CustomSignature = coreplugins.CustomSignature

const (
	WasmI32       = coreplugins.WasmI32
	WasmI64       = coreplugins.WasmI64
	WasmF32       = coreplugins.WasmF32
	WasmF64       = coreplugins.WasmF64
	WasmV128      = coreplugins.WasmV128
	WasmFuncRef   = coreplugins.WasmFuncRef
	WasmExternRef = coreplugins.WasmExternRef
)

func NewBits(width int32, littleEndian []byte) (Bits, error) {
	return coreplugins.NewBits(width, littleEndian)
}

func BitsFromUint32(width int32, value uint32) (Bits, error) {
	return coreplugins.BitsFromUint32(width, value)
}

type registeredInstruction struct {
	spec       InstructionSpec
	definition coreplugins.Definition
}

// Type registers a plugin-owned logical value type and returns its opaque
// identity token. Repeating an identical declaration is idempotent; reusing a
// name with a different size or carrier is rejected.
func (r *Registrar) CompilerTypes() (*CompilerTypeRegistrar, error) {
	grant, err := r.authorize(AuthorityCompilerTypeDefine)
	if err != nil {
		return nil, err
	}
	namespaces := make(map[string]struct{}, len(grant.Scope.Modules))
	for _, namespace := range grant.Scope.Modules {
		namespaces[namespace] = struct{}{}
	}
	return &CompilerTypeRegistrar{reg: r, namespaces: namespaces}, nil
}

func (r *Registrar) CompilerInstructions() (*CompilerInstructionRegistrar, error) {
	grant, err := r.authorize(AuthorityCompilerInstructionDefine)
	if err != nil {
		return nil, err
	}
	modules := make(map[string]struct{}, len(grant.Scope.Modules))
	for _, module := range grant.Scope.Modules {
		modules[module] = struct{}{}
	}
	return &CompilerInstructionRegistrar{reg: r, modules: modules}, nil
}

// Define registers a plugin-owned logical value type and returns its opaque
// identity token.
func (r *CompilerTypeRegistrar) Define(spec CustomTypeSpec) (CustomType, error) {
	if r == nil || r.reg == nil {
		return CustomType{}, fmt.Errorf("wago: nil compiler registry")
	}
	namespace := spec.Name
	for i := 0; i < len(namespace); i++ {
		if namespace[i] == '/' {
			namespace = namespace[:i]
			break
		}
	}
	if _, ok := r.namespaces[namespace]; !ok {
		return CustomType{}, &PluginError{Plugin: r.reg.definition.ID, Phase: PluginPhaseAuthorize, Authority: AuthorityCompilerTypeDefine, Path: "scope.modules", Err: fmt.Errorf("type namespace %q is outside the grant: %w", namespace, ErrPermissionDenied)}
	}
	typ, err := coreplugins.PrepareCustomType(spec)
	if err != nil {
		return CustomType{}, err
	}
	if r.reg.customTypes == nil {
		r.reg.customTypes = make(map[string]CustomType)
	}
	if existing, ok := r.reg.customTypes[typ.Name()]; ok {
		if !existing.Equal(typ) {
			return CustomType{}, fmt.Errorf("wago: custom type %q conflicts with its previous registration", typ.Name())
		}
		return existing, nil
	}
	r.reg.customTypes[typ.Name()] = typ
	return typ, nil
}

func (r *CompilerInstructionRegistrar) validateCustomSignature(sig *CustomSignature) error {
	if sig == nil {
		return nil
	}
	check := func(typ CustomType) error {
		if typ.IsZero() {
			return nil
		}
		registered, ok := r.reg.customTypes[typ.Name()]
		if !ok || !registered.Equal(typ) {
			return fmt.Errorf("wago: custom type %q was not registered with this compiler registry", typ.Name())
		}
		return nil
	}
	for _, typ := range sig.Inputs {
		if err := check(typ); err != nil {
			return err
		}
	}
	if sig.Output != nil {
		return check(*sig.Output)
	}
	return nil
}

func resolveInstructionLowerings(m *wasm.Module, registered map[string]*registeredInstruction) map[uint32]coreplugins.Instruction {
	if len(registered) == 0 {
		return nil
	}
	resolved := make(map[uint32]coreplugins.Instruction)
	var functionIndex uint32
	for i := range m.Imports {
		imp := &m.Imports[i]
		if imp.Type.Kind != wasm.ExternFunc {
			continue
		}
		if ins := registered[imp.Module+"."+imp.Name]; ins != nil {
			if native, ok := ins.definition.Native(); ok {
				resolved[functionIndex] = native
			}
		}
		functionIndex++
	}
	if len(resolved) == 0 {
		return nil
	}
	return resolved
}

// Instruction registers a custom instruction under its ordinary Wasm import
// module and name.
func (r *CompilerInstructionRegistrar) Define(spec InstructionSpec) error {
	if r == nil || r.reg == nil {
		return fmt.Errorf("wago: nil compiler instruction registrar")
	}
	if _, ok := r.modules[spec.Module]; !ok {
		return &PluginError{Plugin: r.reg.definition.ID, Phase: PluginPhaseAuthorize, Authority: AuthorityCompilerInstructionDefine, Path: "scope.modules", Err: fmt.Errorf("module %q is outside the grant: %w", spec.Module, ErrPermissionDenied)}
	}
	if err := r.validateCustomSignature(spec.Custom); err != nil {
		return err
	}
	definition, err := coreplugins.Prepare(spec)
	if err != nil {
		return err
	}
	r.reg.instructions = append(r.reg.instructions, &registeredInstruction{
		spec:       definition.Spec,
		definition: definition,
	})
	return nil
}
