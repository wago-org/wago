//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCRefCastConcreteWire(data []byte) bool { return false }

// AdmitGCRefCastConcreteSource is the exact-wire witness for the official
// concrete struct-subtyping ref.cast checkpoint.
func AdmitGCRefCastConcreteSource(data []byte) bool { return false }

func admitRV64GCRefCastConcrete(m *wasm.Module) bool { return false }
