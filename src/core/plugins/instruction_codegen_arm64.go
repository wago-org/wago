//go:build arm64

package plugins

import (
	"fmt"

	"github.com/wago-org/wago/codegen"
	arm64codegen "github.com/wago-org/wago/codegen/arm64"
)

func prepareMachineCode(spec InstructionSpec) (codegen.Lowering, error) {
	if spec.Codegen == nil {
		return nil, nil
	}
	lowering, ok := spec.Codegen.(*arm64codegen.Lowering)
	if !ok || lowering == nil {
		return nil, fmt.Errorf("wago: instruction %q.%q has %s codegen in an arm64 build", spec.Module, spec.Name, spec.Codegen.Architecture())
	}
	switch lowering.Compatibility {
	case arm64codegen.CompatibilityManaged:
		if lowering.Managed == nil || lowering.Emit != nil {
			return nil, fmt.Errorf("wago: instruction %q.%q managed arm64 lowering requires Managed and forbids Emit", spec.Module, spec.Name)
		}
	case arm64codegen.CompatibilityFullAccess:
		if lowering.Emit == nil || lowering.Managed != nil {
			return nil, fmt.Errorf("wago: instruction %q.%q full-access arm64 lowering requires Emit and forbids Managed", spec.Module, spec.Name)
		}
	default:
		return nil, fmt.Errorf("wago: instruction %q.%q requires an explicit arm64 compatibility mode", spec.Module, spec.Name)
	}
	if err := validateMachineCodeWidths(spec, "arm64"); err != nil {
		return nil, err
	}
	copy := *lowering
	return &copy, nil
}

func cloneMachineCode(lowering codegen.Lowering) codegen.Lowering {
	if lowering == nil {
		return nil
	}
	copy := *(lowering.(*arm64codegen.Lowering))
	return &copy
}
