//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCBrOnCastConcreteWire(data []byte) bool { return false }

func admitRV64GCBrOnCastFailConcreteWire(data []byte) bool { return false }

func AdmitGCBrOnCastConcreteSource(data []byte) bool { return false }

func AdmitGCBrOnCastFailConcreteSource(data []byte) bool { return false }

func admitRV64GCBrOnCastConcrete(m *wasm.Module) bool { return false }

func admitRV64GCBrOnCastFailConcrete(m *wasm.Module) bool { return false }
