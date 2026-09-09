package version

import "strings"

func diagnosticChannel(activeVersion, release string) string {
	switch {
	case activeVersion == "canary", strings.Contains(release, "-canary.g"), strings.HasPrefix(release, "canary@"):
		return "canary"
	case activeVersion == "beta", strings.Contains(release, "-beta."), strings.HasPrefix(release, "beta@"):
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
