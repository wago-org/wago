//go:build linux && !amd64 && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func typeCapAMD64Control(*testing.T, *wasm.Module) {}
