//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayInitElemWire(data []byte) bool { return false }

func AdmitGCArrayInitElemSource(data []byte) bool { return false }

func admitRV64GCArrayInitElem(m *wasm.Module) bool { return false }
