//go:build amd64

package plugins

import (
	"fmt"

	"github.com/wago-org/wago/codegen"
	amd64codegen "github.com/wago-org/wago/codegen/amd64"
)

func prepareMachineCode(spec InstructionSpec) (codegen.Lowering, error) {
	if spec.Codegen == nil {
		return nil, nil
	}
	lowering, ok := spec.Codegen.(*amd64codegen.Lowering)
	if !ok || lowering == nil {
		return nil, fmt.Errorf("wago: instruction %q.%q has %s codegen in an amd64 build", spec.Module, spec.Name, spec.Codegen.Architecture())
	}
	switch lowering.Compatibility {
	case amd64codegen.CompatibilityManaged:
		if lowering.Managed == nil || lowering.Emit != nil {
			return nil, fmt.Errorf("wago: instruction %q.%q managed amd64 lowering requires Managed and forbids Emit", spec.Module, spec.Name)
		}
	case amd64codegen.CompatibilityFullAccess:
		if lowering.Emit == nil || lowering.Managed != nil {
			return nil, fmt.Errorf("wago: instruction %q.%q full-access amd64 lowering requires Emit and forbids Managed", spec.Module, spec.Name)
		}
	default:
		return nil, fmt.Errorf("wago: instruction %q.%q requires an explicit amd64 compatibility mode", spec.Module, spec.Name)
	}
	const knownFeatures = amd64codegen.FeatureAVX2 | amd64codegen.FeatureAVX512 |
		amd64codegen.FeatureSSSE3 | amd64codegen.FeatureSSE41 | amd64codegen.FeatureSSE42 |
		amd64codegen.FeatureAVX | amd64codegen.FeatureBMI1 | amd64codegen.FeatureBMI2 |
		amd64codegen.FeatureLZCNT | amd64codegen.FeaturePOPCNT | amd64codegen.FeatureFMA
	if lowering.Features&^knownFeatures != 0 {
		return nil, fmt.Errorf("wago: instruction %q.%q declares unsupported amd64 features %#x", spec.Module, spec.Name, lowering.Features)
	}
	if err := validateMachineCodeWidths(spec, "amd64"); err != nil {
		return nil, err
	}
	copy := *lowering
	return &copy, nil
}

func cloneMachineCode(lowering codegen.Lowering) codegen.Lowering {
	if lowering == nil {
		return nil
	}
	copy := *(lowering.(*amd64codegen.Lowering))
	return &copy
}
