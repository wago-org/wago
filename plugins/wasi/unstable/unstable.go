// Package unstable provides the wasi_unstable host interface as a wago plugin.
// wasi_unstable (a.k.a. "snapshot 0") is the pre-preview1 WASI ABI: older
// toolchains (early Rust wasm32-wasi, wasi-libc before the rename) import their
// functions under the "wasi_unstable" module name instead of
// "wasi_snapshot_preview1". For the minimal snapshot surface wago implements
// (stdio, args/env, clock, random, exit) the two are function-for-function
// identical, so this reuses the shared core under the older module name.
package unstable

import (
	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/plugins/wasi/internal/core"
)

// Module is the wasm import module name these functions bind under.
const Module = "wasi_unstable"

// Cap is the capability guarding the WASI surface.
const Cap = core.Cap

// Config configures the host bundle. See core.Config for field semantics.
type Config = core.Config

// Ext constructs the wasi_unstable extension from cfg.
func Ext(cfg Config) wago.Extension {
	return core.New(Module, wago.ExtensionInfo{
		ID:          "wago.wasi.unstable",
		Name:        "WASI unstable (snapshot 0)",
		Version:     "1.0.0",
		Description: "Pre-preview1 wasi_unstable: stdio, args/env, clock, random, exit.",
		MinWago:     "0.1.0",
		Stability:   wago.Deprecated,
	}, cfg)
}

// Imports returns the wasi_unstable host bundle for the low-level
// wago.Instantiate(c, imports) path, keyed "wasi_unstable.<name>".
func Imports(cfg Config) wago.Imports { return core.Imports(Module, cfg) }
