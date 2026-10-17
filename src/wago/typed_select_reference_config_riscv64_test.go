//go:build linux && riscv64 && !wago_precompiled

package wago

func typedSelectReferenceConfig() *RuntimeConfig {
	return rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
}
