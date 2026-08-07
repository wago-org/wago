package registry

import "strings"

func splitVersion(value string) (name, version string) {
	if index := strings.LastIndexByte(value, '@'); index >= 0 {
		return value[:index], value[index+1:]
	}
	return value, ""
}

func splitCommaList(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}
