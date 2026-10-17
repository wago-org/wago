//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCTypeSubtypingRecursiveFunctionsWire(data []byte) bool { return false }

func GCTypeSubtypingRecursiveFunctionsSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingRecursiveFunctions(m *wasm.Module) uint8 { return 0 }
