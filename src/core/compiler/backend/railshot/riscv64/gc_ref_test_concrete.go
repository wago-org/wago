//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCRefTestConcreteWire(data []byte) bool { return false }

// AdmitGCRefTestConcreteSource is the exact-wire witness for the official
// concrete struct-subtyping ref.test checkpoint.
func AdmitGCRefTestConcreteSource(data []byte) bool { return false }

func admitRV64GCRefTestConcrete(m *wasm.Module) bool { return false }
