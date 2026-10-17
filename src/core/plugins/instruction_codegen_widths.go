//go:build amd64 || arm64 || riscv64

package plugins

import "fmt"

func validateMachineCodeWidths(spec InstructionSpec, arch string) error {
	if len(spec.Output) > 1 {
		return fmt.Errorf("wago: instruction %q.%q %s lowering supports at most one direct output", spec.Module, spec.Name, arch)
	}
	for i, width := range spec.Input {
		if width > 32 && (spec.Custom == nil || spec.Custom.Inputs[i].IsZero()) {
			return fmt.Errorf("wago: instruction %q.%q %s lowering only supports direct values up to 32 bits", spec.Module, spec.Name, arch)
		}
	}
	for _, width := range spec.Output {
		if width > 32 && (spec.Custom == nil || spec.Custom.Output == nil) {
			return fmt.Errorf("wago: instruction %q.%q %s lowering only supports direct values up to 32 bits", spec.Module, spec.Name, arch)
		}
	}
	return nil
}
