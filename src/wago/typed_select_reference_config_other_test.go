//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

func typedSelectReferenceConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
}
