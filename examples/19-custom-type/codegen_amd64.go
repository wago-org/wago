//go:build amd64 && !tinygo

package main

import (
	"github.com/wago-org/wago/codegen"
	amd64 "github.com/wago-org/wago/codegen/amd64"
)

func zeroCodegen() codegen.Lowering {
	return &amd64.Lowering{
		Compatibility: amd64.CompatibilityFullAccess,
		Emit: func(ctx amd64.Context) error {
			value := ctx.AllocYMM()
			ctx.Encoder().YPxor(value, value, value)
			return ctx.OutputCustom(value)
		},
	}
}

func discardCodegen() codegen.Lowering {
	return &amd64.Lowering{
		Compatibility: amd64.CompatibilityFullAccess,
		Emit: func(ctx amd64.Context) error {
			value, err := ctx.InputCustom(0)
			if err != nil {
				return err
			}
			for _, register := range value {
				ctx.ReleaseVector(register)
			}
			return nil
		},
	}
}
