//go:build riscv64

package plugins

import (
	"fmt"

	"github.com/wago-org/wago/codegen"
)

func prepareMachineCode(spec InstructionSpec) (codegen.Lowering, error) {
	if spec.Codegen != nil {
		return nil, fmt.Errorf("wago: instruction %q.%q: riscv64 custom machine-code lowering is unavailable", spec.Module, spec.Name)
	}
	return nil, nil
}

func cloneMachineCode(lowering codegen.Lowering) codegen.Lowering {
	if lowering != nil {
		panic("wago: unadmitted riscv64 custom machine-code lowering")
	}
	return nil
}
