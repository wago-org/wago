//go:build !wago_wasi

// No-WASI stub for the default build. WASI is not compiled in unless the binary
// is built with `-tags wago_wasi` (see wasi_on.go), so this build imports neither
// the wago-org/wasi module nor its submodule. A wasi/* plugin name errors with a
// hint instead of silently doing nothing.

package main

import "github.com/wago-org/wago"

// wasiImports is the no-op counterpart to the wago_wasi build's hook: it reports
// that WASI is unavailable in this binary. It returns handled=false for non-WASI
// names so the caller falls through to the plugin registry.
func wasiImports(name string, _ []string) (wago.Imports, bool) {
	switch name {
	case "wasi", "wasi/p1", "wasi/unstable", "wasi/p2":
		fatal("--plugin %s: this wago build has no WASI support; rebuild with -tags wago_wasi", name)
	}
	return nil, false
}
