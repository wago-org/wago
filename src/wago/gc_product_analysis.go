package wago

import "os"

// Feature requirements are already proved by validation.
// It avoids product hashes and opcode walks for modules that cannot use GC.
var gcProductRequiredGateEnabled = os.Getenv("WAGO_GC_PRODUCT_GATE") != "0"

func gcProductAnalysisNeeded(required CoreFeatures) bool {
	return !gcProductRequiredGateEnabled || required.IsEnabled(CoreFeatureGC)
}
