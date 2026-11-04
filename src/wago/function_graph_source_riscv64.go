//go:build linux && riscv64 && !wago_standalone && !tinygo && !wago_target_tinygo

package wago

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func railshotPrimitiveFunctionGraphSource(m *wasm.Module) bool {
	return railshot.AdmitPrimitiveFunctionGraphModule(m)
}

func railshotTypeDeclarationsSource(m *wasm.Module) bool {
	return railshot.AdmitTypeDeclarations(m)
}
