//go:build linux && amd64 && !tinygo

package riscv64

import (
	"testing"

	amd "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func typeCapAMD64Control(t *testing.T, m *wasm.Module) {
	t.Helper()
	if _, err := amd.CompileModuleWith(m, amd.CompileOptions{DeferCodeMapping: true}); err != nil {
		t.Fatal("amd64 control", err)
	}
}
