//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCTypeSubtypingRuntimeFinalityCallCastWire(data []byte) bool { return false }

// AdmitGCTypeSubtypingRuntimeFinalityCallCastSource returns the exact-wire
// provenance witness for official gc/type-subtyping.18.wasm.
func AdmitGCTypeSubtypingRuntimeFinalityCallCastSource(data []byte) bool { return false }

func admitRV64GCTypeSubtypingRuntimeFinalityCallCast(m *wasm.Module) bool { return false }
