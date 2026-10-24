package wago

func gcProductAnalysisNeeded(required CoreFeatures) bool {
	return !gcProductRequiredGateEnabled || required.IsEnabled(CoreFeatureGC)
}

// Ordinary numeric and opaque funcref/externref modules use Types for their
// signatures and do not consume collector descriptors. Keep the complete table
// whenever typed references or exceptions could require a runtime GC domain.
func gcTypeMetadataNeeded(required CoreFeatures) bool {
	return required&(CoreFeatureGC|CoreFeatureTypedFunctionReferences|CoreFeatureExceptionHandling) != 0
}
