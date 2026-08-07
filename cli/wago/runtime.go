//go:build wago_runtime

// Command wago-runtime executes WebAssembly modules and exposes commands that
// inspect the plugins compiled into this runtime. The user-facing wago manager
// launches this binary; users normally do not invoke it directly.
package main

import "github.com/wago-org/wago/cli/runtime"

// version is stamped at build time via -ldflags "-X main.version=<tag>".
var version string

func main() { runtime.Main(version) }
