// Command wago runs WebAssembly modules. The implementation lives in the
// importable package github.com/wago-org/wago/cli/wagocli, so a generated .wago
// build module can compile wago together with plugins (see `wago pkg build`).
package main

import "github.com/wago-org/wago/cli/wagocli"

// version is stamped at build time via -ldflags "-X main.version=<tag>" (see
// `make build`). It must be an uninitialized var: TinyGo only honors -X for
// variables declared without an initializer.
var version string

func main() { wagocli.Main(version) }
