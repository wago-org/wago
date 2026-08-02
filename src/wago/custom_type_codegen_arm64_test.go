//go:build arm64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/codegen"
	arm64codegen "github.com/wago-org/wago/codegen/arm64"
)

func customProducerCodegen() codegen.Lowering {
	return &arm64codegen.Lowering{Compatibility: arm64codegen.CompatibilityFullAccess, Emit: func(ctx arm64codegen.Context) error {
		a, b := ctx.AllocVector(), ctx.AllocVector()
		ctx.Encoder().Eor16b(a, a, a)
		ctx.Encoder().Eor16b(b, b, b)
		return ctx.OutputCustom(a, b)
	}}
}

func customConsumerCodegen() codegen.Lowering {
	return &arm64codegen.Lowering{Compatibility: arm64codegen.CompatibilityFullAccess, Emit: func(ctx arm64codegen.Context) error {
		regs, err := ctx.InputCustom(0)
		if err != nil {
			return err
		}
		for _, reg := range regs {
			ctx.ReleaseVector(reg)
		}
		return nil
	}}
}
