package plugin

import (
	"sort"
	"strings"

	"github.com/wago-org/wago"
)

func compiledPluginSummary() string {
	names := wago.RegisteredPluginNames()
	if len(names) == 0 {
		return "none"
	}
	plugins := make([]string, 0, len(names))
	for _, name := range names {
		label := name
		if extension, ok := wago.NewExtension(name); ok {
			if version := strings.TrimSpace(extension.Info().Version); version != "" {
				label += "@" + version
			}
		}
		plugins = append(plugins, label)
	}
	sort.Strings(plugins)
	return strings.Join(plugins, ", ")
}
