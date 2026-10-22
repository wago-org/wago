package wago

// compatibilityDefaultConfig preserves the pre-promotion feature mask for tests
// whose purpose is to prove that a proposal fails closed when its bit is off.
// Those tests must not rely on the process-wide default policy.
func compatibilityDefaultConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(coreFeaturesWithoutSidecar)
}

// artifactTestConfig preserves the build's default bounds mode for source
// execution, but selects the serializable explicit mode when a fixture will be
// round-tripped through an artifact. Signals-based native code deliberately
// cannot be serialized because its guard mapping is process-local.
func artifactTestConfig(artifact bool) *RuntimeConfig {
	cfg := NewRuntimeConfig()
	if artifact {
		cfg = cfg.WithBoundsChecks(BoundsChecksExplicit)
	}
	return cfg
}
