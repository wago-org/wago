package plugin

import (
	"sort"
	"strings"
)

func compiledPluginSummary() string {
	definitions := Definitions()
	if len(definitions) == 0 {
		return "none"
	}
	plugins := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		label := definition.ID
		if version := strings.TrimSpace(definition.Version); version != "" {
			label += "@" + version
		}
		plugins = append(plugins, label)
	}
	sort.Strings(plugins)
	return strings.Join(plugins, ", ")
}
