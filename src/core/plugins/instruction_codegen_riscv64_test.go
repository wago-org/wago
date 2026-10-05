//go:build riscv64

package plugins

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/codegen"
)

type unimplementedRV64Lowering codegen.Architecture

func (l unimplementedRV64Lowering) Architecture() codegen.Architecture {
	return codegen.Architecture(l)
}

func TestRV64InstructionCodegenRejectsEveryNativeTarget(t *testing.T) {
	for _, arch := range []codegen.Architecture{codegen.AMD64, codegen.ARM64, "riscv64"} {
		def, err := Prepare(InstructionSpec{
			Module: "example", Name: "pending", Input: []int32{32},
			Handler: func(InstructionContext, []Bits) ([]Bits, error) { return nil, nil },
			Codegen: unimplementedRV64Lowering(arch),
		})
		if _, native := def.Native(); native || err == nil || !strings.Contains(err.Error(), "riscv64 custom machine-code lowering is unavailable") {
			t.Fatal("pending target was registered", arch, def, err)
		}
	}
	if got := cloneMachineCode(nil); got != nil {
		t.Fatal("nil host-only metadata acquired machine code")
	}
}
