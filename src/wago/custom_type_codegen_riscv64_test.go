//go:build linux && riscv64

package wago

import (
	"github.com/wago-org/wago/codegen"
	rv "github.com/wago-org/wago/codegen/riscv64"
)

func customProducerCodegen() codegen.Lowering {
	return &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(ctx rv.Context) error {
		cells := make([]rv.Reg, 4)
		for i := range cells {
			cells[i] = ctx.AllocGP()
			ctx.Encoder().Addi(cells[i], rv.Zero, 0)
		}
		return ctx.OutputCustom(cells...)
	}}
}
func customConsumerCodegen() codegen.Lowering {
	return &rv.Lowering{Compatibility: rv.CompatibilityManaged, Managed: func(ctx rv.ManagedContext) error {
		cells, err := ctx.InputCustom(0)
		if err != nil {
			return err
		}
		for _, cell := range cells {
			ctx.ReleaseGP(cell)
		}
		return nil
	}}
}
func customCodegenAvailable() bool { return true }
