//go:build amd64

package wagobench

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func benchCompileModule(m *wasm.Module) (*benchCompiledModule, error) {
	return benchCompileModuleWorkers(m, 1)
}

func benchCompileModuleWorkers(m *wasm.Module, workers int) (*benchCompiledModule, error) {
	cm, err := railshot.CompileModuleWith(m, railshot.CompileOptions{Workers: workers})
	if err != nil {
		return nil, err
	}
	return &benchCompiledModule{Code: cm.Code, Entry: cm.Entry}, nil
}

func benchCompileModuleSize(m *wasm.Module) (*benchCompiledModule, error) {
	objective := railshot.OptimizeSize
	cm, err := railshot.CompileModuleWith(m, railshot.CompileOptions{Workers: 1, Objective: &objective})
	if err != nil {
		return nil, err
	}
	return &benchCompiledModule{Code: cm.Code, Entry: cm.Entry}, nil
}
