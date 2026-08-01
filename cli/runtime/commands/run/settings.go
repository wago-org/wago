package run

import (
	"fmt"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/cli/internal/settings"
)

func LoadDefaults() (settings.Config, bool, error) {
	config, configured, err := settings.LoadConfigured()
	if err != nil {
		return settings.Config{}, false, fmt.Errorf("load Wago settings: %w", err)
	}
	return config, configured, nil
}

func ApplyFeatureDefaults(config *wago.RuntimeConfig, defaults settings.Config, configured bool) *wago.RuntimeConfig {
	if !configured {
		return config
	}
	features := map[string]wago.CoreFeatures{
		"bulk-memory-operations":              wago.CoreFeatureBulkMemoryOperations,
		"multi-value":                         wago.CoreFeatureMultiValue,
		"mutable-global":                      wago.CoreFeatureMutableGlobal,
		"nontrapping-float-to-int-conversion": wago.CoreFeatureNonTrappingFloatToIntConversion,
		"reference-types":                     wago.CoreFeatureReferenceTypes,
		"sign-extension-ops":                  wago.CoreFeatureSignExtensionOps,
		"simd":                                wago.CoreFeatureSIMD,
		"extended-constant-expressions":       wago.CoreFeatureExtendedConst,
	}
	for name, enabled := range defaults.Features {
		if feature, ok := features[name]; ok {
			config = config.WithFeature(feature, enabled)
		}
	}
	return config
}

func ResolveParallel(explicit string, defaults settings.Config, configured bool) string {
	if explicit != "" || !configured {
		return explicit
	}
	return defaults.Runtime.Parallel
}
