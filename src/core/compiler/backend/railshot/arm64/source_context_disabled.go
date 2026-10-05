//go:build arm64 && !wago_regalloccheck

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/arm64"
)

func compileSourceModuleWith(m *wasm.Module, opts CompileOptions) (*encoder.CompiledModule, error) {
	return compileModuleWith(m, opts)
}
