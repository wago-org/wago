// Package p1 provides the wasi_snapshot_preview1 host interface as a wago plugin.
// This is the common WASI ABI emitted by wasm32-wasip1 toolchains (Rust, C, Go,
// AssemblyScript): enough for programs that read/write the standard streams, exit,
// and query args/env/clock/random.
//
// Two ways to use it:
//
//	// As a plugin on a Runtime (capability-gated, inspectable):
//	rt := wago.NewRuntime()
//	rt.Use(p1.Ext(p1.Config{Stdout: os.Stdout}))
//
//	// As a raw host-import bundle on the low-level Instantiate path:
//	in, _ := wago.Instantiate(c, p1.Imports(p1.Config{Stdout: os.Stdout}))
//	in.Invoke("_start")
package p1

import (
	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/plugins/wasi/internal/core"
)

// Module is the wasm import module name these functions bind under.
const Module = "wasi_snapshot_preview1"

// Cap is the capability guarding the WASI surface.
const Cap = core.Cap

// Config configures the host bundle. See core.Config for field semantics.
type Config = core.Config

// Ext constructs the wasi_snapshot_preview1 extension from cfg.
func Ext(cfg Config) wago.Extension {
	return core.New(Module, wago.ExtensionInfo{
		ID:          "wago.wasi.preview1",
		Name:        "WASI preview 1",
		Version:     "1.0.0",
		Description: "Minimal wasi_snapshot_preview1: stdio, args/env, clock, random, exit.",
		MinWago:     "0.1.0",
		Stability:   wago.Stable,
	}, cfg)
}

// Imports returns the wasi_snapshot_preview1 host bundle for the low-level
// wago.Instantiate(c, imports) path, keyed "wasi_snapshot_preview1.<name>".
func Imports(cfg Config) wago.Imports { return core.Imports(Module, cfg) }
