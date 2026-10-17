//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCStructGetWire(data []byte) bool { return false }

// AdmitGCStructGetSource is the wire-provenance half of the exact allocating
// struct.new_default/struct.get checkpoint.
func AdmitGCStructGetSource(data []byte) bool { return false }

func admitRV64GCStructGet(m *wasm.Module) bool { return false }
