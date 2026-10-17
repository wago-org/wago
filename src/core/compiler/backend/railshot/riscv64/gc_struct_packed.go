//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCStructPackedWire(data []byte) bool { return false }

func AdmitGCStructPackedSource(data []byte) bool { return false }

func admitRV64GCStructPacked(m *wasm.Module) bool { return false }
