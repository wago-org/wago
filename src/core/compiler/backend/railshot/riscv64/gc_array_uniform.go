//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayUniformWire(data []byte) bool { return false }

// AdmitGCArrayUniformSource is the wire-provenance half of the exact numeric
// array.new/get/set/len checkpoint.
func AdmitGCArrayUniformSource(data []byte) bool { return false }

func admitRV64GCArrayUniform(m *wasm.Module) bool { return false }
