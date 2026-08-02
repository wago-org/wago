//go:build amd64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/codegen"
	amd64codegen "github.com/wago-org/wago/codegen/amd64"
)

func customProducerCodegen() codegen.Lowering {
	return &amd64codegen.Lowering{Compatibility: amd64codegen.CompatibilityFullAccess, Emit: func(ctx amd64codegen.Context) error {
		reg := ctx.AllocYMM()
		ctx.Encoder().YPxor(reg, reg, reg)
		return ctx.OutputCustom(reg)
	}}
}

func customConsumerCodegen() codegen.Lowering {
	return &amd64codegen.Lowering{Compatibility: amd64codegen.CompatibilityFullAccess, Emit: func(ctx amd64codegen.Context) error {
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
