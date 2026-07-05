package main

import (
	"fmt"

	"github.com/wago-org/wago"
)

// printVersion prints the binary version and supported features. Shared by both
// the full version manager and the lean stub.
func printVersion() {
	fmt.Printf("%s %s (linux/amd64)\n", bold("wago"), versionString())
	fmt.Printf("%s %s\n", dim("features:"), wago.SupportedFeatures())
	if wago.GuardPageSupported() {
		fmt.Printf("%s signals-based bounds checks available\n", dim("guard-page:"))
	}
}
