package self

import "strings"

func Channel(current string) string {
	if channel := pinnedChannel(current); channel != "" {
		return channel
	}
	if rolling(current) {
		return current
	}
	if strings.HasPrefix(current, "v") {
		return "latest"
	}
	return "canary"
}

func pinnedChannel(version string) string {
	for _, channel := range []string{"canary", "nightly"} {
		if strings.HasPrefix(version, channel+"-") || strings.HasPrefix(version, channel+"@") {
			return channel
		}
	}
	return ""
}

func rolling(version string) bool {
	return version == "canary" || version == "nightly"
}
