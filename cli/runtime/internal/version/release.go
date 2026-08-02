package version

import "strings"

func diagnosticChannel(activeVersion, release string) string {
	switch {
	case activeVersion == "canary", strings.HasPrefix(release, "canary-"):
		return "canary"
	case activeVersion == "nightly", strings.HasPrefix(release, "nightly-"):
		return "nightly"
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
