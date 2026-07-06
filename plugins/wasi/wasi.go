// Package wasi is the default WASI host interface for wasm guests: it re-exports
// wasi_snapshot_preview1 (the ABI emitted by wasm32-wasip1 toolchains), the common
// case. Importing this package gives you plain wasi.Ext / wasi.Imports / wasi.Config.
//
// To pin a specific snapshot, import the versioned subpackage by its full path
// instead:
//
//	plugins/wasi/p1        — wasi_snapshot_preview1 (same as this package)
//	plugins/wasi/unstable  — wasi_unstable, the pre-preview1 "snapshot 0" ABI
//	plugins/wasi/p2        — WASI preview 2 (component model; placeholder, not yet)
//
// Usage:
//
//	// On a Runtime (capability-gated, inspectable):
//	rt := wago.NewRuntime()
//	rt.Use(wasi.Ext(wasi.Config{Stdout: os.Stdout}))
//
//	// As a raw host-import bundle on the low-level Instantiate path:
//	in, _ := wago.Instantiate(c, wasi.Imports(wasi.Config{Stdout: os.Stdout}))
//	in.Invoke("_start")
package wasi

import (
	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/plugins/wasi/p1"
)

// Module is the default WASI wasm import module name (wasi_snapshot_preview1).
const Module = p1.Module

// Cap is the capability guarding the WASI surface.
const Cap = p1.Cap

// Config configures the WASI host bundle. See the core package for field semantics.
type Config = p1.Config

// Ext constructs the default (wasi_snapshot_preview1) extension from cfg.
func Ext(cfg Config) wago.Extension { return p1.Ext(cfg) }

// Imports returns the default (wasi_snapshot_preview1) host bundle for the
// low-level wago.Instantiate(c, imports) path.
func Imports(cfg Config) wago.Imports { return p1.Imports(cfg) }
