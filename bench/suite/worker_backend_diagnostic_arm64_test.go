//go:build arm64 && !windows

package wagobench

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func compileWorkerDiagnosticBackend(mod *wasm.Module, workers int) error {
	compiled, err := railshot.CompileModuleWith(mod, railshot.CompileOptions{Workers: workers})
	if err != nil {
		return err
	}
	if compiled.CodeImage != nil {
		return compiled.CodeImage.Close()
	}
	return nil
}
