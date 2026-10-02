//go:build arm64

package main

import (
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/arm64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func compileExplain(m *wasm.Module, guard, compact bool) (string, error) {
	var ms railshot.ModuleStats
	if _, err := railshot.CompileModuleWith(m, railshot.CompileOptions{
		ElideBoundsChecks: guard,
		Stats:             &ms,
		CompactNative:     compact,
	}); err != nil {
		return "", err
	}
	return ms.String(), nil
}
