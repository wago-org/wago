//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayLocalWire(data []byte) bool { return false }

// AdmitGCArrayLocalSource is the wire-provenance half of the exact numeric
// array.new_default/get/set/len checkpoint.
func AdmitGCArrayLocalSource(data []byte) bool { return false }

func admitRV64GCArrayLocal(m *wasm.Module) bool { return false }
