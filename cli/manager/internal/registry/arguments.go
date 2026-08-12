package registry

import "strings"

func splitVersion(value string) (name, version string) {
	if index := strings.LastIndexByte(value, '@'); index >= 0 {
		return value[:index], value[index+1:]
	}
	return value, ""
}
