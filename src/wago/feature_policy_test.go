package wago

// compatibilityDefaultConfig preserves the pre-promotion feature mask for tests
// whose purpose is to prove that a proposal fails closed when its bit is off.
// Those tests must not rely on the process-wide default policy.
func compatibilityDefaultConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(coreFeaturesWithoutSidecar)
}
