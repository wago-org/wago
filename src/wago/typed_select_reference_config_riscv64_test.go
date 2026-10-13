//go:build linux && riscv64 && wago_rv64_experimental && !wago_precompiled

package wago

func typedSelectReferenceConfig() *RuntimeConfig {
	return rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
}
