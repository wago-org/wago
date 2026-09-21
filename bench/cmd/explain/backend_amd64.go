//go:build amd64

package main

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func compileExplain(m *wasm.Module, guard, compact, includeCode bool) ([]byte, []int, string, error) {
	var ms railshot.ModuleStats
	compiled, err := railshot.CompileModuleWith(m, railshot.CompileOptions{
		ElideBoundsChecks: guard,
		Stats:             &ms,
		CompactNative:     compact,
	})
	if err != nil {
		return nil, nil, "", err
	}
	var code []byte
	if includeCode {
		code = append(code, compiled.Code...)
	}
	entry := append([]int(nil), compiled.Entry...)
	if compiled.CodeImage != nil {
		if err := compiled.CodeImage.Close(); err != nil {
			return nil, nil, "", err
		}
	}
	return code, entry, ms.String(), nil
}
