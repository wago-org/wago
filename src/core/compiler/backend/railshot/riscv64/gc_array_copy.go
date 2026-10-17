//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayCopyWire(data []byte) bool { return false }

func AdmitGCArrayCopySource(data []byte) bool { return false }

func admitRV64GCArrayCopy(m *wasm.Module) bool { return false }
