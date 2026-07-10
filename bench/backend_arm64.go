//go:build arm64

package wagobench

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func benchCompileModule(m *wasm.Module) (*benchCompiledModule, error) {
	cm, err := railshot.CompileModule(m)
	if err != nil {
		return nil, err
	}
	return &benchCompiledModule{Code: cm.Code, Entry: cm.Entry}, nil
}
