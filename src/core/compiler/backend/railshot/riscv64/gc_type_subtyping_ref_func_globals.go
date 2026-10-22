//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCTypeSubtypingRefFuncGlobalsWire(data []byte) bool { return false }

func GCTypeSubtypingRefFuncGlobalsSourceVariant(data []byte) uint8 { return 0 }

func admitRV64GCTypeSubtypingRefFuncGlobals(m *wasm.Module) uint8 { return 0 }
