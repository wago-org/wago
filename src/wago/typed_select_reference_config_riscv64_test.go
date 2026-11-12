//go:build linux && riscv64

package wago

func typedSelectReferenceConfig() *RuntimeConfig {
	return rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
}
