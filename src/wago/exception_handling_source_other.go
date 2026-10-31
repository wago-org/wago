//go:build !linux || !riscv64 || tinygo || wago_target_tinygo || wago_standalone

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

func railshotExceptionHandlingSource(m *wasm.Module) bool { return false }

func railshotManagedExceptionReferences(*wasm.Module) bool { return false }
