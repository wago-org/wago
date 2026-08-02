//go:build !wago_runtime

// Command wago manages Wago installations and dispatches runtime commands to
// the selected wago-runtime executable.
package main

import "github.com/wago-org/wago/cli/manager"

// version is stamped at build time via -ldflags "-X main.version=<tag>" (see
// `make build`). It must be an uninitialized var: TinyGo only honors -X for
// variables declared without an initializer.
var version string

func main() { manager.Main(version) }
