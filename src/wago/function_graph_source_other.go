//go:build !linux || !riscv64 || wago_precompiled || tinygo || wago_target_tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

func railshotPrimitiveFunctionGraphSource(*wasm.Module) bool { return false }

func railshotTypeDeclarationsSource(*wasm.Module) bool { return false }
