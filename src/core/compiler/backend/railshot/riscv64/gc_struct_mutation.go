//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCStructMutationWire(data []byte) bool { return false }

// AdmitGCStructMutationSource is the wire-provenance half of the bounded
// struct.new_default/struct.get/struct.set checkpoint.
func AdmitGCStructMutationSource(data []byte) bool { return false }

func admitRV64GCStructMutation(m *wasm.Module) bool { return false }
