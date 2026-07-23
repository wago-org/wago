package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/machinecode"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreplugins "github.com/wago-org/wago/src/core/plugins"
)

// CompilerRegistry is the trusted compiler contribution surface exposed during
// Extension.Register.
type CompilerRegistry struct{ reg *Registry }

// Public aliases keep the existing extension interface source-compatible while
// core/plugins owns the backend-neutral model and its implementation.
type AMD64Features = machinecode.AMD64Features
type AMD64Compatibility = machinecode.AMD64Compatibility
type AMD64InstructionLowering = machinecode.AMD64Lowering
type AMD64LoweringContext = machinecode.AMD64Context
type AMD64ManagedLoweringContext = machinecode.AMD64ManagedContext
type SIMDInstruction = coreplugins.SIMDInstruction
type InstructionSpec = coreplugins.InstructionSpec
type InstructionHandler = coreplugins.InstructionHandler
type InstructionContext = coreplugins.InstructionContext
type Bits = coreplugins.Bits
type LowerValue = coreplugins.LowerValue
type InstructionLowerer = coreplugins.InstructionLowerer
type LoweringContext = coreplugins.LoweringContext

const (
	AMD64FeatureAVX2             = machinecode.AMD64FeatureAVX2
	AMD64FeatureAVX512           = machinecode.AMD64FeatureAVX512
	AMD64CompatibilityManaged    = machinecode.AMD64CompatibilityManaged
	AMD64CompatibilityFullAccess = machinecode.AMD64CompatibilityFullAccess
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
func (r *CompilerRegistry) Instruction(spec InstructionSpec) error {
	if r == nil || r.reg == nil {
		return fmt.Errorf("wago: nil compiler registry")
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
