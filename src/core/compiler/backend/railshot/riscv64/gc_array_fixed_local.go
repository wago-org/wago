//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayFixedLocalWire(data []byte) bool { return false }

func AdmitGCArrayFixedLocalSource(data []byte) bool { return false }

func admitRV64GCArrayFixedLocal(m *wasm.Module) bool { return false }
