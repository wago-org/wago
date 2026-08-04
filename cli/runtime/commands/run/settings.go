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
	for name, enabled := range defaults.Features {
		if feature, ok := wago.FeatureInfoByName(name); ok && feature.Available {
			config = config.WithFeature(feature.Feature, enabled)
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
