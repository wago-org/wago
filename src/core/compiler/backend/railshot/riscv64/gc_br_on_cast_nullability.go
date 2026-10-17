//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCBrOnCastNullabilityWire(data []byte) bool { return false }

func admitRV64GCBrOnCastFailNullabilityWire(data []byte) bool { return false }

func AdmitGCBrOnCastNullabilitySource(data []byte) bool { return false }

func AdmitGCBrOnCastFailNullabilitySource(data []byte) bool { return false }

func admitRV64GCBrOnCastNullability(m *wasm.Module) bool { return false }

func admitRV64GCBrOnCastFailNullability(m *wasm.Module) bool { return false }
