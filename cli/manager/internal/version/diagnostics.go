package version

import "strings"

func DiagnosticChannel(activeVersion, release string) string {
	switch {
	case activeVersion == "canary", channelRelease(release) == "canary", strings.HasPrefix(release, "canary@"):
		return "canary"
	case activeVersion == "beta", channelRelease(release) == "beta", strings.HasPrefix(release, "beta@"):
		return "beta"
	case activeVersion == "latest":
		return "latest"
	case strings.HasPrefix(release, "v"):
		return "stable"
	case activeVersion != "":
		return activeVersion
	default:
		return "development"
	}
}
