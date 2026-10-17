//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCTypeSubtypingRuntimeTypedTableCallWire(data []byte) bool { return false }

// AdmitGCTypeSubtypingRuntimeTypedTableCallSource returns the exact-wire
// provenance witness for official gc/type-subtyping.19.wasm.
func AdmitGCTypeSubtypingRuntimeTypedTableCallSource(data []byte) bool { return false }

func admitRV64GCTypeSubtypingRuntimeTypedTableCall(m *wasm.Module) bool { return false }
