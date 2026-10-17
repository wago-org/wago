//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCTypeSubtypingRuntimeCallCastWire(data []byte) bool { return false }

// AdmitGCTypeSubtypingRuntimeCallCastSource returns the exact-wire provenance
// witness for the official gc/type-subtyping runtime call/cast module.
func AdmitGCTypeSubtypingRuntimeCallCastSource(data []byte) bool { return false }

func admitRV64GCTypeSubtypingRuntimeCallCast(m *wasm.Module) bool { return false }
