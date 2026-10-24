package wago

import (
	"os"
)

func gcProductAnalysisNeeded(required CoreFeatures) bool {
	return !gcProductRequiredGateEnabled || required.IsEnabled(CoreFeatureGC)
}

// Ordinary numeric and opaque funcref/externref modules use Types for their
// signatures and do not consume collector descriptors. Keep the complete table
// whenever typed references or exceptions could require a runtime GC domain.
func gcTypeMetadataNeeded(required CoreFeatures) bool {
	return required&(CoreFeatureGC|CoreFeatureTypedFunctionReferences|CoreFeatureExceptionHandling) != 0
}

// Feature requirements are already proved by validation.
// It avoids product hashes and opcode walks for modules that cannot use GC.
var gcProductRequiredGateEnabled = os.Getenv("WAGO_GC_PRODUCT_GATE") != "0"
