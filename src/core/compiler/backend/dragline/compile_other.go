//go:build !amd64 && !arm64

package dragline

import (
	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func compileNative(corecompiler.Input, *wasm.Module, *Metrics, *corecompiler.FunctionArtifactCache) (corecompiler.Output, error) {
	return corecompiler.Output{}, &UnsupportedError{Reason: "native emitter is unavailable on this build"}
}
