//go:build wago_wasi

// WASI support, compiled in only with `-tags wago_wasi`. This is the sole file
// that imports github.com/wago-org/wasi, so a default build needs neither the
// module nor its plugins/wasi submodule. Build with the tag (and the submodule
// checked out) to include WASI:
//
//	go build -tags wago_wasi ./cli/wago

package main

import (
	"os"

	"github.com/wago-org/wago"
	"github.com/wago-org/wasi/p1"
	"github.com/wago-org/wasi/unstable"
)

func init() {
	// WASI plugins are selected by path: `wasi` is the default (preview1), and a
	// specific snapshot is `wasi/<version>` (wasi/p1, wasi/unstable). Preview 2
	// (wasi/p2) is a placeholder and not yet implemented.
	wago.RegisterExtension("wasi", func() wago.Extension { return p1.Ext(wasiCLIConfig()) })
	wago.RegisterExtension("wasi/p1", func() wago.Extension { return p1.Ext(wasiCLIConfig()) })
	wago.RegisterExtension("wasi/unstable", func() wago.Extension { return unstable.Ext(wasiCLIConfig()) })
}

// wasiCLIConfig is the base WASI config for the CLI: process stdio and env. argv
// is filled in per run (the run's positional args) by wasiImports.
func wasiCLIConfig() p1.Config {
	return p1.Config{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, Env: os.Environ()}
}

// wasiImports resolves the built-in WASI plugin names to their host imports.
// handled is false for any non-WASI name, so the caller falls back to the plugin
// registry.
func wasiImports(name string, argv []string) (wago.Imports, bool) {
	cfg := wasiCLIConfig()
	cfg.Args = argv
	out := wago.Imports{}
	switch name {
	case "wasi", "wasi/p1":
		mergeImports(out, p1.Imports(cfg))
	case "wasi/unstable":
		mergeImports(out, unstable.Imports(cfg))
	case "wasi/p2":
		fatal("--plugin wasi/p2: WASI preview 2 (component model) is not implemented yet; use wasi (preview1) or wasi/unstable")
	default:
		return nil, false
	}
	return out, true
}
