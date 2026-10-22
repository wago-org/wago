//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayFillWire(data []byte) bool { return false }

func AdmitGCArrayFillSource(data []byte) bool { return false }

func admitRV64GCArrayFill(m *wasm.Module) bool { return false }
