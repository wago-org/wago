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

func RuntimeTarget(active, channel, resolved string) string {
	activeChannel := active
	if !rolling(activeChannel) {
		activeChannel = pinnedChannel(activeChannel)
	}
	if activeChannel == "" || activeChannel != channel {
		return ""
	}
	if sha, found := strings.CutPrefix(resolved, "canary@"); found && validCommit(sha) {
		return "canary-" + sha[:7]
	}
	return resolved
}

func pinnedChannel(version string) string {
	for _, channel := range []string{"canary", "nightly"} {
		if strings.HasPrefix(version, channel+"-") {
			return channel
		}
	}
	return ""
}

func rolling(version string) bool {
	return version == "canary" || version == "nightly"
}

func validCommit(sha string) bool {
	if len(sha) != 40 {
		return false
	}
	for _, character := range sha {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}
