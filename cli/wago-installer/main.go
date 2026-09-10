// Command wago-installer installs the Wago manager and a Runtime Installation.
package main

import (
	"runtime/debug"

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
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return ""
}
