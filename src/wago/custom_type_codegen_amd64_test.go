//go:build amd64 && !tinygo

package wago

import (
	"github.com/wago-org/wago/codegen"
	amd64codegen "github.com/wago-org/wago/codegen/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

func customProducerCodegen() codegen.Lowering {
	return &amd64codegen.Lowering{Compatibility: amd64codegen.CompatibilityFullAccess, Features: amd64codegen.FeatureAVX2, Emit: func(ctx amd64codegen.Context) error {
		reg := ctx.AllocYMM()
		ctx.Encoder().YPxor(reg, reg, reg)
		return ctx.OutputCustom(reg)
	}}
}

func customConsumerCodegen() codegen.Lowering {
	return &amd64codegen.Lowering{Compatibility: amd64codegen.CompatibilityFullAccess, Features: amd64codegen.FeatureAVX2, Emit: func(ctx amd64codegen.Context) error {
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

func customCodegenAvailable() bool {
	f, ok := cachedAMD64CPUFeatures()
	return ok && selectedAMD64CompileFeatures(f).Has(shared.AMD64AVX|shared.AMD64AVX2)
}
