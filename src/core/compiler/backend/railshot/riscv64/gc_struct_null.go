//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCStructNullWire(data []byte) bool { return false }

func AdmitGCStructNullSource(data []byte) bool { return false }

func admitRV64GCStructNull(m *wasm.Module) bool { return false }
