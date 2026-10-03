// Command wago-installer installs the Wago manager and a Runtime Installation.
package main

import (
	"runtime/debug"
	"strings"

	"github.com/wago-org/wago/cli/installer"
)

// version is stamped at build time via -ldflags "-X main.version=<tag>".
var version string

func main() {
	info, _ := debug.ReadBuildInfo()
	installer.Main(resolveInstallerVersion(version, info))
}

func resolveInstallerVersion(stamped string, info *debug.BuildInfo) string {
	if stamped != "" {
		return stamped
	}
	if info == nil {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.modified" && setting.Value != "false" {
			return ""
		}
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" && !dirtyVersion(info.Main.Version) && !pseudoVersion(info.Main.Version) {
		return info.Main.Version
	}
	return ""
}

func dirtyVersion(version string) bool {
	_, metadata, found := strings.Cut(version, "+")
	if !found {
		return false
	}
	for _, component := range strings.Split(metadata, ".") {
		if component == "dirty" {
			return true
		}
	}
	return false
}

func pseudoVersion(version string) bool {
	version, _, _ = strings.Cut(version, "+")
	dash := strings.LastIndexByte(version, '-')
	if dash < 0 || len(version)-dash-1 != 12 || !hexString(version[dash+1:]) {
		return false
	}
	prefix := version[:dash]
	if len(prefix) < 15 {
		return false
	}
	timestamp := prefix[len(prefix)-14:]
	separator := prefix[len(prefix)-15]
	return (separator == '.' || separator == '-') && decimalString(timestamp)
}

func hexString(value string) bool {
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func decimalString(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
