//go:build amd64 && wago_regalloccheck

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

// Joining workers precedes releasing facts, including scratch retained by a
// custom lowering callback. This scope starts after public option validation.
func compileSourceModuleWith(m *wasm.Module, opts CompileOptions) (*encoder.CompiledModule, error) {
	defer codegen.CloseSourceContext(opts.Codegen, m)
	return compileModuleWith(m, opts)
}
